package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"lanvia/core"
	"lanvia/internal/update"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 检查更新与自动更新：启动后不久和每 6 小时检查一次，也可以在设置页或托盘菜单里手动检查。
// 有新版本时：
//   - 开着“自动更新”：在后台下载并校验，等到没有在用 Lanvia 窗口时自动安装、重新打开，之后通知“已更新到 vX”；
//   - 关着：通知一次，托盘菜单和设置页里显示“更新到 vX”，由用户决定什么时候更新。
// 下载后用发布里的 SHA256SUMS.txt 校验；macOS 上没有权限直接替换时（用安装器装的旧版本），会弹出系统的密码对话框。

type updater struct {
	d        *desktop
	mu       sync.Mutex
	st       core.UpdateState
	latest   *update.Release // 比当前版本新、且有本平台安装包的发布
	pkg      string          // 已下载并校验的安装包
	pkgTag   string
	busy     bool // 正在下载或安装
	notified string
}

func newUpdater(d *desktop) *updater {
	if runtime.GOOS == "windows" {
		if exe, err := os.Executable(); err == nil {
			update.CleanupOld(exe) // 上次更新留下的旧版
		}
	}
	return &updater{d: d}
}

func apiURL() string {
	if u := os.Getenv("LANVIA_UPDATE_URL"); u != "" { // 测试用
		return u
	}
	return update.DefaultAPI
}

// publish 把状态推给设置页，并刷新托盘菜单。调用方不能持有 u.mu。
func (u *updater) publish(fn func(s *core.UpdateState)) {
	u.mu.Lock()
	if fn != nil {
		fn(&u.st)
	}
	st := u.st
	u.mu.Unlock()
	u.d.node.SetUpdateState(st)
	u.d.refreshMenu()
}

func (u *updater) auto() bool { return u.d.node.Settings().AutoUpdate }

func (u *updater) loop() {
	u.publish(nil)
	// 刚自动更新过：告诉用户已经是新版本了
	if prev := u.d.node.NoteVersion(); prev != "" && prev != core.Version && update.Newer(core.Version, prev) {
		u.d.notify.simple(fmt.Sprintf(u.d.tr("已更新到 %s"), "v"+core.Version), u.d.tr("Lanvia 已经自动更新并重新打开"))
	}
	time.Sleep(20 * time.Second)
	go u.autoInstallLoop()
	for {
		u.check(false)
		time.Sleep(6 * time.Hour)
	}
}

// autoInstallLoop：开着自动更新、安装包已经下载好时，等到没有在用 Lanvia 窗口再安装，免得打断正在做的事。
func (u *updater) autoInstallLoop() {
	for range time.Tick(time.Minute) {
		u.mu.Lock()
		ready := u.pkg != "" && !u.busy
		u.mu.Unlock()
		if ready && u.auto() && !u.d.anyWindowFocused() {
			u.install()
		}
	}
}

func (u *updater) check(manual bool) {
	u.mu.Lock()
	if u.busy || u.st.Checking {
		u.mu.Unlock()
		return
	}
	u.mu.Unlock()
	u.publish(func(s *core.UpdateState) { s.Checking, s.Error = true, "" })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, err := update.Latest(ctx, apiURL())
	if err != nil {
		log.Printf("检查更新: %v", err)
		u.publish(func(s *core.UpdateState) { s.Checking, s.Error = false, u.d.tr("无法检查更新，请检查网络") })
		if manual {
			u.d.flash(u.d.tr("无法检查更新"))
		}
		return
	}
	name := update.AssetName(runtime.GOOS, runtime.GOARCH)
	// 有的版本只发布了部分平台：没有本平台的安装包就当作没有新版本
	if !update.Newer(rel.Tag, core.Version) || !rel.Has(name) {
		u.mu.Lock()
		u.latest = nil
		u.mu.Unlock()
		u.publish(func(s *core.UpdateState) {
			s.Checking, s.Latest, s.Notes, s.URL, s.Ready, s.CheckedAt = false, "", "", "", false, time.Now().UnixMilli()
		})
		if manual {
			u.d.flash(u.d.tr("已是最新版本"))
		}
		return
	}
	u.mu.Lock()
	u.latest = rel
	first := u.notified != rel.Tag
	u.notified = rel.Tag
	stale := u.pkg != "" && u.pkgTag != rel.Tag
	u.mu.Unlock()
	if stale { // 下载好的是更旧的新版本，作废
		u.dropPackage()
	}
	u.publish(func(s *core.UpdateState) {
		s.Checking, s.Latest, s.Notes, s.URL, s.CheckedAt = false, rel.Tag, rel.Body, rel.URL, time.Now().UnixMilli()
	})
	if u.auto() {
		go u.download() // 后台下载，下好后找合适的时机安装
		return
	}
	if first || manual {
		u.d.notify.simple(fmt.Sprintf(u.d.tr("有新版本 %s"), rel.Tag), u.d.tr("在设置或托盘菜单里点“立即更新”即可安装"))
	}
}

