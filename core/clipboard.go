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

// errSkip means the clipboard content is marked as sensitive (by a password manager, for example) and must not be synced.
var errSkip = errors.New("clipboard content excluded")

// ClipWatcher polls the system clipboard. The per-platform readClipboard / writeClipboard are in clipboard_*.go.
type ClipWatcher struct {
	a       *App
	mu      sync.Mutex
	last    string
	seq     int64    // the clipboard change counter seen last time (-1 on platforms without support)
	lastImg [32]byte // hash of the image last sent or written
	ok      bool
	idle    bool // the previous round was in passive mode or paused

	// the manual send page polls the clipboard every 2 seconds: images are cached by change counter, and unchanged content is not read or converted again
	peekSeq  int64
	peekSize int
	peekHash [32]byte
}

func newClipWatcher(a *App) *ClipWatcher {
	c := &ClipWatcher{a: a, ok: clipboardSupported(), seq: -1}
	if c.ok {
		c.last, _ = readClipboard() // content already in the clipboard at startup is not shared
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

// poll checks the clipboard once and sends newly copied text or images.
func (c *ClipWatcher) poll() {
	// nothing is sent in passive mode or while paused; on resume the current clipboard is marked as handled, so the content copied in between is not sent later
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
	// the clipboard changed but holds no text: it may be an image (a screenshot, or an image copied in a browser or image viewer)
	if changed && seq >= 0 && err == nil && txt == "" {
		img, _ := readClipboardImage()
		h := sha256.Sum256(img)
		if len(img) == 0 || len(img) > maxClipImage || h == c.lastImg {
			c.mu.Unlock()
			return
		}
		c.lastImg = h
		c.mu.Unlock()
		// files copied in Finder also carry an icon image; content marked as sensitive or copied in a blacklisted app is not sent
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
	// check only once when the content changed: if what was copied is a file (only files dragged in are shared) or the content is marked as sensitive, nothing is sent
	if skipClipboardChange() {
		return
	}
	// content copied in a blacklisted app is not sent
	if id, name := frontApp(); c.a.noteApp(id, name) {
		return
	}
	c.a.newText(txt, c.a.cfgSnapshot().Name, false)
}

// Apply writes remote content to the local clipboard and remembers it, so it is not broadcast again as new content.
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

// ApplyImage writes an image received from a remote peer to the local clipboard, so it is not sent out again as a newly copied image.
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
		c.lastImg = sha256.Sum256(img) // recognized even if the counter did not change
	}
	return err
}

// resync marks the current clipboard as handled. It is called when switching from passive back to active mode, to avoid sending the content copied while passive.
func (c *ClipWatcher) resync() {
	if !c.ok {
		return
	}
	c.mu.Lock()
	c.last, _ = readClipboard()
	c.seq = clipSeq()
	c.mu.Unlock()
}

// ClipboardState is the current state of the local clipboard, used by the passive mode "send current clipboard" action.
type ClipboardState struct {
	Text   string `json:"text"`             // preview (truncated)
	Length int    `json:"length"`           // full length (in characters)
	Image  int    `json:"image,omitempty"`  // when the clipboard holds an image: the image size in bytes
	Reason string `json:"reason,omitempty"` // why it cannot be sent
	Sent   bool   `json:"sent"`             // same as the newest item
}

// peekImage looks for an image in the clipboard (called when there is no text, the caller holds c.mu). It returns the size and the hash, size is -1 for an image that is too large.
func (c *ClipWatcher) peekImage() (size int, hash [32]byte) {
	seq := clipSeq()
	if seq < 0 {
		return 0, hash
	}
	if seq == c.peekSeq {
		return c.peekSize, c.peekHash
	}
	c.peekSeq, c.peekSize, c.peekHash = seq, 0, [32]byte{}
	if skipClipboardChange() { // files copied in Finder
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

// ClipboardState reads the current clipboard (without sending it).
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

// SendClipboard manually sends the text in the current clipboard (used in passive mode, also available in active mode).
func (a *App) SendClipboard() (*Item, error) {
	a.clip.mu.Lock()
	txt, reason := a.clip.read()
	if reason == "" {
		a.clip.last = txt // in active mode it is not sent automatically again
	}
	// when there is no text, check whether it is an image
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
		return l, nil // already the newest item, do not send it again
	}
	return a.newText(txt, a.cfgSnapshot().Name, false), nil
}

// ---- paused sync ----

// Paused reports whether syncing is paused: content copied here is not sent and received content is not written to the clipboard (it still shows up in the list).
func (a *App) Paused() bool {
	pu := a.cfgSnapshot().PausedUntil
	return pu == -1 || pu > nowMs()
}

// Pause pauses for a while; d < 0 means until it is resumed manually, d == 0 means resume.
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

// autoResume clears the paused state once the time is up (so the UI and the tray update in time).
func (a *App) autoResume() {
	if pu := a.cfgSnapshot().PausedUntil; pu > 0 && pu <= nowMs() {
		a.updateCfg(func(c *Config) { c.PausedUntil = 0 })
	}
}

// ---- app blacklist ----

// SourceApp is the app in the foreground when the content was copied.
type SourceApp struct {
	ID   string `json:"id"`   // bundle id on macOS, process name on Windows (chrome.exe for example)
	Name string `json:"name"` // display name
	Seen int64  `json:"seen"`
}

var (
	recentMu   sync.Mutex
	recentApps = map[string]*SourceApp{}
)

// noteApp records the most recent copy source and reports whether that app is on the blacklist.
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

// RecentApps returns the apps that copied content most recently, newest first.
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
