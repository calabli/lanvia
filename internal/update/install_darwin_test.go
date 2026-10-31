//go:build darwin

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 自己的 App：可以直接替换，安装包下载到 App 旁边；替换走普通路径，不弹密码框
func TestCanReplaceOwnApp(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Lanvia.app")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", "Lanvia"), []byte("old"), 0o755)
	if !CanReplace(app) || DownloadDir(app) != dir {
		t.Fatalf("CanReplace=%v DownloadDir=%s", CanReplace(app), DownloadDir(app))
	}
	// 新版本的 zip
	src := filepath.Join(t.TempDir(), "Lanvia.app")
	os.MkdirAll(filepath.Join(src, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(src, "Contents", "MacOS", "Lanvia"), []byte("new"), 0o755)
	zip := filepath.Join(dir, "u.zip")
	if out, err := exec.Command("ditto", "-c", "-k", "--keepParent", src, zip).CombinedOutput(); err != nil {
		t.Fatalf("zip: %v %s", err, out)
	}
	if err := InstallMacAuto(zip, app); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "Lanvia")); string(b) != "new" {
		t.Fatalf("not replaced: %q", b)
	}
	if _, err := os.Stat(app + ".old"); err == nil {
		t.Fatal("backup left behind")
	}
}

// 放在不可写的目录里（例如标准账户下的“应用程序”）：不能直接替换，安装包改为下载到用户缓存目录
func TestCannotReplaceInReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Lanvia.app")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", "Lanvia"), []byte("old"), 0o755)
	os.Chmod(dir, 0o555)
	defer os.Chmod(dir, 0o755)
	if CanReplace(app) {
		t.Fatal("read-only parent should not be replaceable")
	}
	if d := DownloadDir(app); d == dir || filepath.Base(d) != "update" {
		t.Fatalf("DownloadDir = %s", d)
	}
	// /Applications 里系统自带、归 root 所有的程序同样不能直接替换
	if CanReplace("/Applications/Safari.app") {
		t.Fatal("root-owned app should not be replaceable")
	}
}
