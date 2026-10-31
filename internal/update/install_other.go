//go:build !darwin

package update

import (
	"os"
	"path/filepath"
)

// DownloadDir 是下载安装包的目录：程序所在目录（替换时只需改名）。
func DownloadDir(path string) string { return filepath.Dir(path) }

// InstallMacAuto 只在 macOS 上有意义。
func InstallMacAuto(zipPath, appPath string) error { return os.ErrInvalid }