func (u *updater) target() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		app := update.AppBundle(exe)
		if app == "" {
			return "", errors.New(u.d.tr("只有从 Lanvia.app 运行时才能自动更新"))
		}
		return app, nil
	}
	return exe, nil
}

// download 下载并校验安装包，返回是否成功（已经下载好时直接返回成功）。
func (u *updater) download() bool {
	u.mu.Lock()
	rel := u.latest
	if rel == nil || u.busy {
		u.mu.Unlock()
		return false
	}
	if u.pkg != "" && u.pkgTag == rel.Tag {
		u.mu.Unlock()
		return true
	}
	u.busy = true
	u.mu.Unlock()
	defer func() { u.mu.Lock(); u.busy = false; u.mu.Unlock() }()

	target, err := u.target()
	if err != nil {
		u.publish(func(s *core.UpdateState) { s.Error = err.Error() })
		return false
	}
	u.publish(func(s *core.UpdateState) { s.Downloading, s.Progress, s.Error = true, 0, "" })
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	name := update.AssetName(runtime.GOOS, runtime.GOARCH)
	lastPct := -1
	pkg, err := update.Download(ctx, rel, name, update.DownloadDir(target), func(done, total int64) {
		if pct := int(done * 100 / max(total, 1)); total > 0 && pct != lastPct { // 百分比变了才推送
			lastPct = pct
			u.publish(func(s *core.UpdateState) { s.Progress = float64(done) / float64(total) })
		}
	})
	if err != nil {
		log.Printf("下载更新失败: %v", err)
		u.publish(func(s *core.UpdateState) {
			s.Downloading, s.Error = false, fmt.Sprintf(u.d.tr("下载失败：%s"), err.Error())
		})
		return false
	}
	u.mu.Lock()
	u.pkg, u.pkgTag = pkg, rel.Tag
	u.mu.Unlock()
	u.publish(func(s *core.UpdateState) { s.Downloading, s.Progress, s.Ready = false, 1, true })
	return true
}

func (u *updater) dropPackage() {
	u.mu.Lock()
	if u.pkg != "" {
		os.Remove(u.pkg)
	}
	u.pkg, u.pkgTag = "", ""
	u.mu.Unlock()
	u.publish(func(s *core.UpdateState) { s.Ready = false })
}

// install 安装新版本（还没下载就先下载），成功后退出，由后台脚本重新打开新版。
func (u *updater) install() {
	if !u.download() {
		return
	}
	u.mu.Lock()
	if u.busy {
		u.mu.Unlock()
		return
	}
	u.busy = true
	pkg, tag := u.pkg, u.pkgTag
	u.mu.Unlock()
	u.publish(func(s *core.UpdateState) { s.Installing, s.Error = true, "" })
	err := u.doInstall(pkg)
	u.mu.Lock()
	u.busy = false
	u.mu.Unlock()
	if err != nil {
		log.Printf("更新失败: %v", err)
		msg := fmt.Sprintf(u.d.tr("更新失败：%s"), err.Error())
		if errors.Is(err, update.ErrCanceled) {
			msg = u.d.tr("已取消更新")
		}
		u.publish(func(s *core.UpdateState) { s.Installing, s.Error = false, msg })
		u.d.notify.simple(msg, "")
		return
	}
	log.Printf("已更新到 %s，正在重新打开", tag)
	u.d.app.Quit() // 后台脚本会在退出后打开新版
}

func (u *updater) doInstall(pkg string) error {
	target, err := u.target()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		err = update.InstallMacAuto(pkg, target)
	} else {
		err = update.InstallWindows(pkg, target)
	}
	if err != nil {
		return err
	}
	u.mu.Lock()
	u.pkg = "" // 已经装上，安装包已被用掉或会被删除
	u.mu.Unlock()
	os.Remove(pkg)
	return update.Relaunch(target)
}

// menuItem 把“更新到 vX”或“检查更新”加到托盘菜单。
func (u *updater) menuItem(m *application.Menu) {
	u.mu.Lock()
	st, rel := u.st, u.latest
	u.mu.Unlock()
	switch {
	case st.Installing:
		m.Add(u.d.tr("正在更新…")).SetEnabled(false)
	case st.Downloading:
		m.Add(fmt.Sprintf(u.d.tr("正在下载更新 %d%%"), int(st.Progress*100))).SetEnabled(false)
	case rel != nil:
		m.Add(fmt.Sprintf(u.d.tr("更新到 %s"), rel.Tag)).OnClick(func(*application.Context) { go u.install() })
	case st.Checking:
		m.Add(u.d.tr("正在检查更新…")).SetEnabled(false)
	default:
		m.Add(u.d.tr("检查更新")).OnClick(func(*application.Context) { go u.check(true) })
	}
}
