//go:build !windows

package core

import (
	"os/exec"
	"strings"
)

// The terminal usually has LANG; without it (for example when started from a GUI) macOS reports the preferred language.
func platformLanguage() string {
	if out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
		for _, f := range strings.FieldsFunc(string(out), func(r rune) bool { return strings.ContainsRune("()\",\n ", r) }) {
			if strings.HasPrefix(strings.ToLower(f), "zh") {
				return "zh"
			}
			return "en"
		}
	}
	return "zh"
}
