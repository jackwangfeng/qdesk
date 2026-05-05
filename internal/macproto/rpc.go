// Package macproto defines the line-delimited JSON-RPC protocol spoken
// between qdesk-mac (Go) and qdesk-mac-helper (Swift).
//
// One request per line on the helper's stdin; one response per line on
// the helper's stdout. Notifications are not used; every request gets
// a response keyed by ID.
package macproto

import "encoding/json"

// Method names. Kept in one place so Go and Swift can stay in sync —
// any change here requires a matching change in
// cmd/qdesk-mac-helper/Sources/Helper/main.swift dispatch.
const (
	MethodHealth     = "health"
	MethodFrontApp   = "frontApp"
	MethodActivate   = "activate"
	MethodScreenshot = "screenshot"
	MethodClick      = "click"
	MethodType       = "type"
	MethodKey        = "key"
	MethodScroll     = "scroll"
	MethodAXTree     = "axTree"
	MethodAXClick    = "axClick"
)

// Request is one JSON-RPC call from Go → helper.
type Request struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is one helper → Go reply. Exactly one of Result/Error is set.
type Response struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error is the structured failure object the helper sends back.
type Error struct {
	Code    string `json:"code"`    // e.g. "permission-screen-recording"
	Message string `json:"message"` // human-readable; safe to surface to LLM
}

// HealthResponse is returned for MethodHealth.
type HealthResponse struct {
	OK                     bool `json:"ok"`
	ScreenRecordingGranted bool `json:"screenRecordingGranted"`
	AccessibilityGranted   bool `json:"accessibilityGranted"`
}

// FrontAppResponse is returned for MethodFrontApp.
type FrontAppResponse struct {
	BundleID string `json:"bundleId"`
	Name     string `json:"name"`
	PID      int    `json:"pid"`
}

// ActivateRequest brings an app to the foreground by bundle ID.
type ActivateRequest struct {
	BundleID string `json:"bundleId"`
}

// ScreenshotResponse contains a base64-encoded PNG plus dimensions.
// width/height are LOGICAL points; the actual PNG pixels are
// width*scaleFactor × height*scaleFactor.
type ScreenshotResponse struct {
	PNGBase64   string  `json:"pngBase64"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	ScaleFactor float64 `json:"scaleFactor"`
}

// ClickRequest. Coordinates are LOGICAL global screen points.
type ClickRequest struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Button string  `json:"button"` // "left" | "right" | "middle"
	Clicks int     `json:"clicks"` // 1 or 2
}

// TypeRequest sends Unicode text via CGEvent (bypasses IME).
type TypeRequest struct {
	Text string `json:"text"`
}

// KeyRequest sends a key combo, e.g. "return", "escape", "cmd+v".
type KeyRequest struct {
	Combo string `json:"combo"`
}

// ScrollRequest. dx/dy are wheel deltas in lines (positive dy = scroll up).
type ScrollRequest struct {
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	DX float64 `json:"dx"`
	DY float64 `json:"dy"`
}

// AXTreeRequest queries the accessibility tree of a specific app.
type AXTreeRequest struct {
	BundleID string `json:"bundleId"`
	Query    string `json:"query"` // e.g. "role=AXOutline" — a tiny selector
}

// AXNode is one element in the returned tree (flattened, depth-first).
type AXNode struct {
	Path        string `json:"path"` // opaque ID for axClick
	Role        string `json:"role"`
	Title       string `json:"title,omitempty"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
	Frame       Frame  `json:"frame"`
}

// Frame is in LOGICAL screen points. JSON tags lowercase to match what
// the Swift helper emits (Swift Codable is case-sensitive on encode).
type Frame struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// AXTreeResponse is the matched flat list of AX nodes.
type AXTreeResponse struct {
	Nodes []AXNode `json:"nodes"`
}

// AXClickRequest performs a synthetic press on the node at path.
type AXClickRequest struct {
	BundleID string `json:"bundleId"`
	Path     string `json:"path"`
}

// OK is a generic empty success result.
type OK struct {
	OK bool `json:"ok"`
}

// Error codes surfaced to the LLM. Stable strings — do not rename without
// updating the README + tool descriptions.
//
// Most of these are emitted by the Swift helper as raw string literals
// (see Sources/Helper/*.swift) and parsed back into HelperError by the Go
// supervisor. The Go-side constants here are the canonical reference list
// for both sides; only the ones used in Go code (CodeWeChatNotForeground,
// CodeChatNotFound, CodeAXTreeEmpty, CodeInternal) are referenced from
// Go directly. The rest exist for documentation parity with the helper.
const (
	CodeWeChatNotRunning    = "wechat-not-running"
	CodeWeChatNotForeground = "wechat-not-foreground"
	CodePermScreenRecording = "permission-screen-recording"
	CodePermAccessibility   = "permission-accessibility"
	CodeHelperCrashed       = "helper-crashed"
	CodeChatNotFound        = "chat-not-found"
	CodeAXTreeEmpty         = "ax-tree-empty"
	CodeInternal            = "internal"
)
