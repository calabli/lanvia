package core

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// 电脑端“下载”直接保存到 下载/Lanvia，WebView 里没有浏览器的下载管理器。
func downloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "Lanvia")
	}
	return filepath.Join(home, "Downloads", "Lanvia")
}

// SaveFile 把一个文件保存到 下载/Lanvia，返回保存路径。同一条内容只保存一次。
func (a *App) SaveFile(id string) (string, error) {
	it := a.store.Get(id)
	if it == nil || it.Kind != "file" {
		return "", errors.New("文件不存在")
	}
	a.mu.Lock()
	if p, ok := a.saved[id]; ok {
		a.mu.Unlock()
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	} else {
		a.mu.Unlock()
	}
	dir := downloadsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	src, err := a.openBlob(it)
	if err != nil {
		return "", err
	}
	defer src.Close()
	dst := uniquePath(dir, sanitizeName(it.Name))
	f, err := os.Create(dst + ".part")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, src)
	f.Close()
	if err != nil {
		os.Remove(dst + ".part")
		return "", err
	}
	if err := os.Rename(dst+".part", dst); err != nil {
		return "", err
	}
	a.mu.Lock()
	a.saved[id] = dst
	a.mu.Unlock()
	return dst, nil
}

// openBlob 打开本地文件；本机没有时通过自己的 HTTP 接口从来源设备取（顺便缓存）。
func (a *App) openBlob(it *Item) (io.ReadCloser, error) {
	if f, err := os.Open(a.store.blobPath(it.ID)); err == nil {
		return f, nil
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/blob/%s", a.Port(), it.ID), nil)
	req.Host = fmt.Sprintf("localhost:%d", a.Port())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, errors.New("来源设备不在线")
	}
	return resp.Body, nil
}

func uniquePath(dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	p := filepath.Join(dir, name)
	for i := 2; ; i++ {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
}

// inDownloads 确保只能打开/显示 下载/Lanvia 里的文件。
func inDownloads(p string) bool {
	rel, err := filepath.Rel(downloadsDir(), filepath.Clean(p))
	return err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// RevealFile 在访达 / 资源管理器中显示文件。
func RevealFile(p string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", p).Start()
	case "windows":
		return exec.Command("explorer", "/select,", p).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(p)).Start()
	}
}

// OpenFile 用系统默认程序打开文件。
func OpenFile(p string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", p).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", p).Start()
	default:
		return exec.Command("xdg-open", p).Start()
	}
}
