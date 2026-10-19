package main

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
