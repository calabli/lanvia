package core

import (
	"errors"
	"runtime"
	"strings"
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

// Windows 复制文件时剪贴板里没有文字，敏感内容在 readClipboard 里已经跳过。
func skipClipboardChange() bool { return false }

var (
	pGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	pGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	pOpenProcess                = kernel32.NewProc("OpenProcess")
	pCloseHandle                = kernel32.NewProc("CloseHandle")
	pQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

// frontApp 返回前台窗口所属进程的文件名（例如 chrome.exe）。
func frontApp() (id, name string) {
	hwnd, _, _ := pGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", ""
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	const processQueryLimitedInformation = 0x1000
	h, _, _ := pOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return "", ""
	}
	defer pCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if r, _, _ := pQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r == 0 {
		return "", ""
	}
	path := syscall.UTF16ToString(buf[:size])
	if i := strings.LastIndexAny(path, `\\/`); i >= 0 {
		path = path[i+1:]
	}
	return path, strings.TrimSuffix(path, ".exe")
}
