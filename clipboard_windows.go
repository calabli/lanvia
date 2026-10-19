package main

import (
	"errors"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// 直接调用 Win32 API，避免每次轮询都启动 PowerShell。
var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	pOpenClipboard           = user32.NewProc("OpenClipboard")
	pCloseClipboard          = user32.NewProc("CloseClipboard")
	pEmptyClipboard          = user32.NewProc("EmptyClipboard")
	pGetClipboardData        = user32.NewProc("GetClipboardData")
	pSetClipboardData        = user32.NewProc("SetClipboardData")
	pIsFormatAvailable       = user32.NewProc("IsClipboardFormatAvailable")
	pRegisterClipboardFormat = user32.NewProc("RegisterClipboardFormatW")
	pGetSequenceNumber       = user32.NewProc("GetClipboardSequenceNumber")
	pGlobalAlloc             = kernel32.NewProc("GlobalAlloc")
	pGlobalFree              = kernel32.NewProc("GlobalFree")
	pGlobalLock              = kernel32.NewProc("GlobalLock")
	pGlobalUnlock            = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// 密码管理器（1Password、KeePass、Bitwarden 等）会用这些格式标记敏感内容。
var excludeFormats = func() []uintptr {
	var out []uintptr
	for _, n := range []string{"ExcludeClipboardContentFromMonitorProcessing", "Clipboard Viewer Ignore"} {
		p, _ := syscall.UTF16PtrFromString(n)
		if f, _, _ := pRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(p))); f != 0 {
			out = append(out, f)
		}
	}
	return out
}()

func clipboardSupported() bool { return pOpenClipboard.Find() == nil }

func openClipboard() error {
	for i := 0; i < 10; i++ { // 其他程序可能短暂占用剪贴板
		if r, _, _ := pOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("OpenClipboard failed")
}

var lastSeq uintptr
var lastText string

func readClipboard() (string, error) {
	// 序列号没变说明内容没变，省去一次打开剪贴板
	seq, _, _ := pGetSequenceNumber.Call()
	if seq != 0 && seq == lastSeq {
		return lastText, nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer pCloseClipboard.Call()
	for _, f := range excludeFormats {
		if r, _, _ := pIsFormatAvailable.Call(f); r != 0 {
			lastSeq, lastText = seq, ""
			return "", errSkip
		}
	}
	if r, _, _ := pIsFormatAvailable.Call(cfUnicodeText); r == 0 {
		lastSeq, lastText = seq, ""
		return "", nil
	}
	h, _, _ := pGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", errors.New("GetClipboardData failed")
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return "", errors.New("GlobalLock failed")
	}
	defer pGlobalUnlock.Call(h)
	n := 0
	for *(*uint16)(unsafe.Pointer(p + uintptr(n)*2)) != 0 {
		n++
	}
	lastSeq, lastText = seq, syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), n))
	return lastText, nil
}

func writeClipboard(s string) error {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(); err != nil {
		return err
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	h, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return errors.New("GlobalAlloc failed")
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return errors.New("GlobalLock failed")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)), u)
	pGlobalUnlock.Call(h)
	if r, _, _ := pSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		pGlobalFree.Call(h)
		return errors.New("SetClipboardData failed")
	}
	return nil
}
