package core

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Calls the Win32 API directly, so polling does not have to start PowerShell every time.
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
	pGlobalSize              = kernel32.NewProc("GlobalSize")
)

const (
	cfUnicodeText = 13
	cfDIB         = 8
	cfHDrop       = 15
	cfDIBV5       = 17
	gmemMoveable  = 0x0002
)

// Browsers, screenshot tools and the like also put a PNG there (with transparency support)
var cfPNG = func() uintptr {
	p, _ := syscall.UTF16PtrFromString("PNG")
	f, _, _ := pRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(p)))
	return f
}()

// Password managers (1Password, KeePass, Bitwarden and so on) mark sensitive content with these formats.
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
	for i := 0; i < 10; i++ { // other programs may hold the clipboard briefly
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
	// An unchanged sequence number means the content is unchanged, saving one clipboard open
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

// On Windows, copying files leaves no text in the clipboard; sensitive content is already skipped in readClipboard.
func skipClipboardChange() bool { return false }

func clipSeq() int64 {
	seq, _, _ := pGetSequenceNumber.Call()
	if seq == 0 {
		return -1
	}
	return int64(seq)
}

// globalBytes copies out the data of a given clipboard format (the caller has already opened the clipboard).
func globalBytes(format uintptr) []byte {
	if format == 0 {
		return nil
	}
	if r, _, _ := pIsFormatAvailable.Call(format); r == 0 {
		return nil
	}
	h, _, _ := pGetClipboardData.Call(format)
	if h == 0 {
		return nil
	}
	n, _, _ := pGlobalSize.Call(h)
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 || n == 0 || n > maxClipImage*4 {
		if p != 0 {
			pGlobalUnlock.Call(h)
		}
		return nil
	}
	defer pGlobalUnlock.Call(h)
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(p)), n)...)
}

func readClipboardImage() ([]byte, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(); err != nil {
		return nil, err
	}
	defer pCloseClipboard.Call()
	// Files copied in Explorer and content marked by password managers do not count
	if r, _, _ := pIsFormatAvailable.Call(cfHDrop); r != 0 {
		return nil, errNoImage
	}
	for _, f := range excludeFormats {
		if r, _, _ := pIsFormatAvailable.Call(f); r != 0 {
			return nil, errSkip
		}
	}
	if b := globalBytes(cfPNG); len(b) > 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n" {
		return b, nil
	}
	for _, f := range []uintptr{cfDIBV5, cfDIB} {
		if b := globalBytes(f); b != nil {
			return dibToPNG(b)
		}
	}
	return nil, errNoImage
}

// setGlobal puts data into a given clipboard format (the caller has already opened and cleared the clipboard).
func setGlobal(format uintptr, data []byte) error {
	h, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return errors.New("GlobalAlloc failed")
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return errors.New("GlobalLock failed")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(data)), data)
	pGlobalUnlock.Call(h)
	if r, _, _ := pSetClipboardData.Call(format, h); r == 0 {
		pGlobalFree.Call(h)
		return errors.New("SetClipboardData failed")
	}
	return nil
}

func writeClipboardImage(data []byte) error {
	dib, pngData, err := imageToClip(data)
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
	if err := setGlobal(cfDIB, dib); err != nil {
		return err
	}
	if cfPNG != 0 {
		setGlobal(cfPNG, pngData) // programs supporting transparency prefer it; a failure does not affect the bitmap
	}
	return nil
}

var (
	pGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	pGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	pOpenProcess                = kernel32.NewProc("OpenProcess")
	pCloseHandle                = kernel32.NewProc("CloseHandle")
	pQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

// frontApp returns the file name of the process owning the foreground window (for example chrome.exe).
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
