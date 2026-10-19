//go:build !darwin && !windows

package main

import (
	"os"
	"sync"
)

var sysLang = sync.OnceValue(func() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if isZh(v) {
				return "zh"
			}
			return "en"
		}
	}
	return "zh"
})
