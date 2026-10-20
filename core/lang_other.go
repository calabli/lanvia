//go:build !windows

package core

import (
	"os/exec"
	"strings"
)

// 终端里通常有 LANG；没有时（例如从图形界面启动）在 macOS 上读取首选语言。
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
