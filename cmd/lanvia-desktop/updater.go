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

// Update checking and automatic updates: a check runs shortly after startup and every 6 hours, and it can also be triggered manually from the settings page or the tray menu.
// When a new version is available:
//   - With "auto update" on: it is downloaded and verified in the background, then installed and reopened once no Lanvia window is in use, followed by the "Updated to vX" notification;
//   - With it off: a single notification is sent, the tray menu and the settings page show "Update to vX", and the user decides when to update.
// After downloading, it is verified with the SHA256SUMS.txt from the release; on macOS, when there is no permission to replace the app directly (an old version installed with the installer), the system password dialog pops up.

type updater struct {
	d        *desktop
	mu       sync.Mutex
	st       core.UpdateState
	latest   *update.Release // a release newer than the current version that has an installer for this platform
	pkg      string          // the downloaded and verified installer
	pkgTag   string
	busy     bool // downloading or installing
	notified string
	// failedTag is a version whose automatic install failed (e.g. the user cancelled the password dialog).
	// It is not retried automatically, so the dialog and notification don't pop up every minute;
	// "Update now" still works, and automatic installs resume once a newer version is released.
	failedTag string
}

func newUpdater(d *desktop) *updater {
	if runtime.GOOS == "windows" {
		if exe, err := os.Executable(); err == nil {
			update.CleanupOld(exe) // the old version left over from the last update
		}
	}
	return &updater{d: d}
}

func apiURL() string {
	if u := os.Getenv("LANVIA_UPDATE_URL"); u != "" { // for tests
		return u
	}
	return update.DefaultAPI
}

// publish pushes the state to the settings page and refreshes the tray menu. The caller must not hold u.mu.
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
	// Just auto-updated: tell the user it is already the new version
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

// autoInstallLoop: with auto update on and the installer already downloaded, wait until no Lanvia window is in use before installing, so that it does not interrupt what is being done.
func (u *updater) autoInstallLoop() {
	for range time.Tick(time.Minute) {
		u.mu.Lock()
		ready := u.pkg != "" && !u.busy && u.pkgTag != u.failedTag
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
	rel, err := update.Find(ctx, apiURL())
	if err != nil {
		log.Printf("检查更新: %v", err)
		u.publish(func(s *core.UpdateState) { s.Checking, s.Error = false, u.d.tr("无法检查更新，请检查网络") })
		if manual {
			u.d.flash(u.d.tr("无法检查更新"))
		}
		return
	}
	name := update.AssetName(runtime.GOOS, runtime.GOARCH)
	// Some releases only cover some platforms: without an installer for this platform there is no new version
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
	if rel.Body == "" { // a release found through the web page has no release notes, so try to fill them in from the API (a failure does not block the update)
		rel.Body = update.Notes(ctx, apiURL(), rel.Tag)
	}
	u.mu.Lock()
	u.latest = rel
	first := u.notified != rel.Tag
	u.notified = rel.Tag
	stale := u.pkg != "" && u.pkgTag != rel.Tag
	u.mu.Unlock()
	if stale { // the downloaded package is an older new version, drop it
		u.dropPackage()
	}
	u.publish(func(s *core.UpdateState) {
		s.Checking, s.Latest, s.Notes, s.URL, s.CheckedAt = false, rel.Tag, rel.Body, rel.URL, time.Now().UnixMilli()
	})
	if u.auto() {
		go u.download() // download in the background, install it at a suitable time once it is ready
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

// download downloads and verifies the installer, and reports whether it succeeded (it returns success right away when the package is already downloaded).
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
		if pct := int(done * 100 / max(total, 1)); total > 0 && pct != lastPct { // only push when the percentage changed
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

// install installs the new version (it downloads it first if that has not happened yet) and quits on success; a background script reopens the new version.
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
	if err != nil {
		u.failedTag = tag
	}
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
	u.d.app.Quit() // the background script opens the new version after quitting
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
	u.pkg = "" // it is installed, the package has been used up or will be deleted
	u.mu.Unlock()
	os.Remove(pkg)
	return update.Relaunch(target)
}

// menuItem adds "Update to vX" or "Check for updates" to the tray menu.
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
