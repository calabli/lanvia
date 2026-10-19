package main

import (
	"os/exec"
	"strings"
	"sync"
)

var sysLang = sync.OnceValue(func() string {
	// 系统偏好设置里排在第一位的语言，例如 (\n    "zh-Hans-CN",\n    "en-CN"\n)
	out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
	if err == nil {
		for _, f := range strings.FieldsFunc(string(out), func(r rune) bool { return strings.ContainsRune("()\",\n ", r) }) {
			if isZh(f) {
				return "zh"
			}
			return "en"
		}
	}
	return "zh"
})
