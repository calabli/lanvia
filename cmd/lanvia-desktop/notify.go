package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"lanvia/core"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// System notifications: they warn when another device sends content. Nothing pops up while a window is in the foreground (the page already shows a hint),
// and items received in a short time are merged into one, to avoid flooding the screen.

const (
	catText = "lanvia-text"
	catFile = "lanvia-file"
)

type notifier struct {
	d       *desktop
	ns      *notifications.NotificationService
	once    sync.Once
	ready   bool
	mu      sync.Mutex
	pending []core.Item
	timer   *time.Timer
}

func newNotifier(d *desktop) *notifier {
	n := &notifier{d: d, ns: notifications.New()}
	n.ns.OnNotificationResponse(n.onResponse)
	return n
}

// setup requests the permission and registers the buttons on the first notification (it can only be called after the service has started).
func (n *notifier) setup() {
	n.once.Do(func() {
		ok, err := n.ns.CheckNotificationAuthorization()
		if err == nil && !ok {
			ok, err = n.ns.RequestNotificationAuthorization()
		}
		if err != nil || !ok {
			log.Printf("系统通知不可用: %v", err)
			return
		}
		n.ns.RegisterNotificationCategory(notifications.NotificationCategory{
			ID: catText, Actions: []notifications.NotificationAction{{ID: "copy", Title: n.d.tr("复制")}},
		})
		n.ns.RegisterNotificationCategory(notifications.NotificationCategory{
			ID: catFile, Actions: []notifications.NotificationAction{{ID: "reveal", Title: n.d.tr("在文件夹中显示")}},
		})
		n.ready = true
	})
}

// incoming is called by core when another device sends new content.
func (n *notifier) incoming(it core.Item) {
	if !n.d.node.Settings().Notify || n.d.anyWindowFocused() {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pending = append(n.pending, it)
	if n.timer == nil {
		n.timer = time.AfterFunc(800*time.Millisecond, n.flush)
	}
}

func (n *notifier) flush() {
	n.mu.Lock()
	items := n.pending
	n.pending, n.timer = nil, nil
	n.mu.Unlock()
	if len(items) == 0 {
		return
	}
	n.setup()
	if !n.ready {
		return
	}
	tr := n.d.tr
	opts := notifications.NotificationOptions{ID: "lanvia-" + items[len(items)-1].ID, ThreadID: "lanvia"}
	if len(items) > 1 {
		from := items[len(items)-1].From
		opts.Title = fmt.Sprintf(tr("收到 %d 条新内容"), len(items))
		opts.Body = fmt.Sprintf(tr("最新来自 %s"), from)
		opts.Data = map[string]any{"id": items[len(items)-1].ID}
		n.ns.SendNotification(opts)
		return
	}
	it := items[0]
	opts.Title = fmt.Sprintf(tr("来自 %s"), it.From)
	opts.Data = map[string]any{"id": it.ID}
	if it.Kind == "file" {
		opts.Body = fmt.Sprintf("📄 %s", it.Name)
		opts.CategoryID = catFile
	} else if it.Kind == "todo" {
		opts.Body = notifyPreview("☑ " + it.PlainText())
		opts.CategoryID = catText
	} else {
		opts.Body = notifyPreview(it.Text)
		opts.CategoryID = catText
		if s := n.d.node.Settings(); s.AutoApply && !n.d.node.Paused() {
			opts.Subtitle = tr("已写入剪贴板")
		}
	}
	if err := n.ns.SendNotificationWithActions(opts); err != nil {
		log.Printf("发送通知失败: %v", err)
	}
}

// simple sends a notification without buttons (for example when a new version is available).
func (n *notifier) simple(title, body string) {
	n.setup()
	if !n.ready {
		return
	}
	n.ns.SendNotification(notifications.NotificationOptions{ID: fmt.Sprintf("lanvia-%d", time.Now().UnixNano()), Title: title, Body: body})
}

func (n *notifier) onResponse(r notifications.NotificationResult) {
	if r.Error != nil {
		return
	}
	id, _ := r.Response.UserInfo["id"].(string)
	if id == "" {
		n.d.showMain()
		return
	}
	switch r.Response.ActionIdentifier {
	case "copy":
		n.d.node.CopyToClipboard(id)
	case "reveal":
		if p, err := n.d.node.SaveFile(id); err == nil {
			core.RevealFile(p)
		}
	default: // the notification itself was clicked: open the preview
		n.d.openPreview(id)
	}
}

func notifyPreview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}
