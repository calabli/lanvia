package core

import (
	"errors"
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
	for {
		time.Sleep(500 * time.Millisecond)
		if !c.a.cfgSnapshot().Watch {
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
