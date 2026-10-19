// Lanvia 桌面客户端：菜单栏 / 托盘常驻，不依赖浏览器。
//
//   - 点托盘图标（或按 ⌘⇧Y / Ctrl+Shift+Y）弹出快捷面板：输入即搜索，↑↓ 选择，回车写入剪贴板
//   - 右键托盘图标：最近内容一键复制、同步开关、开机启动、退出
//   - 预览：面板里按空格或 ⌘Y，长内容在独立的悬浮窗口里查看和复制；⌘E 编辑，可查看历史版本
//   - 主窗口：配对二维码、设置、完整历史
//
// 节点（core）运行在同一个进程里，窗口加载的是它在 localhost 上提供的页面。
package main

import (
	"embed"
	"fmt"
	"html"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"lanvia/core"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed assets
var assets embed.FS

// ⌘⇧V / Ctrl+Shift+V 在很多应用里是“粘贴为纯文本”，不抢；Y 在两个平台上都基本空闲。
const (
	hotkey     = "CmdOrCtrl+Shift+Y" // 快捷面板
	sendHotkey = "CmdOrCtrl+Shift+U" // 发送当前剪贴板（被动模式下最常用）
)

func hotkeyLabel(accel string) string {
	if runtime.GOOS == "darwin" {
		return map[string]string{hotkey: "⌘⇧Y", sendHotkey: "⌘⇧U"}[accel]
	}
	return strings.TrimPrefix(accel, "CmdOr") // Ctrl+Shift+Y
}

type desktop struct {
	node    *core.App
	app     *application.App
	tray    *application.SystemTray
	menu    *application.Menu
	panel   *application.WebviewWindow
	main    *application.WebviewWindow
	preview *application.WebviewWindow

	mu      sync.Mutex
	pending bool
}

func main() {
	node, err := core.Open(core.Options{})
	if err != nil {
		log.Fatal(err)
	}

	d := &desktop{node: node}
	d.app = application.New(application.Options{
		Name:        "Lanvia",
		Description: "局域网内多设备即时共享剪贴板与文件",
		// 页面由 core 在 localhost 上提供，这里不需要打包前端资源
		Assets: application.AssetOptions{Handler: http.NotFoundHandler()},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory, // 只在菜单栏，不占程序坞
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               "app.lanvia.desktop",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { d.showMain() },
		},
		OnShutdown: node.Close,
	})

	if err := node.Start(); err != nil {
		d.fatalWindow(err)
		return
	}
	d.build()
	if err := d.app.Run(); err != nil {
		log.Fatal(err)
	}
}

func (d *desktop) build() {
	base := d.node.LocalURL()
	d.panel = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "panel",
		Title:            "Lanvia",
		Width:            380,
		Height:           600,
		URL:              base + "/?desktop=panel",
		Frameless:        true,
		AlwaysOnTop:      true,
		Hidden:           true,
		DisableResize:    true,
		HideOnFocusLost:  true,
		HideOnEscape:     true,
		BackgroundColour: application.NewRGB(0xf2, 0xf5, 0xfa),
		Windows:          application.WindowsWindow{HiddenOnTaskbar: true},
	})
	d.main = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Lanvia",
		Width:     960,
		Height:    760,
		MinWidth:  420,
		MinHeight: 480,
		URL:       base + "/?desktop=main",
		Hidden:    true,
	})
	// 预览窗口像访达的“快速查看”：可拖动、可调整大小，默认悬浮在最上层
	d.preview = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:        "preview",
		Title:       "Lanvia 预览",
		Width:       760,
		Height:      620,
		MinWidth:    360,
		MinHeight:   240,
		URL:         base + "/?desktop=preview",
		Hidden:      true,
		AlwaysOnTop: true,
		// 不用 HideOnEscape：编辑时 Esc 是“取消编辑”，由网页自己决定何时关闭
	})
	// 关闭窗口只是隐藏，Lanvia 继续在后台同步
	for _, w := range []*application.WebviewWindow{d.panel, d.main, d.preview} {
		w := w
		w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			w.Hide()
			e.Cancel()
		})
	}

	// macOS 上 WebView 的 ⌘C / ⌘V / ⌘A 依赖“编辑”菜单；菜单栏应用看不到这个菜单，但快捷键需要它
	if runtime.GOOS == "darwin" {
		appMenu := d.app.NewMenu()
		appMenu.AddRole(application.AppMenu)
		appMenu.AddRole(application.EditMenu)
		d.app.Menu.SetApplicationMenu(appMenu)
	}

	d.tray = d.app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		icon, _ := assets.ReadFile("assets/tray-template.png")
		d.tray.SetTemplateIcon(icon)
	} else {
		icon, _ := assets.ReadFile("assets/tray.png")
		d.tray.SetIcon(icon)
	}
	d.tray.SetTooltip("Lanvia")
	d.tray.AttachWindow(d.panel).WindowOffset(6)
	d.menu = d.app.NewMenu()
	d.fillMenu()
	d.tray.SetMenu(d.menu)

	if err := d.app.GlobalShortcut.Register(hotkey, d.togglePanel); err != nil {
		log.Printf("无法注册快捷键 %s: %v", hotkey, err)
	}
	if err := d.app.GlobalShortcut.Register(sendHotkey, d.sendClipboard); err != nil {
		log.Printf("无法注册快捷键 %s: %v", sendHotkey, err)
	}

	// 网页里的“收起”“打开主窗口”“预览”
	d.node.SetDesktopHook(func(action, id string) bool {
		switch action {
		case "hide":
			d.panel.Hide()
		case "main":
			d.panel.Hide()
			d.showMain()
		case "preview", "edit", "history":
			if id == "" {
				return false
			}
			mode := map[string]string{"preview": "view", "edit": "edit", "history": "history"}[action]
			d.panel.Hide()
			d.preview.SetURL(base + "/?desktop=preview&id=" + id + "&mode=" + mode)
			d.preview.SetAlwaysOnTop(true)
			d.preview.Show()
			d.preview.Focus()
		case "hide-preview":
			d.preview.Hide()
		case "pin", "unpin":
			d.preview.SetAlwaysOnTop(action == "pin")
		default:
			return false
		}
		return true
	})
	d.node.OnChange(d.refreshMenu)

	// 第一次使用（还没有任何内容）时打开主窗口，方便扫码配对
	if len(d.node.Recent(1)) == 0 {
		d.showMain()
	}
}

