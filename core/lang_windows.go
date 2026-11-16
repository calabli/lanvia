package core

import "syscall"

func platformLanguage() string {
	// The low 10 bits of LANGID are the primary language; 0x04 is Chinese
	r, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage").Call()
	if r&0x3ff == 0x04 {
		return "zh"
	}
	return "en"
}
