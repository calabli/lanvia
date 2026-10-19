package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"lanvia/core"
	"lanvia/internal/update"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 自动更新：启动后和每 6 小时检查一次；有新版时托盘菜单显示“更新到 vX.Y.Z”，并弹出一次通知。
// 下载后用发布里的 SHA256SUMS.txt 校验，替换当前程序，再由后台脚本在本程序退出后重新打开。

type updater struct {
	d        *desktop
	mu       sync.Mutex
	latest   *update.Release // 比当前版本新的发布
	busy     bool
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

func (u *updater) loop() {
	time.Sleep(30 * time.Second)
	for {
		u.check(false)
		time.Sleep(6 * time.Hour)
	}
}

func (u *updater) check(manual bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, err := update.Latest(ctx, apiURL())
	if err != nil {
		if manual {
			u.d.flash(u.d.tr("无法检查更新"))
		}
		log.Printf("检查更新: %v", err)
		return
	}
	if !update.Newer(rel.Tag, core.Version) || update.AssetName(runtime.GOOS, runtime.GOARCH) == "" {
		if manual {
			u.d.flash(u.d.tr("已是最新版本"))
		}
		return
	}
	u.mu.Lock()
	u.latest = rel
	first := u.notified != rel.Tag
	u.notified = rel.Tag
	u.mu.Unlock()
	u.d.refreshMenu()
	if first {
		u.d.notify.simple(fmt.Sprintf(u.d.tr("有新版本 %s"), rel.Tag), fmt.Sprintf(u.d.tr("点击托盘菜单里的“更新到 %s”即可安装"), rel.Tag))
	}
}

// menuItem 把“更新到 vX”或“检查更新”加到托盘菜单。
func (u *updater) menuItem(m *application.Menu) {
	u.mu.Lock()
	rel, busy := u.latest, u.busy
	u.mu.Unlock()
	switch {
	case busy:
		m.Add(u.d.tr("正在更新…")).SetEnabled(false)
	case rel != nil:
		m.Add(fmt.Sprintf(u.d.tr("更新到 %s"), rel.Tag)).OnClick(func(*application.Context) { go u.install() })
	default:
		m.Add(u.d.tr("检查更新")).OnClick(func(*application.Context) { go u.check(true) })
	}
}

func (u *updater) install() {
	u.mu.Lock()
	rel := u.latest
	if rel == nil || u.busy {
		u.mu.Unlock()
		return
	}
	u.busy = true
	u.mu.Unlock()
	u.d.refreshMenu()
	err := u.doInstall(rel)
	u.mu.Lock()
	u.busy = false
	u.mu.Unlock()
	if err != nil {
		log.Printf("更新失败: %v", err)
		u.d.notify.simple(fmt.Sprintf(u.d.tr("更新失败：%s"), err.Error()), "")
		u.d.refreshMenu()
		return
	}
	u.d.app.Quit() // 后台脚本会在退出后打开新版
}

func (u *updater) doInstall(rel *update.Release) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	name := update.AssetName(runtime.GOOS, runtime.GOARCH)
	target := exe
	if runtime.GOOS == "darwin" {
		if target = update.AppBundle(exe); target == "" {
			return fmt.Errorf("只有从 Lanvia.app 运行时才能自动更新")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	// 下载到目标所在目录，替换时只需改名（跨磁盘无法改名）
	pkg, err := update.Download(ctx, rel, name, filepath.Dir(target))
	if err != nil {
		return err
	}
	defer os.Remove(pkg)
	if runtime.GOOS == "darwin" {
		err = update.InstallMac(pkg, target)
	} else {
		err = update.InstallWindows(pkg, target)
	}
	if err != nil {
		return err
	}
	log.Printf("已更新到 %s，正在重新打开", rel.Tag)
	return update.Relaunch(target)
}
