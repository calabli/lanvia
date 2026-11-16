package main

import (
	"os/exec"
	"strings"
	"sync"
)

var sysLang = sync.OnceValue(func() string {
	// The first language in the system preferences, e.g. (\n    "zh-Hans-CN",\n    "en-CN"\n)
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
