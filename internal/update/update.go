// Package update implements the automatic update of the desktop client: it looks up the latest version on GitHub, downloads the installer for the matching platform,
// and replaces the current program after verifying it with SHA256SUMS.txt. The replacement steps are independent of the UI and can be tested on their own.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultAPI is the address of the latest version in the GitHub API. For unauthenticated access the API allows only 60 requests per hour per public IP,
// which is easily used up on networks sharing an egress IP (it returns 403), so update checks mainly go through the web address, see Find.
const DefaultAPI = "https://api.github.com/repos/calabli/lanvia/releases/latest"

// The repository and the website address where the releases live
const (
	DefaultRepo = "calabli/lanvia"
	githubWeb   = "https://github.com"
)

var ErrNotFound = errors.New("找不到发布信息（仓库不是公开的，或还没有发布）")

// ErrCanceled means the user clicked "Cancel" in the password dialog.
var ErrCanceled = errors.New("已取消更新")

type Release struct {
	Tag    string  `json:"tag_name"`
	Name   string  `json:"name"`
	Body   string  `json:"body"` // release notes (Markdown)
	URL    string  `json:"html_url"`
	Assets []Asset `json:"assets"`
}

// Has reports whether this release has a given installer (some releases only cover some platforms).
func (r *Release) Has(name string) bool { return name != "" && r.asset(name) != nil }

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

func (r *Release) asset(name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

var client = &http.Client{Timeout: 30 * time.Second}

// Find looks up the latest release: it uses the web address first (not limited by the API request count) and falls back to the API when that fails.
// When apiURL is not DefaultAPI (a fake server for tests) it goes straight to the API. The web route gives no release notes, so use Notes to fill them in when needed.
func Find(ctx context.Context, apiURL string) (*Release, error) {
	if apiURL != DefaultAPI {
		return Latest(ctx, apiURL)
	}
	rel, err := latestWeb(ctx, githubWeb, DefaultRepo)
	if err == nil {
		return rel, nil
	}
	if r, e := Latest(ctx, apiURL); e == nil {
		return r, nil
	}
	return nil, err
}

// Notes tries to fetch the release notes of a version (it goes through the API and may fail because the request count is used up; it returns an empty string on failure).
func Notes(ctx context.Context, apiURL, tag string) string {
	if apiURL != DefaultAPI {
		return ""
	}
	u := "https://api.github.com/repos/" + DefaultRepo + "/releases/tags/" + url.PathEscape(tag)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var r Release
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&r) != nil {
		return ""
	}
	return r.Body
}

// latestWeb looks up the latest version without the API: github.com/<repo>/releases/latest redirects to /releases/tag/<version>,
// and then it reads the SHA256SUMS.txt of that version, which lists the installers it has; the download URLs are built from a fixed format.
func latestWeb(ctx context.Context, base, repo string) (*Release, error) {
	noRedirect := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/"+repo+"/releases/latest", nil)
	resp, err := noRedirect.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/releases/tag/")
	if resp.StatusCode/100 != 3 || i < 0 {
		if resp.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("找不到最新版本（HTTP %d）", resp.StatusCode)
	}
	tag, err := url.PathUnescape(loc[i+len("/releases/tag/"):])
	if err != nil || tag == "" {
		return nil, fmt.Errorf("最新版本地址不对：%s", loc)
	}
	dl := base + "/" + repo + "/releases/download/" + url.PathEscape(tag) + "/"
	rel := &Release{Tag: tag, URL: base + "/" + repo + "/releases/tag/" + url.PathEscape(tag)}
	sums, err := fetch(ctx, dl+"SHA256SUMS.txt")
	if err != nil {
		return nil, fmt.Errorf("读取 %s 的校验文件失败：%w", tag, err)
	}
	rel.Assets = append(rel.Assets, Asset{Name: "SHA256SUMS.txt", URL: dl + "SHA256SUMS.txt"})
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			name := strings.TrimPrefix(f[1], "*")
			rel.Assets = append(rel.Assets, Asset{Name: name, URL: dl + url.PathEscape(name)})
		}
	}
	return rel, nil
}

