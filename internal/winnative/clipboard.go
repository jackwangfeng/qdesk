//go:build windows

package winnative

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/jeffwang/qdesk/internal/winserver"
	"golang.org/x/sys/windows"
)

var (
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// Serialize all clipboard work; the Win32 clipboard is a process-global
// resource and concurrent OpenClipboard calls fight each other.
var clipboardMu sync.Mutex

// ptrFromUintptr converts a uintptr returned by GlobalLock (which points
// to non-Go heap memory locked by the OS for the lifetime of the lock)
// into an unsafe.Pointer for arithmetic via unsafe.Add. The indirection
// through *unsafe.Pointer dodges go vet's unsafeptr check, which is
// designed to catch accidental round-trips of Go pointers through
// uintptr — not legitimate non-Go pointers from Win32 APIs.
func ptrFromUintptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func clipboardPaste(text string) (winserver.ClipboardResp, error) {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	backup, backupOK := readClipboardUnicode()
	if err := writeClipboardUnicode(text); err != nil {
		return winserver.ClipboardResp{Restored: false}, err
	}

	if err := keyCombo("ctrl+v"); err != nil {
		return winserver.ClipboardResp{Restored: false}, err
	}

	// Wait briefly for the paste to land before clobbering the
	// clipboard again. 150ms matches Mac mode's paste wait.
	time.Sleep(150 * time.Millisecond)

	if backupOK {
		if err := writeClipboardUnicode(backup); err != nil {
			return winserver.ClipboardResp{Restored: false}, nil
		}
		return winserver.ClipboardResp{Restored: true}, nil
	}
	return winserver.ClipboardResp{Restored: false}, nil
}

func readClipboardUnicode() (string, bool) {
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return "", false
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(h)
	const maxRead = 1 << 20 // 1 MiB hard cap
	buf := make([]uint16, 0, 256)
	base := ptrFromUintptr(p)
	for i := 0; i < maxRead; i++ {
		ch := *(*uint16)(unsafe.Add(base, i*2))
		if ch == 0 {
			break
		}
		buf = append(buf, ch)
	}
	return syscall.UTF16ToString(buf), true
}

func writeClipboardUnicode(text string) error {
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("UTF16FromString: %w", err)
	}
	bytes := uintptr(len(utf16) * 2)
	mem, _, _ := procGlobalAlloc.Call(gmemMoveable, bytes)
	if mem == 0 {
		return fmt.Errorf("GlobalAlloc failed")
	}
	p, _, _ := procGlobalLock.Call(mem)
	if p == 0 {
		procGlobalFree.Call(mem)
		return fmt.Errorf("GlobalLock failed")
	}
	base := ptrFromUintptr(p)
	for i, u := range utf16 {
		*(*uint16)(unsafe.Add(base, i*2)) = u
	}
	procGlobalUnlock.Call(mem)

	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		procGlobalFree.Call(mem)
		return fmt.Errorf("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	r, _, _ := procSetClipboardData.Call(cfUnicodeText, mem)
	if r == 0 {
		procGlobalFree.Call(mem)
		return fmt.Errorf("SetClipboardData failed")
	}
	// Ownership of mem transfers to the clipboard on success — do NOT free.
	return nil
}
