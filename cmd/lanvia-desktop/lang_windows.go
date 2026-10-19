package main

import (
	"sync"
	"syscall"
)

var sysLang = sync.OnceValue(func() string {
	// LANGID 的低 10 位是主语言，0x04 为中文
	r, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage").Call()
	if r&0x3ff == 0x04 {
		return "zh"
	}
	return "en"
})