func (d *desktop) togglePanel() {
	if d.panel.IsVisible() {
		d.panel.Hide()
		return
	}
	d.panel.Center()
	d.panel.Show()
	d.panel.Focus()
}

func (d *desktop) showMain() {
	d.main.Show()
	d.main.Focus()
}

// sendClipboard 手动发送当前剪贴板。没有窗口可以提示，所以在菜单栏图标旁边短暂显示结果。
func (d *desktop) sendClipboard() {
	msg := "已发送"
	if _, err := d.node.SendClipboard(); err != nil {
		msg = err.Error()
		log.Printf("发送剪贴板: %v", err)
	}
	d.flash(msg)
}

func (d *desktop) flash(msg string) {
	if runtime.GOOS != "darwin" { // Windows 托盘没有文字标签，用提示文字代替
		d.tray.SetTooltip("Lanvia · " + msg)
		time.AfterFunc(3*time.Second, func() { d.tray.SetTooltip("Lanvia") })
		return
	}
	if r := []rune(msg); len(r) > 12 {
		msg = string(r[:12]) + "…"
	}
	d.tray.SetLabel(" " + msg)
	time.AfterFunc(2*time.Second, func() { d.tray.SetLabel("") })
}

// refreshMenu 合并短时间内的多次变化，避免频繁重建菜单。
func (d *desktop) refreshMenu() {
	d.mu.Lock()
	if d.pending {
		d.mu.Unlock()
		return
	}
	d.pending = true
	d.mu.Unlock()
	time.AfterFunc(300*time.Millisecond, func() {
		d.mu.Lock()
		d.pending = false
		d.mu.Unlock()
		d.menu.Clear()
		d.fillMenu()
		d.menu.Update()
	})
}

func (d *desktop) fillMenu() {
	m := d.menu
	m.Add(fmt.Sprintf("Lanvia · %s", d.node.Name())).SetEnabled(false)
	m.AddSeparator()

	recent := d.node.Recent(6)
	if len(recent) == 0 {
		m.Add("还没有内容").SetEnabled(false)
	}
	for _, it := range recent {
		it := it
		if it.Kind == "text" {
			m.Add(menuLabel(it.Text)).OnClick(func(*application.Context) {
				if err := d.node.CopyToClipboard(it.ID); err != nil {
					log.Printf("复制失败: %v", err)
				}
			})
		} else {
			m.Add("📄 " + menuLabel(it.Name)).OnClick(func(*application.Context) {
				if p, err := d.node.SaveFile(it.ID); err == nil {
					core.RevealFile(p)
				}
			})
		}
	}
	m.AddSeparator()

	s := d.node.Settings()
	m.AddRadio("主动模式 · 复制即发送", s.Watch).OnClick(func(*application.Context) { d.node.SetWatch(true) })
	m.AddRadio("被动模式 · 手动发送", !s.Watch).OnClick(func(*application.Context) { d.node.SetWatch(false) })
	m.Add("发送当前剪贴板    " + hotkeyLabel(sendHotkey)).OnClick(func(*application.Context) { d.sendClipboard() })
	m.AddSeparator()
	m.AddCheckbox("自动写入本机剪贴板", s.AutoApply).OnClick(func(c *application.Context) {
		d.node.SetAutoApply(c.IsChecked())
	})
	auto, _ := d.app.Autostart.IsEnabled()
	m.AddCheckbox("开机时启动", auto).OnClick(func(c *application.Context) {
		var err error
		if c.IsChecked() {
			err = d.app.Autostart.Enable()
		} else {
			err = d.app.Autostart.Disable()
		}
		if err != nil {
			log.Printf("设置开机启动失败: %v", err)
		}
	})
	m.AddSeparator()

	m.Add("快捷面板    " + hotkeyLabel(hotkey)).OnClick(func(*application.Context) { d.togglePanel() })
	m.Add("打开主窗口…").OnClick(func(*application.Context) { d.showMain() })
	m.Add("配对码  " + d.node.Code()).SetEnabled(false)
	m.AddSeparator()
	m.Add("退出 Lanvia").OnClick(func(*application.Context) { d.app.Quit() })
}

// menuLabel 取第一行并截断，菜单项太长会被系统撑得很宽。
func menuLabel(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i]) + " …"
	}
	r := []rune(s)
	if len(r) > 32 {
		s = string(r[:32]) + "…"
	}
	if s == "" {
		s = "（空白）"
	}
	return s
}

// fatalWindow 在端口被占用等情况下给出可读的提示，而不是静默退出。
func (d *desktop) fatalWindow(err error) {
	msg := fmt.Sprintf(`<body style="font:15px/1.6 -apple-system,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;padding:28px">
<h2 style="margin:0 0 12px">Lanvia 无法启动</h2><p>%s</p>
<p style="color:#666">如果命令行版 Lanvia 正在运行，请先退出它；两者不能同时使用同一个端口。</p></body>`, html.EscapeString(err.Error()))
	d.app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "Lanvia", Width: 520, Height: 260, HTML: msg})
	d.app.Run()
}
