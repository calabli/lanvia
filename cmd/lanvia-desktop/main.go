// Lanvia 桌面客户端：菜单栏 / 托盘常驻，不依赖浏览器。
//
//   - 点托盘图标弹出快捷面板：输入即搜索，↑↓ 选择，回车写入剪贴板（不注册全局快捷键，避免和其他应用冲突）
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

type desktop struct {
	node    *core.App
	app     *application.App
	tray    *application.SystemTray
	menu    *application.Menu
	panel   *application.WebviewWindow
	main    *application.WebviewWindow
	preview *application.WebviewWindow
	winMu   sync.Mutex // 保护按需创建 main / preview
	notify  *notifier
	update  *updater

	mu      sync.Mutex
	pending bool
}

func main() {
	node, err := core.Open(core.Options{})
	if err != nil {
		log.Fatal(err)
	}

	d := &desktop{node: node}
	d.notify = newNotifier(d)
	d.update = newUpdater(d)
	d.app = application.New(application.Options{
		Services:    []application.Service{application.NewService(d.notify.ns)},
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
	d.panel = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "panel",
		Title:            "Lanvia",
		Width:            380,
		Height:           600,
		URL:              d.node.LocalURL() + "/?desktop=panel",
		Frameless:        true,
		AlwaysOnTop:      true,
		Hidden:           true,
		DisableResize:    true,
		HideOnFocusLost:  true,
		HideOnEscape:     true,
		BackgroundColour: application.NewRGB(0xf2, 0xf5, 0xfa),
		Windows:          application.WindowsWindow{HiddenOnTaskbar: true},
	})
	// 主窗口和预览窗口第一次用到时才创建（见 mainWindow / previewWindow），
	// 启动时只有快捷面板：它要挂在托盘图标上。每个窗口都是一个完整的网页，常驻会占内存和 CPU。
	d.hideOnClose(d.panel)

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

	// 网页里的“收起”“打开主窗口”“预览”
	d.node.SetDesktopHook(func(action, id string) bool {
		switch action {
		case "hide":
			d.panel.Hide()
		case "main":
			d.panel.Hide()
			d.showMain()
		case "preview", "edit", "history", "comments":
			if id == "" {
				return false
			}
			d.openPreviewMode(id, map[string]string{"preview": "view", "edit": "edit", "history": "history", "comments": "comments"}[action])
		case "update":
			go d.update.install()
		case "hide-preview":
			if d.preview != nil {
				d.preview.Hide()
			}
		case "pin", "unpin":
			if d.preview != nil {
				d.preview.SetAlwaysOnTop(action == "pin")
			}
		default:
			return false
		}
		return true
	})
	core.SetLanguage(d.lang()) // 日志跟随界面语言
	d.node.OnChange(func() { core.SetLanguage(d.lang()); d.refreshMenu() })
	d.node.OnIncoming(d.notify.incoming)
	go d.update.loop()

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

// hideOnClose：关闭窗口只是隐藏，Lanvia 继续在后台同步，下次打开也不用重新加载。
func (d *desktop) hideOnClose(w *application.WebviewWindow) {
	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		w.Hide()
		e.Cancel()
	})
}

func (d *desktop) mainWindow() *application.WebviewWindow {
	d.winMu.Lock()
	defer d.winMu.Unlock()
	if d.main == nil {
		d.main = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:      "main",
			Title:     "Lanvia",
			Width:     960,
			Height:    760,
			MinWidth:  420,
			MinHeight: 480,
			URL:       d.node.LocalURL() + "/?desktop=main",
		})
		d.hideOnClose(d.main)
	}
	return d.main
}

// previewWindow 返回预览窗口；第一次创建时直接打开 url，返回 true。
func (d *desktop) previewWindow(url string) (*application.WebviewWindow, bool) {
	d.winMu.Lock()
	defer d.winMu.Unlock()
	if d.preview != nil {
		return d.preview, false
	}
	// 预览窗口像访达的“快速查看”：可拖动、可调整大小，默认悬浮在最上层
	d.preview = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:        "preview",
		Title:       "Lanvia 预览",
		Width:       760,
		Height:      620,
		MinWidth:    360,
		MinHeight:   240,
		URL:         url,
		AlwaysOnTop: true,
		// 不用 HideOnEscape：编辑时 Esc 是“取消编辑”，由网页自己决定何时关闭
	})
	d.hideOnClose(d.preview)
	return d.preview, true
}

