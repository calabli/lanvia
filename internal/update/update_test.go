package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		tag, cur string
		want     bool
	}{
		{"v0.6.0", "0.5.0", true}, {"v0.5.1", "0.5.0", true}, {"v1.0.0", "0.9.9", true},
		{"v0.5.0", "0.5.0", false}, {"v0.4.9", "0.5.0", false}, {"v0.10.0", "0.9.0", true}, {"v0.6.0-beta", "0.5.0", true},
	}
	for _, c := range cases {
		if got := Newer(c.tag, c.cur); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.tag, c.cur, got)
		}
	}
}

func TestAssetName(t *testing.T) {
	if AssetName("darwin", "arm64") != "Lanvia-macos.zip" || AssetName("windows", "amd64") != "Lanvia-windows-amd64.exe" || AssetName("linux", "amd64") != "" {
		t.Fatal("unexpected asset names")
	}
}

// 模拟 GitHub：发布信息、安装包、校验文件
func fakeRelease(t *testing.T, name string, body []byte, sum string) (*httptest.Server, string) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[{"name":%q,"browser_download_url":"%s/pkg"},{"name":"SHA256SUMS.txt","browser_download_url":"%s/sums"}]}`, name, srv.URL, srv.URL)
	})
	mux.HandleFunc("/pkg", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  other.zip\n%s  %s\n", sum, sum, name)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.URL + "/latest"
}

func TestDownloadVerifies(t *testing.T) {
	body := []byte("installer bytes")
	h := sha256.Sum256(body)
	_, api := fakeRelease(t, "Lanvia-windows-amd64.exe", body, hex.EncodeToString(h[:]))
	rel, err := Latest(context.Background(), api)
	if err != nil || rel.Tag != "v9.9.9" {
		t.Fatalf("latest: %v %+v", err, rel)
	}
	dir := t.TempDir()
	p, err := Download(context.Background(), rel, "Lanvia-windows-amd64.exe", dir, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != string(body) {
		t.Fatal("content mismatch")
	}

	// 校验和不对：放弃，且不留下文件
	_, api2 := fakeRelease(t, "Lanvia-windows-amd64.exe", body, hex.EncodeToString(make([]byte, 32)))
	rel2, _ := Latest(context.Background(), api2)
	dir2 := t.TempDir()
	if _, err := Download(context.Background(), rel2, "Lanvia-windows-amd64.exe", dir2, nil); err == nil {
		t.Fatal("tampered installer accepted")
	}
	if ents, _ := os.ReadDir(dir2); len(ents) != 0 {
		t.Fatalf("leftover files: %v", ents)
	}
}

func TestLatestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := Latest(context.Background(), srv.URL); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestInstallWindows(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "Lanvia.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	newExe := filepath.Join(dir, ".lanvia-update-1")
	os.WriteFile(newExe, []byte("new"), 0o755)
	if err := InstallWindows(newExe, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatal("not replaced")
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old" {
		t.Fatal("no backup")
	}
	CleanupOld(exe)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("backup not cleaned")
	}
}

func TestInstallMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	dir := t.TempDir()
	mkApp := func(root, marker string) string {
		app := filepath.Join(root, "Lanvia.app")
		os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
		os.WriteFile(filepath.Join(app, "Contents", "MacOS", "Lanvia"), []byte(marker), 0o755)
		return app
	}
	installed := mkApp(filepath.Join(dir, "Applications"), "old")
	src := mkApp(filepath.Join(dir, "build"), "new")
	zip := filepath.Join(dir, "Applications", ".lanvia-update-zip")
	if out, err := exec.Command("ditto", "-c", "-k", "--keepParent", src, zip).CombinedOutput(); err != nil {
		t.Fatalf("zip: %v %s", err, out)
	}
	real, _ := filepath.EvalSymlinks(installed) // 临时目录 /var 是 /private/var 的符号链接
	if AppBundle(filepath.Join(installed, "Contents", "MacOS", "Lanvia")) != real {
		t.Fatal("AppBundle failed")
	}
	if err := InstallMac(zip, installed); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(installed, "Contents", "MacOS", "Lanvia")); string(b) != "new" {
		t.Fatal("not replaced")
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "Applications"))
	for _, e := range ents {
		if e.Name() != "Lanvia.app" && e.Name() != ".lanvia-update-zip" {
			t.Fatalf("leftover: %s", e.Name())
		}
	}
	// 安装包里没有 Lanvia.app：失败，旧版保持原样
	bad := filepath.Join(dir, "Applications", ".bad.zip")
	os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644)
	exec.Command("ditto", "-c", "-k", filepath.Join(dir, "x.txt"), bad).Run()
	if err := InstallMac(bad, installed); err == nil {
		t.Fatal("bad package accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(installed, "Contents", "MacOS", "Lanvia")); string(b) != "new" {
		t.Fatal("app damaged by failed install")
	}
}

// 下载时报告进度；发布里没有本平台的安装包时 Has 返回 false
func TestDownloadProgressAndHas(t *testing.T) {
	body := make([]byte, 3<<20)
	for i := range body {
		body[i] = byte(i)
	}
	h := sha256.Sum256(body)
	_, api := fakeRelease(t, "Lanvia-windows-amd64.exe", body, hex.EncodeToString(h[:]))
	rel, err := Latest(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	if !rel.Has("Lanvia-windows-amd64.exe") || rel.Has("Lanvia-macos.zip") || rel.Has("") {
		t.Fatal("Has")
	}
	var last, total int64
	calls := 0
	p, err := Download(context.Background(), rel, "Lanvia-windows-amd64.exe", t.TempDir(), func(done, tot int64) { calls++; last, total = done, tot })
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(p)
	if calls == 0 || last != int64(len(body)) || total != int64(len(body)) {
		t.Fatalf("progress: calls=%d last=%d total=%d", calls, last, total)
	}
}

// 不用接口也能查到最新版本：跟随 /releases/latest 的跳转拿到版本号，从校验文件里得知有哪些安装包，再正常下载校验
func TestLatestWeb(t *testing.T) {
	body := []byte("mac zip bytes")
	h := sha256.Sum256(body)
	sum := hex.EncodeToString(h[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/o/r/releases/tag/v9.9.9", http.StatusFound)
	})
	mux.HandleFunc("/o/r/releases/download/v9.9.9/SHA256SUMS.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  Lanvia-macos.zip\n%s *Lanvia-windows-amd64.exe\n", sum, sum)
	})
	mux.HandleFunc("/o/r/releases/download/v9.9.9/Lanvia-macos.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel, err := latestWeb(context.Background(), srv.URL, "o/r")
	if err != nil || rel.Tag != "v9.9.9" || rel.URL != srv.URL+"/o/r/releases/tag/v9.9.9" {
		t.Fatalf("latestWeb: %v %+v", err, rel)
	}
	if !rel.Has("Lanvia-macos.zip") || !rel.Has("Lanvia-windows-amd64.exe") || rel.Has("Lanvia-windows-arm64.exe") {
		t.Fatalf("assets: %+v", rel.Assets)
	}
	p, err := Download(context.Background(), rel, "Lanvia-macos.zip", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != string(body) {
		t.Fatal("downloaded content differs")
	}
	// 没有发布过：404
	if _, err := latestWeb(context.Background(), srv.URL, "x/none"); err != ErrNotFound {
		t.Fatalf("missing repo: %v", err)
	}
}

// 连真实的 GitHub 查一次（需要联网，默认跳过）：LANVIA_LIVE=1 go test -run TestLiveFind ./internal/update
func TestLiveFind(t *testing.T) {
	if os.Getenv("LANVIA_LIVE") == "" {
		t.Skip("set LANVIA_LIVE=1 to query github.com")
	}
	rel, err := Find(context.Background(), DefaultAPI)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("latest %s, %d assets, has mac zip: %v, notes: %d bytes", rel.Tag, len(rel.Assets), rel.Has("Lanvia-macos.zip"), len(Notes(context.Background(), DefaultAPI, rel.Tag)))
}
