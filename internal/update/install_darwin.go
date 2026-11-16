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

// A Lanvia installed with the installer belongs to root (the installer of v0.13.0), and a standard account cannot modify "Applications" either:
// in those cases it cannot be replaced directly, so it is replaced as administrator through the system password dialog, and the new version is handed back to the current user,
// so that later updates do not need a password anymore.

func ownedByMe(path string) bool {
	st, err := os.Lstat(path)
	if err != nil {
		return false
	}
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == os.Getuid()
}

func writable(dir string) bool { return syscall.Access(dir, 2 /* W_OK */) == nil }

// CanReplace reports whether the current user can replace appPath directly (without administrator rights).
func CanReplace(appPath string) bool {
	return writable(filepath.Dir(appPath)) && ownedByMe(appPath) && ownedByMe(filepath.Join(appPath, "Contents")) &&
		ownedByMe(filepath.Join(appPath, "Contents", "MacOS", "Lanvia"))
}

// DownloadDir is the directory the installer is downloaded to: next to the App when it can be replaced directly (replacing only needs a rename), otherwise in the user cache directory.
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

// InstallMacAuto replaces it directly when that is possible; otherwise it pops up the system password dialog and replaces it as administrator.
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
	// Unzip → check → rename the old version as a backup → put the new version in place → hand it back to the current user → delete the backup; on failure the old version is restored
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
		if strings.Contains(string(out), "-128") { // the user clicked "Cancel"
			return ErrCanceled
		}
		return fmt.Errorf("替换失败：%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
