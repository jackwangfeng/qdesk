// Package winserver implements the MCP server for qdesk-win. It is
// platform-independent; the actual Win32 syscalls live in
// internal/winnative which satisfies the Native interface defined here.
package winserver

import "context"

// Native is everything the MCP dispatcher needs from the OS. Real impl
// is internal/winnative.New(); tests use FakeNative.
type Native interface {
	FrontApp(ctx context.Context) (FrontApp, error)
	Activate(ctx context.Context, req ActivateReq) (ActivateResp, error)
	Screenshot(ctx context.Context) (Screenshot, error)
	Click(ctx context.Context, req ClickReq) error
	Type(ctx context.Context, text string) error
	Key(ctx context.Context, combo string) error
	Scroll(ctx context.Context, req ScrollReq) error
	ClipboardPaste(ctx context.Context, text string) (ClipboardResp, error)
}

// FrontApp is what GetForegroundWindow + GetWindowThreadProcessId yield.
// Exe is the basename, lowercased (e.g. "notepad.exe"); the expected_exe
// guard in tools.go compares against it case-insensitively.
type FrontApp struct {
	HWND  uintptr `json:"hwnd"`
	PID   uint32  `json:"pid"`
	Exe   string  `json:"exe"` // basename, lowercased ("notepad.exe")
	Title string  `json:"title"`
}

// ActivateReq targets a window. Priority: HWND > Exe > TitleRegex.
// At least one field must be set (non-zero HWND, or non-empty Exe / TitleRegex).
type ActivateReq struct {
	HWND       uintptr `json:"hwnd,omitempty"`
	Exe        string  `json:"exe,omitempty"`
	TitleRegex string  `json:"title_regex,omitempty"`
}

// ActivateResp reports the window we ended up activating and whether
// SetForegroundWindow actually succeeded (Windows rejects requests
// from non-foreground processes — caller must not assume success).
type ActivateResp struct {
	HWND               uintptr `json:"hwnd"`
	ActuallyForeground bool    `json:"actually_foreground"`
}

// Screenshot is a PNG of the primary monitor.
type Screenshot struct {
	PNGBase64 string `json:"png_base64"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// ClickReq describes a synthetic mouse click. Coordinates are PHYSICAL
// pixels — the same coordinate space Screenshot dimensions use under
// PerMonitorV2 DPI awareness.
type ClickReq struct {
	X         int      `json:"x"`
	Y         int      `json:"y"`
	Button    string   `json:"button"`              // "left" | "right" | "middle"
	Double    bool     `json:"double"`              // true = double-click
	Modifiers []string `json:"modifiers,omitempty"` // "ctrl" "shift" "alt" "win"
}

// ScrollReq describes a wheel-scroll at a physical pixel point. dx/dy
// are wheel notches (positive dy = scroll up, matching mac.scroll convention).
type ScrollReq struct {
	X  int `json:"x"`
	Y  int `json:"y"`
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// ClipboardResp tells the caller whether we managed to put back
// the user's original clipboard contents after pasting.
type ClipboardResp struct {
	Restored bool `json:"clipboard_restored"`
}
