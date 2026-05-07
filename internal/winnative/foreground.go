//go:build windows

package winnative

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unsafe"

	"github.com/jeffwang/qdesk/internal/winserver"
	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowTextW             = user32.NewProc("GetWindowTextW")
	procIsWindowVisible            = user32.NewProc("IsWindowVisible")
	procEnumWindows                = user32.NewProc("EnumWindows")
	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procAttachThreadInput          = user32.NewProc("AttachThreadInput")
	procGetCurrentThreadId         = kernel32.NewProc("GetCurrentThreadId")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

const (
	swRestore                      = 9
	processQueryLimitedInformation = 0x1000
)

func frontApp() (winserver.FrontApp, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return winserver.FrontApp{}, fmt.Errorf("no foreground window")
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	exe, _ := exeForPID(pid)
	title := windowText(hwnd)
	return winserver.FrontApp{
		HWND:  hwnd,
		PID:   pid,
		Exe:   strings.ToLower(filepath.Base(exe)),
		Title: title,
	}, nil
}

func windowText(hwnd uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func exeForPID(pid uint32) (string, error) {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return "", fmt.Errorf("OpenProcess failed for pid %d", pid)
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return "", fmt.Errorf("QueryFullProcessImageNameW failed")
	}
	return syscall.UTF16ToString(buf[:size]), nil
}

func activate(req winserver.ActivateReq) (winserver.ActivateResp, error) {
	target := uintptr(req.HWND)
	if target == 0 {
		var titleRe *regexp.Regexp
		if req.TitleRegex != "" {
			re, err := regexp.Compile(req.TitleRegex)
			if err != nil {
				return winserver.ActivateResp{}, fmt.Errorf("title_regex: %w", err)
			}
			titleRe = re
		}
		target = findWindow(req.Exe, titleRe)
		if target == 0 {
			return winserver.ActivateResp{}, fmt.Errorf("no window matched exe=%q title_regex=%q", req.Exe, req.TitleRegex)
		}
	}

	procShowWindow.Call(target, swRestore)
	stealForeground(target)
	cur, _, _ := procGetForegroundWindow.Call()
	return winserver.ActivateResp{HWND: target, ActuallyForeground: cur == target}, nil
}

// findWindow returns the first visible top-level window whose exe
// matches (case-insensitive) and/or whose title matches titleRe.
func findWindow(exeWant string, titleRe *regexp.Regexp) uintptr {
	exeWant = strings.ToLower(exeWant)
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		if exeWant != "" {
			var pid uint32
			procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
			exe, err := exeForPID(pid)
			if err != nil || strings.ToLower(filepath.Base(exe)) != exeWant {
				return 1
			}
		}
		if titleRe != nil {
			t := windowText(hwnd)
			if !titleRe.MatchString(t) {
				return 1
			}
		}
		found = hwnd
		return 0 // stop enumeration
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// stealForeground tries to make hwnd the foreground window, working
// around Windows' restriction that only the current foreground process
// can change focus. Trick: AttachThreadInput to the foreground thread,
// then SetForegroundWindow.
func stealForeground(hwnd uintptr) {
	curHwnd, _, _ := procGetForegroundWindow.Call()
	if curHwnd == hwnd {
		return
	}
	var fgPID uint32
	fgThread, _, _ := procGetWindowThreadProcessId.Call(curHwnd, uintptr(unsafe.Pointer(&fgPID)))
	myThread, _, _ := procGetCurrentThreadId.Call()
	procAttachThreadInput.Call(myThread, fgThread, 1)
	defer procAttachThreadInput.Call(myThread, fgThread, 0)
	procSetForegroundWindow.Call(hwnd)
}
