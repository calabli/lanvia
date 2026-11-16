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

// On the desktop "Download" saves straight to Downloads/Lanvia: the WebView has no download manager.
func downloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "Lanvia")
	}
	return filepath.Join(home, "Downloads", "Lanvia")
}

// SaveFile saves a file to Downloads/Lanvia and returns the path. The same item is only saved once.
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

// openBlob opens a local file; when it is missing here it is fetched from the source device over our own HTTP API (and cached on the way).
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

// inDownloads makes sure only files inside Downloads/Lanvia can be opened or shown.
func inDownloads(p string) bool {
	rel, err := filepath.Rel(downloadsDir(), filepath.Clean(p))
	return err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// RevealFile shows the file in Finder / Explorer.
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

// OpenFile opens the file with the default application.
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
