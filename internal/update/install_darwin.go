//go:build darwin

package update

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
)

// 用安装器装的 Lanvia 归 root 所有（v0.13.0 的安装器），标准账户也改不了“应用程序”：
// 这些情况下不能直接替换，改为通过系统的密码对话框以管理员身份替换，并把新版本交还给当前用户，
// 这样以后的更新就不用再输密码了。

func ownedByMe(path string) bool {
	st, err := os.Lstat(path)
	if err != nil {
		return false
	}
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == os.Getuid()
}

func writable(dir string) bool { return syscall.Access(dir, 2 /* W_OK */) == nil }

// CanReplace 判断当前用户能不能直接替换 appPath（不需要管理员权限）。
func CanReplace(appPath string) bool {
	return writable(filepath.Dir(appPath)) && ownedByMe(appPath) && ownedByMe(filepath.Join(appPath, "Contents")) &&
		ownedByMe(filepath.Join(appPath, "Contents", "MacOS", "Lanvia"))
}

// DownloadDir 是下载安装包的目录：能直接替换时放在 App 旁边（替换只需改名），否则放在用户缓存目录。
func DownloadDir(appPath string) string {
	if CanReplace(appPath) {
		return filepath.Dir(appPath)
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "app.lanvia.desktop", "update")
	os.MkdirAll(dir, 0o700)
	return dir
}

// InstallMacAuto 能直接替换就直接替换；否则弹出系统的密码对话框，以管理员身份替换。
func InstallMacAuto(zipPath, appPath string) error {
	if CanReplace(appPath) {
		return InstallMac(zipPath, appPath)
	}
	return installMacAdmin(zipPath, appPath)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func appleScriptQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func installMacAdmin(zipPath, appPath string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	// 解压 → 检查 → 旧版改名备份 → 新版就位 → 交还给当前用户 → 删除备份；中途失败就恢复旧版
	script := fmt.Sprintf(`set -e
t=$(/usr/bin/mktemp -d /tmp/lanvia-update.XXXXXX)
trap '/bin/rm -rf "$t"' EXIT
/usr/bin/ditto -x -k %[1]s "$t"
test -x "$t/Lanvia.app/Contents/MacOS/Lanvia"
/bin/rm -rf %[2]s.old
/bin/mv %[2]s %[2]s.old
if ! /bin/mv "$t/Lanvia.app" %[2]s; then /bin/mv %[2]s.old %[2]s; exit 1; fi
/usr/sbin/chown -R %[3]s %[2]s
/bin/rm -rf %[2]s.old`, shellQuote(zipPath), shellQuote(appPath), shellQuote(u.Username))
	prompt := "Lanvia 要更新到新版本，需要你的密码来替换“应用程序”里的旧版本。以后的更新就不用再输入密码了。"
	osa := fmt.Sprintf("do shell script %s with administrator privileges with prompt %s", appleScriptQuote(script), appleScriptQuote(prompt))
	out, err := exec.Command("osascript", "-e", osa).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "-128") { // 用户点了“取消”
			return ErrCanceled
		}
		return fmt.Errorf("替换失败：%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
