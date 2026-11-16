//go:build !darwin

package update

import (
	"os"
	"path/filepath"
)

// DownloadDir is the directory the installer is downloaded to: the directory of the program (replacing only needs a rename).
func DownloadDir(path string) string { return filepath.Dir(path) }

// InstallMacAuto only makes sense on macOS.
func InstallMacAuto(zipPath, appPath string) error { return os.ErrInvalid }
