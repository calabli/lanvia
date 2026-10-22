package core

import (
	"crypto/sha256"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// errSkip 表示剪贴板内容被标记为敏感（如密码管理器），不应同步。
var errSkip = errors.New("clipboard content excluded")

// ClipWatcher 轮询系统剪贴板。各平台的 readClipboard / writeClipboard 见 clipboard_*.go。
type ClipWatcher struct {
	a       *App
	mu      sync.Mutex
	last    string
	seq     int64    // 上次看到的剪贴板变化计数（不支持的平台为 -1）
	lastImg [32]byte // 上次发送或写入的图片的哈希
	ok      bool
	idle    bool // 上一轮处于被动模式或暂停中

	// 手动发送页面每 2 秒查一次剪贴板：图片按变化计数缓存，内容没变就不重新读取、转换
	peekSeq  int64
	peekSize int
	peekHash [32]byte
}

func newClipWatcher(a *App) *ClipWatcher {
	c := &ClipWatcher{a: a, ok: clipboardSupported(), seq: -1}
	if c.ok {
		c.last, _ = readClipboard() // 启动时已有的内容不分享
		c.seq = clipSeq()
	}
	return c
}

func (c *ClipWatcher) run() {
	if !c.ok {
		return
	}
	for {
		time.Sleep(500 * time.Millisecond)
		c.poll()
	}
}

// poll 检查一次剪贴板，有新复制的文字或图片就发送。
func (c *ClipWatcher) poll() {
	// 被动模式或暂停中不发送；恢复时把当前剪贴板记为已处理，不补发这段时间复制的内容
	if !c.a.cfgSnapshot().Watch || c.a.Paused() {
		c.idle = true
		return
	}
	if c.idle {
		c.idle = false
		c.resync()
		return
	}
	c.mu.Lock()
	seq := clipSeq()
	changed := seq < 0 || seq != c.seq
	c.seq = seq
	txt, err := readClipboard()
	// 剪贴板变了、里面没有文字：可能是图片（截图、在浏览器或看图软件里复制的图片）
	if changed && seq >= 0 && err == nil && txt == "" {
		img, _ := readClipboardImage()
		h := sha256.Sum256(img)
		if len(img) == 0 || len(img) > maxClipImage || h == c.lastImg {
			c.mu.Unlock()
			return
		}
		c.lastImg = h
		c.mu.Unlock()
		// 访达里复制的文件也带着图标图片；被标记为敏感的、黑名单应用里复制的都不发送
		if skipClipboardChange() {
			return
		}
		if id, name := frontApp(); c.a.noteApp(id, name) {
			return
		}
		c.a.newClipImage(img)
		return
	}
	if err != nil || txt == "" || txt == c.last {
		if err == errSkip {
			c.last = ""
		}
		c.mu.Unlock()
		return
	}
	c.last = txt
	c.mu.Unlock()
	if len(txt) > maxTextLen || !utf8.ValidString(txt) {
		return
	}
	// 只在内容变化时检查一次：复制的是文件（只共享拖拽进来的文件）或被标记为敏感的内容就不发送
	if skipClipboardChange() {
		return
	}
	// 在黑名单里的应用中复制的不发送
	if id, name := frontApp(); c.a.noteApp(id, name) {
		return
	}
	c.a.newText(txt, c.a.cfgSnapshot().Name, false)
}

// Apply 把远端内容写入本机剪贴板，同时记住它，避免被当作新内容再次广播。
func (c *ClipWatcher) Apply(text string) error {
	if !c.ok {
		return errors.New("clipboard unsupported")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = text
	err := writeClipboard(text)
	c.seq = clipSeq()
	return err
}

// ApplyImage 把远端发来的图片写入本机剪贴板，不会被当作新复制的图片再发出去。
func (c *ClipWatcher) ApplyImage(data []byte) error {
	if !c.ok {
		return errors.New("clipboard unsupported")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := writeClipboardImage(data)
	c.seq = clipSeq()
	c.last = ""
	if img, e := readClipboardImage(); e == nil {
		c.lastImg = sha256.Sum256(img) // 万一计数没变也能认出来
	}
	return err
}

// resync 把当前剪贴板记为已处理。从被动切回主动时调用，避免补发被动期间复制的内容。
func (c *ClipWatcher) resync() {
	if !c.ok {
		return
	}
	c.mu.Lock()
	c.last, _ = readClipboard()
	c.seq = clipSeq()
	c.mu.Unlock()
}

// ClipboardState 是本机剪贴板的当前状态，给被动模式的“发送当前剪贴板”用。
type ClipboardState struct {
	Text   string `json:"text"`             // 预览（截断）
	Length int    `json:"length"`           // 完整长度（字符数）
	Image  int    `json:"image,omitempty"`  // 剪贴板里是图片时：图片大小（字节）
	Reason string `json:"reason,omitempty"` // 不能发送的原因
	Sent   bool   `json:"sent"`             // 和最新一条内容相同
}

// peekImage 看看剪贴板里有没有图片（没有文字时调用，调用方持有 c.mu）。返回大小和哈希，图片太大时 size 为 -1。
func (c *ClipWatcher) peekImage() (size int, hash [32]byte) {
	seq := clipSeq()
	if seq < 0 {
		return 0, hash
	}
	if seq == c.peekSeq {
		return c.peekSize, c.peekHash
	}
	c.peekSeq, c.peekSize, c.peekHash = seq, 0, [32]byte{}
	if skipClipboardChange() { // 访达里复制的文件
		return 0, hash
	}
	img, err := readClipboardImage()
	if err != nil || len(img) == 0 {
		return 0, hash
	}
	c.peekSize, c.peekHash = len(img), sha256.Sum256(img)
	if len(img) > maxClipImage {
		c.peekSize = -1
	}
	return c.peekSize, c.peekHash
}

func (c *ClipWatcher) read() (txt, reason string) {
	if !c.ok {
		return "", "这台电脑不支持读取剪贴板"
	}
	txt, err := readClipboard()
	switch {
	case err == errSkip || (err == nil && txt != "" && skipClipboardChange()):
		return "", "剪贴板里是文件或敏感内容，不会发送"
	case err != nil:
		return "", "读取剪贴板失败"
	case txt == "":
		return "", "剪贴板里没有文字或图片"
	case len(txt) > maxTextLen || !utf8.ValidString(txt):
		return "", "内容太长"
	}
	return txt, ""
}

// ClipboardState 读取当前剪贴板（不发送）。
func (a *App) ClipboardState() ClipboardState {
	a.clip.mu.Lock()
	txt, reason := a.clip.read()
	var size int
	var hash [32]byte
	if txt == "" && reason == "剪贴板里没有文字或图片" {
		size, hash = a.clip.peekImage()
	}
	sentImg := size != 0 && hash == a.clip.lastImg
	a.clip.mu.Unlock()
	switch {
	case size > 0:
		return ClipboardState{Image: size, Sent: sentImg}
	case size < 0:
		return ClipboardState{Reason: "图片太大（超过 20 MB），请拖进窗口发送"}
	}
	st := ClipboardState{Text: trimRunes(txt, 300), Length: len([]rune(txt)), Reason: reason}
	if l := a.store.LatestText(); l != nil && txt != "" && l.Text == txt {
		st.Sent = true
	}
	return st
}

// SendClipboard 手动发送当前剪贴板里的文字（被动模式下使用，主动模式下也可以用）。
func (a *App) SendClipboard() (*Item, error) {
	a.clip.mu.Lock()
	txt, reason := a.clip.read()
	if reason == "" {
		a.clip.last = txt // 主动模式下不会再自动发一遍
	}
	// 没有文字时看看是不是图片
	if txt == "" && reason == "剪贴板里没有文字或图片" && clipSeq() >= 0 && !skipClipboardChange() {
		if img, err := readClipboardImage(); err == nil && len(img) > 0 {
			h := sha256.Sum256(img)
			if len(img) > maxClipImage {
				a.clip.mu.Unlock()
				return nil, errors.New("图片太大（超过 20 MB），请拖进窗口发送")
			}
			if h == a.clip.lastImg {
				a.clip.mu.Unlock()
				return nil, errors.New("这张图片已经发送过")
			}
			a.clip.lastImg = h
			a.clip.mu.Unlock()
			return a.newClipImage(img)
		}
	}
	a.clip.mu.Unlock()
	if reason != "" {
		return nil, errors.New(reason)
	}
	if l := a.store.LatestText(); l != nil && l.Text == txt {
		return l, nil // 已经是最新一条，不重复发送
	}
	return a.newText(txt, a.cfgSnapshot().Name, false), nil
}

// ---- 暂停同步 ----

// Paused 表示暂停中：本机复制的不发送，收到的不写入剪贴板（仍会出现在列表里）。
func (a *App) Paused() bool {
	pu := a.cfgSnapshot().PausedUntil
	return pu == -1 || pu > nowMs()
}

// Pause 暂停一段时间；d < 0 表示直到手动恢复，d == 0 表示恢复。
func (a *App) Pause(d time.Duration) {
	var until int64
	switch {
	case d < 0:
		until = -1
	case d > 0:
		until = nowMs() + d.Milliseconds()
	}
	if until == 0 {
		a.clip.resync()
	}
	a.updateCfg(func(c *Config) { c.PausedUntil = until })
}

// autoResume 到点后清掉暂停状态（让界面和托盘及时更新）。
func (a *App) autoResume() {
	if pu := a.cfgSnapshot().PausedUntil; pu > 0 && pu <= nowMs() {
		a.updateCfg(func(c *Config) { c.PausedUntil = 0 })
	}
}

// ---- 应用黑名单 ----

// SourceApp 是复制内容时位于最前面的应用。
type SourceApp struct {
	ID   string `json:"id"`   // macOS 为 bundle id，Windows 为进程名（如 chrome.exe）
	Name string `json:"name"` // 显示名
	Seen int64  `json:"seen"`
}

var (
	recentMu   sync.Mutex
	recentApps = map[string]*SourceApp{}
)

// noteApp 记下最近复制来源，返回这个应用是否在黑名单里。
func (a *App) noteApp(id, name string) bool {
	if id == "" && name == "" {
		return false
	}
	key := strings.ToLower(id + "|" + name)
	recentMu.Lock()
	recentApps[key] = &SourceApp{ID: id, Name: name, Seen: nowMs()}
	if len(recentApps) > 20 {
		var oldest string
		for k, v := range recentApps {
			if oldest == "" || v.Seen < recentApps[oldest].Seen {
				oldest = k
			}
		}
		delete(recentApps, oldest)
	}
	recentMu.Unlock()
	return a.blocked(id, name)
}

func (a *App) blocked(id, name string) bool {
	for _, b := range a.cfgSnapshot().BlockApps {
		b = strings.TrimSpace(b)
		if b != "" && (strings.EqualFold(b, id) || strings.EqualFold(b, name)) {
			return true
		}
	}
	return false
}

// RecentApps 返回最近复制过内容的应用，新的在前。
func (a *App) RecentApps() []SourceApp {
	recentMu.Lock()
	defer recentMu.Unlock()
	out := make([]SourceApp, 0, len(recentApps))
	for _, v := range recentApps {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seen > out[j].Seen })
	return out
}
