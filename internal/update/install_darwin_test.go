//go:build darwin

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Own App: it can be replaced directly, the installer is downloaded next to the App, and the replacement takes the normal path without a password dialog
func TestCanReplaceOwnApp(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Lanvia.app")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", "Lanvia"), []byte("old"), 0o755)
	if !CanReplace(app) || DownloadDir(app) != dir {
		t.Fatalf("CanReplace=%v DownloadDir=%s", CanReplace(app), DownloadDir(app))
	}
	// The zip of the new version
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

// In a non-writable directory (for example "Applications" under a standard account): it cannot be replaced directly, so the installer is downloaded to the user cache directory instead
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
	// Programs that ship with the system in /Applications and belong to root cannot be replaced directly either
	if CanReplace("/Applications/Safari.app") {
		t.Fatal("root-owned app should not be replaceable")
	}
}
