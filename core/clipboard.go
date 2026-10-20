package core

import (
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
	a    *App
	mu   sync.Mutex
	last string
	ok   bool
}

func newClipWatcher(a *App) *ClipWatcher {
	c := &ClipWatcher{a: a, ok: clipboardSupported()}
	if c.ok {
		c.last, _ = readClipboard() // 启动时已有的内容不分享
	}
	return c
}

func (c *ClipWatcher) run() {
	if !c.ok {
		return
	}
	wasIdle := false
	for {
		time.Sleep(500 * time.Millisecond)
		// 被动模式或暂停中不发送；恢复时把当前剪贴板记为已处理，不补发这段时间复制的内容
		if !c.a.cfgSnapshot().Watch || c.a.Paused() {
			wasIdle = true
			continue
		}
		if wasIdle {
			wasIdle = false
			c.resync()
			continue
		}
		c.mu.Lock()
		txt, err := readClipboard()
		if err != nil || txt == "" || txt == c.last {
			if err == errSkip {
				c.last = ""
			}
			c.mu.Unlock()
			continue
		}
		c.last = txt
		c.mu.Unlock()
		if len(txt) > maxTextLen || !utf8.ValidString(txt) {
			continue
		}
		// 只在内容变化时检查一次：复制的是文件（只共享拖拽进来的文件）或被标记为敏感的内容就不发送
		if skipClipboardChange() {
			continue
		}
		// 在黑名单里的应用中复制的不发送
		if id, name := frontApp(); c.a.noteApp(id, name) {
			continue
		}
		c.a.newText(txt, c.a.cfgSnapshot().Name, false)
	}
}

// Apply 把远端内容写入本机剪贴板，同时记住它，避免被当作新内容再次广播。
func (c *ClipWatcher) Apply(text string) error {
	if !c.ok {
		return errors.New("clipboard unsupported")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = text
	return writeClipboard(text)
}

// resync 把当前剪贴板记为已处理。从被动切回主动时调用，避免补发被动期间复制的内容。
func (c *ClipWatcher) resync() {
	if !c.ok {
		return
	}
	c.mu.Lock()
	c.last, _ = readClipboard()
	c.mu.Unlock()
}

// ClipboardState 是本机剪贴板的当前状态，给被动模式的“发送当前剪贴板”用。
type ClipboardState struct {
	Text   string `json:"text"`             // 预览（截断）
	Length int    `json:"length"`           // 完整长度（字符数）
	Reason string `json:"reason,omitempty"` // 不能发送的原因
	Sent   bool   `json:"sent"`             // 和最新一条内容相同
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
		return "", "剪贴板里没有文字"
	case len(txt) > maxTextLen || !utf8.ValidString(txt):
		return "", "内容太长"
	}
	return txt, ""
}

// ClipboardState 读取当前剪贴板（不发送）。
func (a *App) ClipboardState() ClipboardState {
	a.clip.mu.Lock()
	txt, reason := a.clip.read()
	a.clip.mu.Unlock()
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