func (d *desktop) openPreviewMode(id, mode string) {
	d.panel.Hide()
	url := d.node.LocalURL() + "/?desktop=preview&id=" + id + "&mode=" + mode
	w, created := d.previewWindow(url)
	if !created {
		w.SetURL(url)
	}
	w.SetAlwaysOnTop(true)
	w.Show()
	w.Focus()
}

func (d *desktop) openPreview(id string) { d.openPreviewMode(id, "view") }

// anyWindowFocused：有 Lanvia 窗口在前台时，页面里已经有提示，不再弹系统通知。
func (d *desktop) anyWindowFocused() bool {
	for _, w := range []*application.WebviewWindow{d.panel, d.main, d.preview} {
		if w != nil && w.IsVisible() && w.IsFocused() {
			return true
		}
	}
	return false
}

func (d *desktop) showMain() {
	w := d.mainWindow()
	w.Show()
	w.Focus()
}

// sendClipboard 手动发送当前剪贴板。没有窗口可以提示，所以在菜单栏图标旁边短暂显示结果。
func (d *desktop) sendClipboard() {
	msg := d.tr("已发送")
	if _, err := d.node.SendClipboard(); err != nil {
		msg = d.tr(err.Error())
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
		m.Add(d.tr("还没有内容")).SetEnabled(false)
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
	// 收藏的常用片段：点一下就复制
	if favs := d.node.Favorites(); len(favs) > 0 {
		sub := m.AddSubmenu(d.tr("收藏"))
		for _, it := range favs {
			it := it
			label := it.Title
			if label == "" {
				label = it.Text
				if it.Kind == "file" {
					label = it.Name
				}
			}
			sub.Add(menuLabel(label)).OnClick(func(*application.Context) {
				if it.Kind == "text" {
					d.node.CopyToClipboard(it.ID)
					d.flash(d.tr("已复制"))
				} else if p, err := d.node.SaveFile(it.ID); err == nil {
					core.RevealFile(p)
				}
			})
		}
	}
	m.AddSeparator()

	s := d.node.Settings()
	m.AddRadio(d.tr("主动模式 · 复制即发送"), s.Watch).OnClick(func(*application.Context) { d.node.SetWatch(true) })
	m.AddRadio(d.tr("被动模式 · 手动发送"), !s.Watch).OnClick(func(*application.Context) { d.node.SetWatch(false) })
	m.Add(d.tr("发送当前剪贴板")).OnClick(func(*application.Context) { d.sendClipboard() })
	// 暂停同步：本机复制的不发送，收到的不写入剪贴板
	if until := d.node.PausedUntil(); until != 0 {
		label := d.tr("恢复同步（已暂停）")
		if until > 0 {
			label = fmt.Sprintf(d.tr("恢复同步（暂停到 %s）"), time.UnixMilli(until).Format("15:04"))
		}
		m.Add(label).OnClick(func(*application.Context) { d.node.Pause(0) })
	} else {
		ps := m.AddSubmenu(d.tr("暂停同步"))
		ps.Add(d.tr("15 分钟")).OnClick(func(*application.Context) { d.node.Pause(15 * time.Minute) })
		ps.Add(d.tr("1 小时")).OnClick(func(*application.Context) { d.node.Pause(time.Hour) })
		ps.Add(d.tr("直到手动恢复")).OnClick(func(*application.Context) { d.node.Pause(-1) })
	}
	m.AddSeparator()
	m.AddCheckbox(d.tr("自动写入本机剪贴板"), s.AutoApply).OnClick(func(c *application.Context) {
		d.node.SetAutoApply(c.IsChecked())
	})
	auto, _ := d.app.Autostart.IsEnabled()
	m.AddCheckbox(d.tr("开机时启动"), auto).OnClick(func(c *application.Context) {
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

	m.Add(d.tr("打开快捷面板")).OnClick(func(*application.Context) { d.togglePanel() })
	m.Add(d.tr("打开主窗口…")).OnClick(func(*application.Context) { d.showMain() })
	m.Add(d.tr("配对码  ") + d.node.Code()).SetEnabled(false)
	m.AddSeparator()
	d.update.menuItem(m)
	m.Add(d.tr("退出 Lanvia")).OnClick(func(*application.Context) { d.app.Quit() })
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