func fetch(ctx context.Context, u string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Latest looks up the latest release through the GitHub API.
func Latest(ctx context.Context, apiURL string) (*Release, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r Release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Newer reports whether tag (such as v0.6.0) is newer than current (such as 0.5.0).
func Newer(tag, current string) bool {
	a, b := parse(tag), parse(current)
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

// AssetName returns the file name of the desktop installer for the current platform; unsupported platforms get an empty string.
func AssetName(goos, goarch string) string {
	switch goos {
	case "darwin":
		return "Lanvia-macos.zip" // universal binary, used by both Apple silicon and Intel
	case "windows":
		if goarch == "amd64" || goarch == "arm64" {
			return "Lanvia-windows-" + goarch + ".exe"
		}
	}
	return ""
}

// Download downloads the installer into dir and verifies it with the SHA256SUMS.txt from the release. dir should be on the same disk as the target program,
// so that the program can later be replaced by renaming it.
//
// onProgress (which may be nil) is called during the download, with the number of bytes downloaded and the total.
func Download(ctx context.Context, r *Release, name, dir string, onProgress func(done, total int64)) (string, error) {
	a := r.asset(name)
	if a == nil {
		return "", fmt.Errorf("这个版本没有 %s", name)
	}
	sums := r.asset("SHA256SUMS.txt")
	if sums == nil {
		return "", errors.New("这个版本没有校验文件 SHA256SUMS.txt")
	}
	want, err := checksum(ctx, sums.URL, name)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".lanvia-update-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(path)
		}
	}()
	req, _ := http.NewRequestWithContext(ctx, "GET", a.URL, nil)
	resp, err := (&http.Client{}).Do(req) // large files have no overall time limit, ctx controls it
	if err != nil {
		f.Close()
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		f.Close()
		return "", fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	total := resp.ContentLength
	if total <= 0 {
		total = a.Size
	}
	var w io.Writer = io.MultiWriter(f, h)
	if onProgress != nil {
		w = &progressWriter{w: w, total: total, fn: onProgress}
	}
	n, err := io.Copy(w, resp.Body)
	if err == nil && onProgress != nil {
		onProgress(n, max(total, n)) // download finished: always report 100% once (also when the server did not report a size)
	}
	f.Close()
	if err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", errors.New("安装包校验失败，已放弃更新")
	}
	ok = true
	return path, nil
}

func checksum(ctx context.Context, url, name string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("校验文件里没有 %s", name)
}

// AppBundle returns the path of the running .app (macOS); it returns an empty string when not running from a .app.
func AppBundle(exe string) string {
	exe, _ = filepath.EvalSymlinks(exe)
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	app := filepath.Dir(contents)
	if filepath.Base(macos) == "MacOS" && filepath.Base(contents) == "Contents" && strings.HasSuffix(app, ".app") {
		return app
	}
	return ""
}

// InstallMac replaces appPath with the Lanvia.app inside the zip. The zip should already be downloaded into the directory of appPath.
// It renames the old version as a backup first and deletes the backup once the new version is in place; any failing step restores the old version.
func InstallMac(zipPath, appPath string) error {
	dir := filepath.Dir(appPath)
	tmp, err := os.MkdirTemp(dir, ".lanvia-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if out, err := exec.Command("ditto", "-x", "-k", zipPath, tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("解压失败：%v %s", err, out)
	}
	newApp := filepath.Join(tmp, "Lanvia.app")
	if _, err := os.Stat(filepath.Join(newApp, "Contents", "MacOS", "Lanvia")); err != nil {
		return errors.New("安装包里没有 Lanvia.app")
	}
	backup := appPath + ".old"
	os.RemoveAll(backup)
	if err := os.Rename(appPath, backup); err != nil {
		return fmt.Errorf("无法替换 %s：%v", appPath, err)
	}
	if err := os.Rename(newApp, appPath); err != nil {
		os.Rename(backup, appPath) // restore the old version
		return err
	}
	os.RemoveAll(backup) // the running old program is already loaded into memory, deleting the file does not affect it
	return nil
}

// InstallWindows replaces exePath with newExe. Windows cannot overwrite a running exe, but it can be renamed,
// so the old version is renamed to .old first (deleted by CleanupOld on the next start) and then the new version is put in the original place.
func InstallWindows(newExe, exePath string) error {
	old := exePath + ".old"
	os.Remove(old)
	if err := os.Rename(exePath, old); err != nil {
		return fmt.Errorf("无法替换 %s：%v", exePath, err)
	}
	if err := os.Rename(newExe, exePath); err != nil {
		os.Rename(old, exePath)
		return err
	}
	return nil
}

// CleanupOld deletes the old program left over from the last update (Windows).
func CleanupOld(exePath string) { os.Remove(exePath + ".old") }

type progressWriter struct {
	w     io.Writer
	done  int64
	total int64
	last  time.Time
	fn    func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if now := time.Now(); now.Sub(p.last) > 200*time.Millisecond || p.done == p.total {
		p.last = now
		p.fn(p.done, p.total)
	}
	return n, err
}
