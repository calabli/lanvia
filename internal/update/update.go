// Package update 实现桌面版的自动更新：查询 GitHub 上的最新版本，下载对应平台的安装包，
// 用 SHA256SUMS.txt 校验后替换当前程序。替换步骤和界面无关，可以单独测试。
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

// DefaultAPI 是 GitHub 接口里最新版本的地址。接口对不登录的访问每个公网 IP 每小时只给 60 次，
// 共用出口的网络里很容易用完（返回 403），所以检查更新主要走网页地址，见 Find。
const DefaultAPI = "https://api.github.com/repos/calabli/lanvia/releases/latest"

// 发布所在的仓库和网站地址
const (
	DefaultRepo = "calabli/lanvia"
	githubWeb   = "https://github.com"
)

var ErrNotFound = errors.New("找不到发布信息（仓库不是公开的，或还没有发布）")

// ErrCanceled 表示用户在密码对话框里点了“取消”。
var ErrCanceled = errors.New("已取消更新")

type Release struct {
	Tag    string  `json:"tag_name"`
	Name   string  `json:"name"`
	Body   string  `json:"body"` // 发布说明（Markdown）
	URL    string  `json:"html_url"`
	Assets []Asset `json:"assets"`
}

// Has 判断这个版本有没有某个安装包（有的版本只发布了部分平台）。
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

// Find 查询最新发布的版本：先用网页地址（不受接口访问次数限制），失败时再试接口。
// apiURL 不是 DefaultAPI 时（测试用的假服务器）直接用接口。网页方式拿不到更新说明，需要时用 Notes 补。
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

// Notes 尽量取某个版本的更新说明（走接口，可能因为访问次数用完而失败，失败时返回空）。
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

// latestWeb 不用接口查最新版本：github.com/<repo>/releases/latest 会跳转到 /releases/tag/<版本>，
// 再读这个版本的 SHA256SUMS.txt，里面列出了它有哪些安装包；下载地址按固定格式拼出来。
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

// Latest 通过 GitHub 接口查询最新发布的版本。
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

// Newer 判断 tag（如 v0.6.0）是否比 current（如 0.5.0）新。
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

// AssetName 返回当前平台的桌面版安装包文件名；不支持的平台返回空。
func AssetName(goos, goarch string) string {
	switch goos {
	case "darwin":
		return "Lanvia-macos.zip" // 通用二进制，Apple 芯片和 Intel 都用这个
	case "windows":
		if goarch == "amd64" || goarch == "arm64" {
			return "Lanvia-windows-" + goarch + ".exe"
		}
	}
	return ""
}

// Download 下载安装包到 dir，并用发布里的 SHA256SUMS.txt 校验。dir 应和目标程序在同一磁盘上，
// 这样后面可以直接改名替换。
//
// onProgress（可以为 nil）在下载过程中被调用，参数是已下载和总共的字节数。
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
	resp, err := (&http.Client{}).Do(req) // 大文件不限总时长，由 ctx 控制
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
		onProgress(n, max(total, n)) // 下载完成：总是报告一次 100%（服务器没给大小时也一样）
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

// AppBundle 返回正在运行的 .app 路径（macOS）；不是从 .app 运行时返回空。
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

// InstallMac 用 zip 里的 Lanvia.app 替换 appPath。zip 应已下载到 appPath 所在目录。
// 先把旧版改名备份，新版就位后再删除备份；任何一步失败都会恢复旧版。
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
		os.Rename(backup, appPath) // 恢复旧版
		return err
	}
	os.RemoveAll(backup) // 正在运行的旧程序已加载进内存，删除文件不影响它
	return nil
}

// InstallWindows 用 newExe 替换 exePath。Windows 不能覆盖正在运行的 exe，但可以改名，
// 所以先把旧版改名为 .old（下次启动时由 CleanupOld 删除），再把新版放到原位置。
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

// CleanupOld 删除上次更新留下的旧版程序（Windows）。
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
