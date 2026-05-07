//go:build windows

package winnative

import "golang.org/x/sys/windows"

// init sets the process DPI awareness to PerMonitorV2 so screenshot
// pixel coordinates and SetCursorPos coordinates are in the same
// physical-pixel space. Must run before any GUI syscall.
func init() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SetProcessDpiAwarenessContext")
	if proc.Find() != nil {
		return // older Windows: silently fall through
	}
	const DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = ^uintptr(0) - 3 // -4
	_, _, _ = proc.Call(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)
}
