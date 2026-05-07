# Windows host-mode v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `qdesk-win.exe` — a single Go binary that runs on a Windows host and exposes the `windows.*` MCP tool surface (front_app, activate, screenshot, click, type, key, scroll, clipboard_paste) over HTTP+Bearer, mirroring `qdesk-mac --listen` for cross-platform symmetry.

**Architecture:** Mirrors `internal/macserver` exactly: an MCP dispatcher that depends on a `Native` interface, a fake-native for unit tests, and a real Win32 implementation under `internal/winnative` (windows-only build tag). No sidecar — `golang.org/x/sys/windows` syscalls happen in-process. HTTP transport reuses the auth/CIDR/Tailscale-headers middleware from `cmd/qdesk-mac/http.go`.

**Tech Stack:** Go 1.25, `golang.org/x/sys/windows`, stdlib `image/png`, no third-party deps. Reference design: `docs/superpowers/specs/2026-05-07-windows-host-mode-design.md`. Reference implementation pattern: `internal/macserver/*.go`, `cmd/qdesk-mac/{main,http}.go`.

---

## File Structure

```
cmd/qdesk-win/                        (windows-only build tag on .go files except stub)
  main.go              //go:build windows  — flag parsing, HTTP startup
  main_other.go        //go:build !windows — stub that exits with "windows-only"
  http.go              //go:build windows  — copy of mac http.go without caffeinate

internal/winserver/                   (cross-platform; tested on macOS dev)
  mcp.go               — initialize/tools/list/tools/call dispatch
  tools.go             — windows.* tool implementations (calls Native)
  guard.go             — per-call expected_exe foreground guard
  native.go            — Native interface + types (FrontApp, ActivateReq, etc.)
  fakenative.go        — programmable fake for tests
  mcp_test.go          — initialize, tools/list shape
  tools_test.go        — per-tool dispatch + ASCII/non-ASCII routing
  guard_test.go        — guard pass/fail/empty cases

internal/winserver/keymap/            (cross-platform; pure logic)
  keymap.go            — combo string → []KeyEvent (vk + down/up)
  keymap_test.go       — table-driven tests

internal/winnative/                   (//go:build windows on every file)
  dpi.go               — SetProcessDpiAwarenessContext on init()
  foreground.go        — GetForegroundWindow, EnumWindows, SetForegroundWindow + AttachThreadInput
  capture.go           — GetDC + BitBlt + PNG encode
  input.go             — SendInput for mouse/keyboard/wheel; SetCursorPos
  clipboard.go         — OpenClipboard/SetClipboardData + backup/restore
  native.go            — Real implementation of winserver.Native, wraps the above

docs/superpowers/specs/2026-05-07-windows-host-mode-design.md   (already committed)
```

Branch: continue on `feat/qdesk-win-host-mode-design` (renamed via git later if desired). Spec already committed at `2fcc288`.

---

## Task 1: Bootstrap winserver package + Native interface

**Files:**
- Create: `internal/winserver/native.go`
- Create: `internal/winserver/fakenative.go`
- Test: (none yet — the fake gets exercised in Task 2)

- [ ] **Step 1: Create `internal/winserver/native.go`**

```go
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
type FrontApp struct {
	HWND  uintptr `json:"hwnd"`
	PID   uint32  `json:"pid"`
	Exe   string  `json:"exe"`   // basename, lowercased ("notepad.exe")
	Title string  `json:"title"`
}

// ActivateReq targets a window. Priority: HWND > Exe > TitleRegex.
// At least one must be non-zero/empty.
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

// ClickReq. Coordinates are PHYSICAL pixels (the same coordinate
// space Screenshot dimensions use under PerMonitorV2 DPI awareness).
type ClickReq struct {
	X         int      `json:"x"`
	Y         int      `json:"y"`
	Button    string   `json:"button"` // "left" | "right" | "middle"
	Double    bool     `json:"double"`
	Modifiers []string `json:"modifiers,omitempty"` // "ctrl" "shift" "alt" "win"
}

// ScrollReq. dx/dy are wheel notches (positive dy = scroll up,
// matching mac.scroll convention).
type ScrollReq struct {
	X, Y   int `json:"x"`
	DX, DY int `json:"dy"`
}

// ClipboardResp tells the caller whether we managed to put back
// the user's original clipboard contents after pasting.
type ClipboardResp struct {
	Restored bool `json:"clipboard_restored"`
}
```

Note: the `ScrollReq` field tags above only show two — fix when writing: each on its own line.

- [ ] **Step 2: Fix ScrollReq tags (the snippet above is condensed)**

Replace the ScrollReq struct with the explicit version:

```go
type ScrollReq struct {
	X  int `json:"x"`
	Y  int `json:"y"`
	DX int `json:"dx"`
	DY int `json:"dy"`
}
```

- [ ] **Step 3: Create `internal/winserver/fakenative.go`**

```go
package winserver

import (
	"context"
	"errors"
)

// FakeNative is a programmable Native for tests. Each method
// delegates to the corresponding Fn field; nil Fn returns a
// "no handler" error, which most tests should treat as a bug
// (it means a code path called a method tests forgot to wire up).
type FakeNative struct {
	FrontAppFn       func() (FrontApp, error)
	ActivateFn       func(ActivateReq) (ActivateResp, error)
	ScreenshotFn     func() (Screenshot, error)
	ClickFn          func(ClickReq) error
	TypeFn           func(string) error
	KeyFn            func(string) error
	ScrollFn         func(ScrollReq) error
	ClipboardPasteFn func(string) (ClipboardResp, error)
}

func (f *FakeNative) FrontApp(_ context.Context) (FrontApp, error) {
	if f.FrontAppFn == nil {
		return FrontApp{}, errors.New("fake: FrontApp not wired")
	}
	return f.FrontAppFn()
}
func (f *FakeNative) Activate(_ context.Context, r ActivateReq) (ActivateResp, error) {
	if f.ActivateFn == nil {
		return ActivateResp{}, errors.New("fake: Activate not wired")
	}
	return f.ActivateFn(r)
}
func (f *FakeNative) Screenshot(_ context.Context) (Screenshot, error) {
	if f.ScreenshotFn == nil {
		return Screenshot{}, errors.New("fake: Screenshot not wired")
	}
	return f.ScreenshotFn()
}
func (f *FakeNative) Click(_ context.Context, r ClickReq) error {
	if f.ClickFn == nil {
		return errors.New("fake: Click not wired")
	}
	return f.ClickFn(r)
}
func (f *FakeNative) Type(_ context.Context, t string) error {
	if f.TypeFn == nil {
		return errors.New("fake: Type not wired")
	}
	return f.TypeFn(t)
}
func (f *FakeNative) Key(_ context.Context, c string) error {
	if f.KeyFn == nil {
		return errors.New("fake: Key not wired")
	}
	return f.KeyFn(c)
}
func (f *FakeNative) Scroll(_ context.Context, r ScrollReq) error {
	if f.ScrollFn == nil {
		return errors.New("fake: Scroll not wired")
	}
	return f.ScrollFn(r)
}
func (f *FakeNative) ClipboardPaste(_ context.Context, t string) (ClipboardResp, error) {
	if f.ClipboardPasteFn == nil {
		return ClipboardResp{}, errors.New("fake: ClipboardPaste not wired")
	}
	return f.ClipboardPasteFn(t)
}
```

- [ ] **Step 4: Compile check**

Run: `go build ./internal/winserver/`
Expected: clean exit (no output).

- [ ] **Step 5: Commit**

```bash
git add internal/winserver/native.go internal/winserver/fakenative.go
git commit -m "feat(win): winserver Native interface + FakeNative for tests"
```

---

## Task 2: MCP server skeleton — initialize + tools/list + ping

**Files:**
- Create: `internal/winserver/mcp.go`
- Test: `internal/winserver/mcp_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/winserver/mcp_test.go`:

```go
package winserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInitializeReturnsServerInfo(t *testing.T) {
	srv := NewMCPServer(&FakeNative{})
	req := &RPCRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize"}
	resp := srv.Handle(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("Result not a map: %T", resp.Result)
	}
	si, _ := m["serverInfo"].(map[string]any)
	if si["name"] != "qdesk-win" {
		t.Fatalf("serverInfo.name=%v, want qdesk-win", si["name"])
	}
}

func TestToolsListContainsAllWindowsTools(t *testing.T) {
	srv := NewMCPServer(&FakeNative{})
	req := &RPCRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list"}
	resp := srv.Handle(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	m := resp.Result.(map[string]any)
	tools := m["tools"].([]ToolDef)
	want := []string{
		"windows.front_app", "windows.activate", "windows.screenshot",
		"windows.click", "windows.type", "windows.key",
		"windows.scroll", "windows.clipboard_paste",
	}
	got := map[string]bool{}
	for _, td := range tools {
		got[td.Name] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing tool %q in tools/list (got %s)", w, strings.Join(toolNames(tools), ","))
		}
	}
}

func toolNames(tools []ToolDef) []string {
	out := make([]string, len(tools))
	for i, td := range tools {
		out[i] = td.Name
	}
	return out
}
```

- [ ] **Step 2: Run the test, expect failure**

Run: `go test ./internal/winserver/ -run 'TestInitializeReturnsServerInfo|TestToolsListContainsAllWindowsTools' -v`
Expected: compile error — `NewMCPServer`, `RPCRequest`, `ToolDef` undefined.

- [ ] **Step 3: Create `internal/winserver/mcp.go`**

Mirror the structure of `internal/macserver/mcp.go` (referenced — read it for the full patterns). The tool definitions should match the spec exactly.

```go
package winserver

import (
	"context"
	"encoding/json"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "qdesk-win"
	serverVersion   = "0.1.0"
)

// RPC envelope types — identical to macserver.RPC* shape.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ContentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type MCPServer struct {
	native Native
}

func NewMCPServer(n Native) *MCPServer { return &MCPServer{native: n} }

func (s *MCPServer) Handle(ctx context.Context, req *RPCRequest) *RPCResponse {
	resp := &RPCResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.tools()}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "invalid params: " + err.Error()}
			return resp
		}
		out, err := s.callTool(ctx, p.Name, p.Arguments)
		if err != nil {
			resp.Result = &ToolResult{
				Content: []ContentItem{{Type: "text", Text: "error: " + err.Error()}},
				IsError: true,
			}
			return resp
		}
		resp.Result = out
	case "ping":
		resp.Result = map[string]any{}
	default:
		resp.Error = &RPCError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func (s *MCPServer) tools() []ToolDef {
	return []ToolDef{
		{
			Name:        "windows.front_app",
			Description: "Return HWND, PID, exe basename and window title of the current foreground window. Use to discover what's in front before screenshotting or sending input.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "windows.activate",
			Description: "Bring a window to the foreground. Provide hwnd, exe (basename like \"notepad.exe\"), or title_regex. Priority: hwnd > exe > title_regex. Returns actually_foreground=false if Windows refused the focus change (caller must not assume success).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"hwnd":        map[string]any{"type": "integer"},
					"exe":         map[string]any{"type": "string"},
					"title_regex": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "windows.screenshot",
			Description: "Capture the primary monitor as PNG (physical pixels). No foreground guard. Returns the image plus the foreground exe + title.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "windows.click",
			Description: "Click at PHYSICAL pixel coordinates. Optional expected_exe verifies that exe is in front before posting; omit to skip the check.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":            map[string]any{"type": "integer"},
					"y":            map[string]any{"type": "integer"},
					"button":       map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "default": "left"},
					"double":       map[string]any{"type": "boolean", "default": false},
					"modifiers":    map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"ctrl", "shift", "alt", "win"}}},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "windows.type",
			Description: "Type Unicode text at the current focus. ASCII-only text uses SendInput KEYEVENTF_UNICODE; non-ASCII auto-routes through the clipboard fallback (some old Win32 controls drop unicode events). Optional expected_exe verifies the right exe is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":         map[string]any{"type": "string"},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "windows.key",
			Description: "Send a key combo at the current focus, e.g. \"return\", \"escape\", \"ctrl+f\", \"win+r\", \"alt+tab\". Optional expected_exe verifies the right exe is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"combo":        map[string]any{"type": "string"},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"combo"},
			},
		},
		{
			Name:        "windows.scroll",
			Description: "Wheel-scroll at physical pixel point (x, y). Positive dy scrolls up. dx (horizontal wheel) is accepted but many apps ignore it. Optional expected_exe verifies the right exe is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":            map[string]any{"type": "integer"},
					"y":            map[string]any{"type": "integer"},
					"dy":           map[string]any{"type": "integer"},
					"dx":           map[string]any{"type": "integer", "default": 0},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"x", "y", "dy"},
			},
		},
		{
			Name:        "windows.clipboard_paste",
			Description: "Set the system clipboard to `text`, post ctrl+v at the focused window, wait briefly, then restore the original clipboard. Returns clipboard_restored=false if backup or restore failed (paste still happens). Optional expected_exe verifies the right exe is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":         map[string]any{"type": "string"},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
		},
	}
}

// callTool stub — real dispatch in Task 4+. For now everything errors.
func (s *MCPServer) callTool(_ context.Context, name string, _ json.RawMessage) (*ToolResult, error) {
	return nil, errToolNotImpl(name)
}

func errToolNotImpl(name string) error {
	return &notImplementedError{name: name}
}

type notImplementedError struct{ name string }

func (e *notImplementedError) Error() string { return "tool not implemented yet: " + e.name }
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/winserver/ -run 'TestInitializeReturnsServerInfo|TestToolsListContainsAllWindowsTools' -v`
Expected: PASS for both.

- [ ] **Step 5: Commit**

```bash
git add internal/winserver/mcp.go internal/winserver/mcp_test.go
git commit -m "feat(win): MCP server skeleton — initialize, tools/list, ping"
```

---

## Task 3: Per-call expected_exe guard

**Files:**
- Create: `internal/winserver/guard.go`
- Test: `internal/winserver/guard_test.go`

- [ ] **Step 1: Write the failing test**

```go
package winserver

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGuardEmptyExeSkipsCheck(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		t.Fatalf("FrontApp should NOT be called when expected_exe is empty")
		return FrontApp{}, nil
	}}
	if err := requireForeground(context.Background(), n, ""); err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
}

func TestGuardMatchPasses(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		return FrontApp{Exe: "notepad.exe"}, nil
	}}
	if err := requireForeground(context.Background(), n, "notepad.exe"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestGuardCaseInsensitive(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		return FrontApp{Exe: "Notepad.EXE"}, nil
	}}
	if err := requireForeground(context.Background(), n, "notepad.exe"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestGuardMismatchFails(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		return FrontApp{Exe: "explorer.exe", Title: "File Explorer"}, nil
	}}
	err := requireForeground(context.Background(), n, "notepad.exe")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "explorer.exe") {
		t.Errorf("error should name actual front exe; got %q", err.Error())
	}
}

func TestGuardFrontAppErrorPropagates(t *testing.T) {
	want := errors.New("syscall failed")
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) { return FrontApp{}, want }}
	err := requireForeground(context.Background(), n, "notepad.exe")
	if err == nil || !strings.Contains(err.Error(), "syscall failed") {
		t.Fatalf("want wrapped syscall failed, got %v", err)
	}
}
```

- [ ] **Step 2: Run test, expect failure**

Run: `go test ./internal/winserver/ -run TestGuard -v`
Expected: compile error, `requireForeground` undefined.

- [ ] **Step 3: Create `internal/winserver/guard.go`**

```go
package winserver

import (
	"context"
	"fmt"
	"strings"
)

// requireForeground returns nil when expectedExe is empty (no check)
// or when the foreground exe basename matches expectedExe (case
// insensitive). Otherwise returns a structured error naming what's
// actually in front so the LLM can decide whether to call activate.
func requireForeground(ctx context.Context, n Native, expectedExe string) error {
	if expectedExe == "" {
		return nil
	}
	fa, err := n.FrontApp(ctx)
	if err != nil {
		return fmt.Errorf("frontApp: %w", err)
	}
	if !strings.EqualFold(fa.Exe, expectedExe) {
		return fmt.Errorf("foreground-mismatch: front exe is %q (title %q), expected %q; call windows.activate first",
			fa.Exe, fa.Title, expectedExe)
	}
	return nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/winserver/ -run TestGuard -v`
Expected: PASS for all 5.

- [ ] **Step 5: Commit**

```bash
git add internal/winserver/guard.go internal/winserver/guard_test.go
git commit -m "feat(win): per-call expected_exe foreground guard"
```

---

## Task 4: windows.front_app + windows.screenshot

**Files:**
- Create: `internal/winserver/tools.go`
- Test: `internal/winserver/tools_test.go`
- Modify: `internal/winserver/mcp.go` (replace stub callTool)

- [ ] **Step 1: Write the failing tests**

Add to `internal/winserver/tools_test.go`:

```go
package winserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func callTool(t *testing.T, srv *MCPServer, name string, args any) *ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := srv.callTool(context.Background(), name, raw)
	if err != nil {
		t.Fatalf("callTool returned dispatch error: %v", err)
	}
	return out
}

func TestFrontAppToolReturnsExeAndTitle(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		return FrontApp{HWND: 0xDEAD, PID: 1234, Exe: "notepad.exe", Title: "Untitled - Notepad"}, nil
	}}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.front_app", map[string]any{})
	if res.IsError {
		t.Fatalf("unexpected IsError: %+v", res)
	}
	got := res.Content[0].Text
	for _, want := range []string{"notepad.exe", "1234", "Untitled - Notepad"} {
		if !strings.Contains(got, want) {
			t.Errorf("front_app text %q missing %q", got, want)
		}
	}
}

func TestScreenshotToolReturnsImageContent(t *testing.T) {
	n := &FakeNative{
		FrontAppFn: func() (FrontApp, error) {
			return FrontApp{Exe: "explorer.exe", Title: "Desktop"}, nil
		},
		ScreenshotFn: func() (Screenshot, error) {
			return Screenshot{PNGBase64: "iVBORw0KGgo=", Width: 1920, Height: 1080}, nil
		},
	}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.screenshot", map[string]any{})
	if res.IsError {
		t.Fatalf("unexpected IsError: %+v", res)
	}
	if len(res.Content) < 2 {
		t.Fatalf("expected image + text content, got %+v", res.Content)
	}
	if res.Content[0].Type != "image" || res.Content[0].MIMEType != "image/png" {
		t.Errorf("first content not PNG image: %+v", res.Content[0])
	}
	if res.Content[0].Data != "iVBORw0KGgo=" {
		t.Errorf("PNG data not propagated: %q", res.Content[0].Data)
	}
	if !strings.Contains(res.Content[1].Text, "explorer.exe") || !strings.Contains(res.Content[1].Text, "1920x1080") {
		t.Errorf("screenshot text missing exe or dims: %q", res.Content[1].Text)
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/winserver/ -run 'TestFrontAppTool|TestScreenshotTool' -v`
Expected: failure — `callTool` returns "tool not implemented yet".

- [ ] **Step 3: Create `internal/winserver/tools.go`**

```go
package winserver

import (
	"context"
	"encoding/json"
	"fmt"
)

func (s *MCPServer) callToolReal(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	switch name {
	case "windows.front_app":
		return s.toolFrontApp(ctx)
	case "windows.screenshot":
		return s.toolScreenshot(ctx)
	default:
		return errToolResult(fmt.Errorf("unknown tool: %s", name)), nil
	}
}

func (s *MCPServer) toolFrontApp(ctx context.Context) (*ToolResult, error) {
	fa, err := s.native.FrontApp(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("frontApp.exe=%s pid=%d title=%q hwnd=0x%x", fa.Exe, fa.PID, fa.Title, fa.HWND),
	}}}, nil
}

func (s *MCPServer) toolScreenshot(ctx context.Context) (*ToolResult, error) {
	fa, err := s.native.FrontApp(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	shot, err := s.native.Screenshot(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{
		{Type: "image", MIMEType: "image/png", Data: shot.PNGBase64},
		{Type: "text", Text: fmt.Sprintf("frontApp.exe=%s title=%q  size=%dx%d (physical pixels)",
			fa.Exe, fa.Title, shot.Width, shot.Height)},
	}}, nil
}

func errToolResult(err error) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentItem{{Type: "text", Text: err.Error()}},
	}
}
```

- [ ] **Step 4: Wire callTool through to callToolReal — modify `mcp.go`**

In `internal/winserver/mcp.go`, replace the stub:

```go
func (s *MCPServer) callTool(_ context.Context, name string, _ json.RawMessage) (*ToolResult, error) {
	return nil, errToolNotImpl(name)
}
```

with:

```go
func (s *MCPServer) callTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	return s.callToolReal(ctx, name, args)
}
```

And delete the `errToolNotImpl` function and `notImplementedError` type from mcp.go.

- [ ] **Step 5: Run tests, expect pass**

Run: `go test ./internal/winserver/ -v`
Expected: all tests so far pass.

- [ ] **Step 6: Commit**

```bash
git add internal/winserver/tools.go internal/winserver/tools_test.go internal/winserver/mcp.go
git commit -m "feat(win): windows.front_app and windows.screenshot tools"
```

---

## Task 5: windows.activate

**Files:**
- Modify: `internal/winserver/tools.go`
- Modify: `internal/winserver/tools_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `tools_test.go`:

```go
func TestActivateRequiresAtLeastOneTarget(t *testing.T) {
	srv := NewMCPServer(&FakeNative{})
	res := callTool(t, srv, "windows.activate", map[string]any{})
	if !res.IsError {
		t.Fatalf("expected IsError, got %+v", res)
	}
	if !strings.Contains(res.Content[0].Text, "hwnd, exe, or title_regex") {
		t.Errorf("error message should mention required fields, got %q", res.Content[0].Text)
	}
}

func TestActivatePassesArgsToNative(t *testing.T) {
	var got ActivateReq
	n := &FakeNative{ActivateFn: func(r ActivateReq) (ActivateResp, error) {
		got = r
		return ActivateResp{HWND: 0xBEEF, ActuallyForeground: true}, nil
	}}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.activate", map[string]any{
		"exe":         "notepad.exe",
		"title_regex": "Untitled.*",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if got.Exe != "notepad.exe" || got.TitleRegex != "Untitled.*" {
		t.Errorf("native got wrong req: %+v", got)
	}
	if !strings.Contains(res.Content[0].Text, "actually_foreground=true") {
		t.Errorf("text should report actually_foreground; got %q", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "0xbeef") {
		t.Errorf("text should report hwnd; got %q", res.Content[0].Text)
	}
}

func TestActivateReportsForegroundFailureWithoutErroring(t *testing.T) {
	n := &FakeNative{ActivateFn: func(r ActivateReq) (ActivateResp, error) {
		return ActivateResp{HWND: 0x1234, ActuallyForeground: false}, nil
	}}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.activate", map[string]any{"exe": "notepad.exe"})
	// Not an error — Windows refused the focus change but the call succeeded.
	// The caller decides what to do based on actually_foreground.
	if res.IsError {
		t.Fatalf("activate should not IsError when only foreground steal failed; got %+v", res)
	}
	if !strings.Contains(res.Content[0].Text, "actually_foreground=false") {
		t.Errorf("text should expose actually_foreground=false; got %q", res.Content[0].Text)
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/winserver/ -run TestActivate -v`
Expected: failure — "unknown tool: windows.activate".

- [ ] **Step 3: Add toolActivate to `tools.go`**

In `tools.go`, add to the switch in `callToolReal`:

```go
	case "windows.activate":
		return s.toolActivate(ctx, args)
```

And append:

```go
func (s *MCPServer) toolActivate(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		HWND       uintptr `json:"hwnd"`
		Exe        string  `json:"exe"`
		TitleRegex string  `json:"title_regex"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return errToolResult(err), nil
		}
	}
	if in.HWND == 0 && in.Exe == "" && in.TitleRegex == "" {
		return errToolResult(fmt.Errorf("activate: provide one of hwnd, exe, or title_regex")), nil
	}
	resp, err := s.native.Activate(ctx, ActivateReq{HWND: in.HWND, Exe: in.Exe, TitleRegex: in.TitleRegex})
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("activated hwnd=0x%x actually_foreground=%t", resp.HWND, resp.ActuallyForeground),
	}}}, nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/winserver/ -run TestActivate -v`
Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/winserver/tools.go internal/winserver/tools_test.go
git commit -m "feat(win): windows.activate tool with hwnd/exe/title_regex priority"
```

---

## Task 6: windows.click + windows.key + windows.scroll (guard-using tools)

These three tools all share the same shape: parse args, run guard, delegate to Native, return text describing what was done.

**Files:**
- Modify: `internal/winserver/tools.go`
- Modify: `internal/winserver/tools_test.go`

- [ ] **Step 1: Write failing tests**

Append to `tools_test.go`:

```go
func TestClickGuardBlocksOnMismatch(t *testing.T) {
	called := false
	n := &FakeNative{
		FrontAppFn: func() (FrontApp, error) { return FrontApp{Exe: "explorer.exe"}, nil },
		ClickFn:    func(ClickReq) error { called = true; return nil },
	}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.click", map[string]any{
		"x": 100, "y": 200, "expected_exe": "notepad.exe",
	})
	if !res.IsError {
		t.Fatalf("expected IsError on guard mismatch")
	}
	if called {
		t.Errorf("Click should NOT be called when guard fails")
	}
}

func TestClickPassesArgsThrough(t *testing.T) {
	var got ClickReq
	n := &FakeNative{ClickFn: func(r ClickReq) error { got = r; return nil }}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.click", map[string]any{
		"x": 100, "y": 200, "button": "right", "double": true,
		"modifiers": []string{"ctrl", "shift"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if got.X != 100 || got.Y != 200 || got.Button != "right" || !got.Double {
		t.Errorf("native got wrong req: %+v", got)
	}
	if len(got.Modifiers) != 2 {
		t.Errorf("modifiers not propagated: %v", got.Modifiers)
	}
}

func TestClickDefaultButtonIsLeft(t *testing.T) {
	var got ClickReq
	n := &FakeNative{ClickFn: func(r ClickReq) error { got = r; return nil }}
	srv := NewMCPServer(n)
	callTool(t, srv, "windows.click", map[string]any{"x": 0, "y": 0})
	if got.Button != "left" {
		t.Errorf("default button should be left; got %q", got.Button)
	}
}

func TestKeyDelegatesCombo(t *testing.T) {
	var got string
	n := &FakeNative{KeyFn: func(c string) error { got = c; return nil }}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.key", map[string]any{"combo": "ctrl+f"})
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if got != "ctrl+f" {
		t.Errorf("native got combo=%q want ctrl+f", got)
	}
}

func TestScrollDelegates(t *testing.T) {
	var got ScrollReq
	n := &FakeNative{ScrollFn: func(r ScrollReq) error { got = r; return nil }}
	srv := NewMCPServer(n)
	callTool(t, srv, "windows.scroll", map[string]any{"x": 50, "y": 60, "dy": 3, "dx": 0})
	if got.X != 50 || got.Y != 60 || got.DY != 3 {
		t.Errorf("scroll args not propagated: %+v", got)
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/winserver/ -run 'TestClick|TestKey|TestScroll' -v`
Expected: failure — unknown tools.

- [ ] **Step 3: Add the three tools to `tools.go`**

In `callToolReal`, add to the switch:

```go
	case "windows.click":
		return s.toolClick(ctx, args)
	case "windows.key":
		return s.toolKey(ctx, args)
	case "windows.scroll":
		return s.toolScroll(ctx, args)
```

Append:

```go
func (s *MCPServer) toolClick(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		X, Y        int
		Button      string
		Double      bool
		Modifiers   []string
		ExpectedExe string `json:"expected_exe"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return errToolResult(err), nil
		}
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	if in.Button == "" {
		in.Button = "left"
	}
	if err := s.native.Click(ctx, ClickReq{
		X: in.X, Y: in.Y, Button: in.Button, Double: in.Double, Modifiers: in.Modifiers,
	}); err != nil {
		return errToolResult(err), nil
	}
	dbl := ""
	if in.Double {
		dbl = " (double)"
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("clicked %s%s at (%d, %d)", in.Button, dbl, in.X, in.Y),
	}}}, nil
}

func (s *MCPServer) toolKey(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		Combo       string
		ExpectedExe string `json:"expected_exe"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	if err := s.native.Key(ctx, in.Combo); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("sent key %q", in.Combo),
	}}}, nil
}

func (s *MCPServer) toolScroll(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		X, Y, DX, DY int
		ExpectedExe  string `json:"expected_exe"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	if err := s.native.Scroll(ctx, ScrollReq{X: in.X, Y: in.Y, DX: in.DX, DY: in.DY}); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("scrolled (dx=%d dy=%d) at (%d, %d)", in.DX, in.DY, in.X, in.Y),
	}}}, nil
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/winserver/ -v`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/winserver/tools.go internal/winserver/tools_test.go
git commit -m "feat(win): windows.click, windows.key, windows.scroll with guard"
```

---

## Task 7: windows.type with ASCII vs non-ASCII routing

**Files:**
- Modify: `internal/winserver/tools.go`
- Modify: `internal/winserver/tools_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestTypeASCIIUsesNativeType(t *testing.T) {
	var typed string
	pasted := false
	n := &FakeNative{
		TypeFn:           func(s string) error { typed = s; return nil },
		ClipboardPasteFn: func(string) (ClipboardResp, error) { pasted = true; return ClipboardResp{}, nil },
	}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.type", map[string]any{"text": "hello world 123"})
	if res.IsError {
		t.Fatalf("unexpected: %+v", res)
	}
	if typed != "hello world 123" {
		t.Errorf("Type not called with right text: %q", typed)
	}
	if pasted {
		t.Errorf("ClipboardPaste should NOT be called for ASCII text")
	}
	if !strings.Contains(res.Content[0].Text, "unicode") {
		t.Errorf("result should mention path=unicode; got %q", res.Content[0].Text)
	}
}

func TestTypeNonASCIIUsesClipboardPaste(t *testing.T) {
	typed := false
	var pasted string
	n := &FakeNative{
		TypeFn:           func(string) error { typed = true; return nil },
		ClipboardPasteFn: func(s string) (ClipboardResp, error) { pasted = s; return ClipboardResp{Restored: true}, nil },
	}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.type", map[string]any{"text": "你好 qdesk"})
	if res.IsError {
		t.Fatalf("unexpected: %+v", res)
	}
	if typed {
		t.Errorf("Type should NOT be called for non-ASCII text")
	}
	if pasted != "你好 qdesk" {
		t.Errorf("ClipboardPaste not called with right text: %q", pasted)
	}
	if !strings.Contains(res.Content[0].Text, "clipboard") {
		t.Errorf("result should mention path=clipboard; got %q", res.Content[0].Text)
	}
}

func TestTypeReportsClipboardRestoreFailure(t *testing.T) {
	n := &FakeNative{
		ClipboardPasteFn: func(string) (ClipboardResp, error) { return ClipboardResp{Restored: false}, nil },
	}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.type", map[string]any{"text": "中文"})
	if res.IsError {
		t.Fatalf("type should not error when only restore failed")
	}
	if !strings.Contains(res.Content[0].Text, "clipboard_restored=false") {
		t.Errorf("should surface restore failure; got %q", res.Content[0].Text)
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/winserver/ -run TestType -v`
Expected: failure — unknown tool windows.type.

- [ ] **Step 3: Add toolType + toolClipboardPaste to `tools.go`**

In `callToolReal` switch:

```go
	case "windows.type":
		return s.toolType(ctx, args)
	case "windows.clipboard_paste":
		return s.toolClipboardPaste(ctx, args)
```

Append:

```go
func (s *MCPServer) toolType(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		Text        string
		ExpectedExe string `json:"expected_exe"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	if isASCII(in.Text) {
		if err := s.native.Type(ctx, in.Text); err != nil {
			return errToolResult(err), nil
		}
		return &ToolResult{Content: []ContentItem{{Type: "text",
			Text: fmt.Sprintf("typed %d chars (path=unicode)", len([]rune(in.Text))),
		}}}, nil
	}
	resp, err := s.native.ClipboardPaste(ctx, in.Text)
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("typed %d chars (path=clipboard) clipboard_restored=%t",
			len([]rune(in.Text)), resp.Restored),
	}}}, nil
}

func (s *MCPServer) toolClipboardPaste(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		Text        string
		ExpectedExe string `json:"expected_exe"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	resp, err := s.native.ClipboardPaste(ctx, in.Text)
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("pasted %d chars clipboard_restored=%t",
			len([]rune(in.Text)), resp.Restored),
	}}}, nil
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 0x7F {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/winserver/ -v`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/winserver/tools.go internal/winserver/tools_test.go
git commit -m "feat(win): windows.type ASCII/non-ASCII routing + windows.clipboard_paste"
```

---

## Task 8: keymap parser (cross-platform, pure logic)

The keymap turns combo strings like `"ctrl+f"` or `"win+r"` into a sequence of virtual-key events that the windows-only Native impl will translate into SendInput calls. Keeping this in a sub-package means we can unit-test it on macOS without any Win32 dependency.

**Files:**
- Create: `internal/winserver/keymap/keymap.go`
- Test: `internal/winserver/keymap/keymap_test.go`

- [ ] **Step 1: Write failing tests**

```go
package keymap

import "testing"

func TestParseSimpleLetterCombo(t *testing.T) {
	got, err := Parse("ctrl+f")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	want := []Event{
		{VK: VKControl, Down: true},
		{VK: 'F', Down: true},
		{VK: 'F', Down: false},
		{VK: VKControl, Down: false},
	}
	if !equalEvents(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestParseMultipleModifiers(t *testing.T) {
	got, err := Parse("ctrl+shift+a")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// ctrl down, shift down, A down, A up, shift up, ctrl up
	if len(got) != 6 {
		t.Fatalf("expected 6 events, got %d: %+v", len(got), got)
	}
	if got[0].VK != VKControl || got[1].VK != VKShift || got[2].VK != 'A' {
		t.Errorf("modifier order wrong: %+v", got)
	}
	if !got[2].Down || got[3].Down {
		t.Errorf("A should be down then up: %+v", got[2:4])
	}
}

func TestParseWinKey(t *testing.T) {
	got, err := Parse("win+r")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got[0].VK != VKLWin {
		t.Errorf("first event should be VKLWin, got 0x%x", got[0].VK)
	}
}

func TestParseSingleKey(t *testing.T) {
	got, err := Parse("return")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("single key should be 2 events; got %+v", got)
	}
	if got[0].VK != VKReturn || !got[0].Down || got[1].Down {
		t.Errorf("return events wrong: %+v", got)
	}
}

func TestParseAltTab(t *testing.T) {
	got, err := Parse("alt+tab")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got[0].VK != VKMenu || got[1].VK != VKTab {
		t.Errorf("alt+tab parse wrong: %+v", got)
	}
}

func TestParseUnknownKey(t *testing.T) {
	_, err := Parse("ctrl+notakey")
	if err == nil {
		t.Errorf("expected error for unknown key")
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Errorf("expected error for empty combo")
	}
}

func equalEvents(a, b []Event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/winserver/keymap/ -v`
Expected: package not found.

- [ ] **Step 3: Create `internal/winserver/keymap/keymap.go`**

```go
// Package keymap parses combo strings like "ctrl+f" / "win+r" /
// "alt+tab" into a sequence of virtual-key down/up events. Pure
// logic — no Win32 dependency — so it's tested on macOS dev hosts.
//
// VK codes match the Windows virtual-key constants (User32 VK_*),
// referenced by the windows-only winnative package when building
// SendInput INPUT structs.
package keymap

import (
	"fmt"
	"strings"
)

// Event is one keyboard transition.
type Event struct {
	VK   uint16
	Down bool
}

// Subset of Win32 VK_* values we use. Add more as needed.
const (
	VKBack    = 0x08
	VKTab     = 0x09
	VKReturn  = 0x0D
	VKShift   = 0x10
	VKControl = 0x11
	VKMenu    = 0x12 // Alt
	VKEscape  = 0x1B
	VKSpace   = 0x20
	VKLeft    = 0x25
	VKUp      = 0x26
	VKRight   = 0x27
	VKDown    = 0x28
	VKDelete  = 0x2E
	VKLWin    = 0x5B
	VKF1      = 0x70
	VKF2      = 0x71
	VKF3      = 0x72
	VKF4      = 0x73
	VKF5      = 0x74
	VKF6      = 0x75
	VKF7      = 0x76
	VKF8      = 0x77
	VKF9      = 0x78
	VKF10     = 0x79
	VKF11     = 0x7A
	VKF12     = 0x7B
)

var modifierVK = map[string]uint16{
	"ctrl":    VKControl,
	"control": VKControl,
	"shift":   VKShift,
	"alt":     VKMenu,
	"meta":    VKMenu, // alias
	"win":     VKLWin,
	"windows": VKLWin,
	"super":   VKLWin,
}

var namedKeyVK = map[string]uint16{
	"return":    VKReturn,
	"enter":     VKReturn,
	"tab":       VKTab,
	"escape":    VKEscape,
	"esc":       VKEscape,
	"backspace": VKBack,
	"delete":    VKDelete,
	"del":       VKDelete,
	"space":     VKSpace,
	"left":      VKLeft,
	"right":     VKRight,
	"up":        VKUp,
	"down":      VKDown,
	"f1":        VKF1, "f2": VKF2, "f3": VKF3, "f4": VKF4,
	"f5": VKF5, "f6": VKF6, "f7": VKF7, "f8": VKF8,
	"f9": VKF9, "f10": VKF10, "f11": VKF11, "f12": VKF12,
}

// Parse turns "ctrl+shift+a" into the press/release sequence for
// SendInput: each modifier down (in order), the main key down/up,
// then each modifier up (reverse order).
//
// The "main key" is the LAST token; everything before must be a
// known modifier. ASCII letters and digits map to their uppercase
// rune (Windows VK codes for A-Z and 0-9 are the ASCII codepoints).
func Parse(combo string) ([]Event, error) {
	combo = strings.TrimSpace(combo)
	if combo == "" {
		return nil, fmt.Errorf("empty combo")
	}
	parts := strings.Split(strings.ToLower(combo), "+")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty combo")
	}

	mods := parts[:len(parts)-1]
	mainKey := parts[len(parts)-1]

	// Validate modifiers up-front.
	modVKs := make([]uint16, 0, len(mods))
	for _, m := range mods {
		vk, ok := modifierVK[m]
		if !ok {
			return nil, fmt.Errorf("unknown modifier: %q", m)
		}
		modVKs = append(modVKs, vk)
	}

	mainVK, err := resolveKey(mainKey)
	if err != nil {
		return nil, err
	}

	out := make([]Event, 0, 2+2*len(modVKs))
	for _, vk := range modVKs {
		out = append(out, Event{VK: vk, Down: true})
	}
	out = append(out, Event{VK: mainVK, Down: true}, Event{VK: mainVK, Down: false})
	for i := len(modVKs) - 1; i >= 0; i-- {
		out = append(out, Event{VK: modVKs[i], Down: false})
	}
	return out, nil
}

func resolveKey(k string) (uint16, error) {
	if vk, ok := namedKeyVK[k]; ok {
		return vk, nil
	}
	// Single ASCII letter or digit?
	if len(k) == 1 {
		c := k[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint16(c - 32), nil // upper-case rune
		case c >= '0' && c <= '9':
			return uint16(c), nil
		}
	}
	return 0, fmt.Errorf("unknown key: %q", k)
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/winserver/keymap/ -v`
Expected: 7 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/winserver/keymap/
git commit -m "feat(win): keymap parser for combo strings (ctrl+f, win+r, etc.)"
```

---

## Task 9: cross-platform stub for cmd/qdesk-win

So `go build ./...` on macOS doesn't fail when a dev runs the full project build.

**Files:**
- Create: `cmd/qdesk-win/main_other.go`

- [ ] **Step 1: Create the stub**

```go
//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "qdesk-win is windows-only; build with GOOS=windows")
	os.Exit(1)
}
```

- [ ] **Step 2: Verify it compiles on the dev host**

Run: `go build ./cmd/qdesk-win/`
Expected: compiles, produces a `qdesk-win` binary that prints "qdesk-win is windows-only" and exits 1 if invoked. (Don't bother running it.)

- [ ] **Step 3: Commit**

```bash
git add cmd/qdesk-win/main_other.go
git commit -m "build(win): non-windows stub for cmd/qdesk-win"
```

---

## Task 10: cmd/qdesk-win HTTP transport (windows-only)

Mirror `cmd/qdesk-mac/http.go` minus `caffeinate`. Includes bearer auth, CIDR allowlist, optional Tailscale-headers logging.

**Files:**
- Create: `cmd/qdesk-win/http.go` (build tag `//go:build windows`)

- [ ] **Step 1: Create `cmd/qdesk-win/http.go`**

This is a near-verbatim copy of `cmd/qdesk-mac/http.go` with the following changes:
- `//go:build windows` at the top
- Drop `Caffeinate` field, drop `startCaffeinate` function and its call in `runHTTP`
- Replace `macserver.MCPServer` with `winserver.MCPServer`
- Replace `macserver.RPCRequest`/`RPCResponse` with `winserver.RPCRequest`/`RPCResponse`
- Replace error message "qdesk-mac" with "qdesk-win" / "QDESK_WIN_API_KEY"

```go
//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jeffwang/qdesk/internal/winserver"
)

type httpConfig struct {
	Listen               string
	APIKey               string
	TrustedCIDR          string
	TrustTailscaleHeader bool
}

func runHTTP(ctx context.Context, srv *winserver.MCPServer, cfg httpConfig) error {
	if cfg.APIKey == "" {
		return errors.New("HTTP mode refuses to start with an empty --api-key (or QDESK_WIN_API_KEY env)")
	}
	cidrs, err := parseCIDRs(cfg.TrustedCIDR)
	if err != nil {
		return fmt.Errorf("--trusted-cidr: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`+"\n")
	})
	mcpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		var req winserver.RPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON-RPC: "+err.Error(), http.StatusBadRequest)
			return
		}
		resp := srv.Handle(r.Context(), &req)
		writeMCPResponse(w, r, resp)
	})

	var chained http.Handler = mcpHandler
	if cfg.TrustTailscaleHeader {
		chained = logTailscaleIdentity(chained)
	}
	chained = bearerAuth(cfg.APIKey, chained)
	if len(cidrs) > 0 {
		chained = cidrAllowlist(cidrs, chained)
	}
	mux.Handle("/mcp", chained)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return fmt.Errorf("http listen: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		return nil
	}
}

func writeMCPResponse(w http.ResponseWriter, r *http.Request, resp *winserver.RPCResponse) {
	body, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "marshal response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if clientWantsSSE(r) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(body)
		_, _ = w.Write([]byte("\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n"))
}

func clientWantsSSE(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}
	for _, part := range strings.Split(accept, ",") {
		mt := strings.TrimSpace(part)
		if i := strings.IndexByte(mt, ';'); i >= 0 {
			mt = strings.TrimSpace(mt[:i])
		}
		if strings.EqualFold(mt, "text/event-stream") {
			return true
		}
	}
	return false
}

func bearerAuth(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		got := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if !constantTimeEqual(got, key) {
			http.Error(w, "invalid api key", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func parseCIDRs(csv string) ([]*net.IPNet, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, nil
	}
	var out []*net.IPNet
	for _, s := range strings.Split(csv, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func cidrAllowlist(allowed []*net.IPNet, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := remoteClientIP(r)
		if ip == nil {
			http.Error(w, "cannot determine client IP", http.StatusForbidden)
			return
		}
		for _, n := range allowed {
			if n.Contains(ip) {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Error(w, "client IP not in trusted CIDR", http.StatusForbidden)
	})
}

func remoteClientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return nil
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return nil
	}
	if peer.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.IndexByte(xff, ','); idx >= 0 {
				xff = xff[:idx]
			}
			if forwarded := net.ParseIP(strings.TrimSpace(xff)); forwarded != nil {
				return forwarded
			}
		}
	}
	return peer
}

func logTailscaleIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login := r.Header.Get("Tailscale-User-Login")
		name := r.Header.Get("Tailscale-User-Name")
		if login != "" || name != "" {
			logf("tailscale request from login=%q name=%q remote=%s path=%s",
				login, name, r.RemoteAddr, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 2: Defer compile check until Task 12 (main.go provides logf and brings everything together)**

- [ ] **Step 3: Commit (file compiles only when paired with Task 12 main.go; that's fine — commits don't have to individually build the whole binary in a TDD plan, only tests and full builds at checkpoints)**

```bash
git add cmd/qdesk-win/http.go
git commit -m "feat(win): cmd/qdesk-win HTTP transport (bearer auth, CIDR, Tailscale headers)"
```

---

## Task 11: winnative real implementation (windows-only)

This task is the big one — actual Win32 syscalls. It cannot be unit-tested on macOS; correctness is validated by the E2E task on the VM.

**Files:**
- Create: `internal/winnative/dpi.go`
- Create: `internal/winnative/foreground.go`
- Create: `internal/winnative/capture.go`
- Create: `internal/winnative/input.go`
- Create: `internal/winnative/clipboard.go`
- Create: `internal/winnative/native.go`

All files start with `//go:build windows`.

- [ ] **Step 1: Create `internal/winnative/dpi.go`**

```go
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
```

- [ ] **Step 2: Create `internal/winnative/foreground.go`**

```go
//go:build windows

package winnative

import (
	"context"
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

	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

const (
	swRestore                  = 9
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
// can change focus. The trick: AttachThreadInput to the current
// foreground thread, then SetForegroundWindow.
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

func _ctx(ctx context.Context) context.Context { return ctx } // silence unused import warnings during incremental edits
```

- [ ] **Step 3: Create `internal/winnative/capture.go`**

```go
//go:build windows

package winnative

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"unsafe"

	"github.com/jeffwang/qdesk/internal/winserver"
	"golang.org/x/sys/windows"
)

var (
	gdi32                = windows.NewLazySystemDLL("gdi32.dll")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procBitBlt           = gdi32.NewProc("BitBlt")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procDeleteDC         = gdi32.NewProc("DeleteDC")
	procGetDIBits        = gdi32.NewProc("GetDIBits")
)

const (
	smCXScreen = 0
	smCYScreen = 1
	srcCopy    = 0x00CC0020
	biRGB      = 0
	dibRGB     = 0
)

type bitmapInfoHeader struct {
	Size            uint32
	Width           int32
	Height          int32
	Planes          uint16
	BitCount        uint16
	Compression     uint32
	SizeImage       uint32
	XPelsPerMeter   int32
	YPelsPerMeter   int32
	ClrUsed         uint32
	ClrImportant    uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32 // unused; placeholder to match ABI
}

func screenshot() (winserver.Screenshot, error) {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if w == 0 || h == 0 {
		return winserver.Screenshot{}, fmt.Errorf("GetSystemMetrics returned zero size")
	}

	srcDC, _, _ := procGetDC.Call(0)
	if srcDC == 0 {
		return winserver.Screenshot{}, fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, srcDC)

	memDC, _, _ := procCreateCompatibleDC.Call(srcDC)
	if memDC == 0 {
		return winserver.Screenshot{}, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	bmp, _, _ := procCreateCompatibleBitmap.Call(srcDC, w, h)
	if bmp == 0 {
		return winserver.Screenshot{}, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(bmp)

	procSelectObject.Call(memDC, bmp)
	r, _, _ := procBitBlt.Call(memDC, 0, 0, w, h, srcDC, 0, 0, srcCopy)
	if r == 0 {
		return winserver.Screenshot{}, fmt.Errorf("BitBlt failed")
	}

	pixelCount := int(w * h)
	pixels := make([]byte, pixelCount*4)
	bi := bitmapInfo{Header: bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width: int32(w), Height: -int32(h), // negative = top-down
		Planes: 1, BitCount: 32, Compression: biRGB,
	}}
	gr, _, _ := procGetDIBits.Call(memDC, bmp, 0, h,
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&bi)), dibRGB)
	if gr == 0 {
		return winserver.Screenshot{}, fmt.Errorf("GetDIBits failed")
	}

	// BGRA → RGBA in place.
	for i := 0; i < len(pixels); i += 4 {
		pixels[i], pixels[i+2] = pixels[i+2], pixels[i]
	}

	img := &image.RGBA{
		Pix:    pixels,
		Stride: int(w) * 4,
		Rect:   image.Rect(0, 0, int(w), int(h)),
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return winserver.Screenshot{}, fmt.Errorf("png encode: %w", err)
	}
	return winserver.Screenshot{
		PNGBase64: base64.StdEncoding.EncodeToString(buf.Bytes()),
		Width:     int(w),
		Height:    int(h),
	}, nil
}
```

- [ ] **Step 4: Create `internal/winnative/input.go`**

```go
//go:build windows

package winnative

import (
	"fmt"
	"unsafe"

	"github.com/jeffwang/qdesk/internal/winserver"
	"github.com/jeffwang/qdesk/internal/winserver/keymap"
)

var (
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procSendInput    = user32.NewProc("SendInput")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseEventLeftDown   = 0x0002
	mouseEventLeftUp     = 0x0004
	mouseEventRightDown  = 0x0008
	mouseEventRightUp    = 0x0010
	mouseEventMiddleDown = 0x0020
	mouseEventMiddleUp   = 0x0040
	mouseEventWheel      = 0x0800
	mouseEventHWheel     = 0x01000

	keyEventKeyUp        = 0x0002
	keyEventUnicode      = 0x0004
)

// INPUT struct for SendInput. Total size on x64 is 40 bytes.
type input struct {
	Type uint32
	_    uint32 // pad to 8-byte align mi
	Data [32]byte
}

type mouseInput struct {
	DX, DY      int32
	MouseData   uint32
	Flags       uint32
	Time        uint32
	ExtraInfo   uintptr
}

type keybdInput struct {
	WVk      uint16
	WScan    uint16
	Flags    uint32
	Time     uint32
	ExtraInfo uintptr
}

func click(req winserver.ClickReq) error {
	procSetCursorPos.Call(uintptr(req.X), uintptr(req.Y))

	mods, mainDown, mainUp, err := mouseFlags(req.Button)
	if err != nil {
		return err
	}
	_ = mods // reserved if we add modifier keys
	count := 1
	if req.Double {
		count = 2
	}

	// Modifier keys down
	for _, m := range req.Modifiers {
		if vk, ok := modVK(m); ok {
			sendKey(vk, false)
		}
	}
	for i := 0; i < count; i++ {
		sendMouse(mainDown)
		sendMouse(mainUp)
	}
	for i := len(req.Modifiers) - 1; i >= 0; i-- {
		if vk, ok := modVK(req.Modifiers[i]); ok {
			sendKey(vk, true)
		}
	}
	return nil
}

func modVK(m string) (uint16, bool) {
	switch m {
	case "ctrl":
		return keymap.VKControl, true
	case "shift":
		return keymap.VKShift, true
	case "alt":
		return keymap.VKMenu, true
	case "win":
		return keymap.VKLWin, true
	}
	return 0, false
}

func mouseFlags(button string) (uint32, uint32, uint32, error) {
	switch button {
	case "", "left":
		return 0, mouseEventLeftDown, mouseEventLeftUp, nil
	case "right":
		return 0, mouseEventRightDown, mouseEventRightUp, nil
	case "middle":
		return 0, mouseEventMiddleDown, mouseEventMiddleUp, nil
	}
	return 0, 0, 0, fmt.Errorf("unknown button %q", button)
}

func sendMouse(flags uint32) {
	in := input{Type: inputMouse}
	mi := mouseInput{Flags: flags}
	*(*mouseInput)(unsafe.Pointer(&in.Data)) = mi
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

// sendKey sends one keyboard transition. up=true means release.
func sendKey(vk uint16, up bool) {
	in := input{Type: inputKeyboard}
	ki := keybdInput{WVk: vk}
	if up {
		ki.Flags |= keyEventKeyUp
	}
	*(*keybdInput)(unsafe.Pointer(&in.Data)) = ki
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

// sendUnicodeChar uses KEYEVENTF_UNICODE to inject a single UTF-16
// code unit (surrogates handled by the caller).
func sendUnicodeChar(ch uint16) {
	for _, up := range []bool{false, true} {
		in := input{Type: inputKeyboard}
		ki := keybdInput{WScan: ch, Flags: keyEventUnicode}
		if up {
			ki.Flags |= keyEventKeyUp
		}
		*(*keybdInput)(unsafe.Pointer(&in.Data)) = ki
		procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
}

func typeText(text string) error {
	utf16 := []uint16{}
	for _, r := range text {
		if r < 0x10000 {
			utf16 = append(utf16, uint16(r))
		} else {
			r -= 0x10000
			utf16 = append(utf16, 0xD800+uint16(r>>10), 0xDC00+uint16(r&0x3FF))
		}
	}
	for _, u := range utf16 {
		sendUnicodeChar(u)
	}
	return nil
}

func keyCombo(combo string) error {
	events, err := keymap.Parse(combo)
	if err != nil {
		return err
	}
	for _, e := range events {
		sendKey(e.VK, !e.Down)
	}
	return nil
}

func scroll(req winserver.ScrollReq) error {
	procSetCursorPos.Call(uintptr(req.X), uintptr(req.Y))
	if req.DY != 0 {
		// 120 wheel notches per delta unit.
		in := input{Type: inputMouse}
		mi := mouseInput{Flags: mouseEventWheel, MouseData: uint32(int32(req.DY * 120))}
		*(*mouseInput)(unsafe.Pointer(&in.Data)) = mi
		procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
	if req.DX != 0 {
		in := input{Type: inputMouse}
		mi := mouseInput{Flags: mouseEventHWheel, MouseData: uint32(int32(req.DX * 120))}
		*(*mouseInput)(unsafe.Pointer(&in.Data)) = mi
		procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
	return nil
}
```

- [ ] **Step 5: Create `internal/winnative/clipboard.go`**

```go
//go:build windows

package winnative

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/jeffwang/qdesk/internal/winserver"
	"github.com/jeffwang/qdesk/internal/winserver/keymap"
	"golang.org/x/sys/windows"
)

var (
	procOpenClipboard           = user32.NewProc("OpenClipboard")
	procCloseClipboard          = user32.NewProc("CloseClipboard")
	procEmptyClipboard          = user32.NewProc("EmptyClipboard")
	procGetClipboardData        = user32.NewProc("GetClipboardData")
	procSetClipboardData        = user32.NewProc("SetClipboardData")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procGlobalAlloc             = kernel32.NewProc("GlobalAlloc")
	procGlobalLock              = kernel32.NewProc("GlobalLock")
	procGlobalUnlock            = kernel32.NewProc("GlobalUnlock")
	procGlobalFree              = kernel32.NewProc("GlobalFree")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// Serialize all clipboard work; the Win32 clipboard is a process-global
// resource and concurrent OpenClipboard calls fight each other.
var clipboardMu sync.Mutex

func clipboardPaste(text string) (winserver.ClipboardResp, error) {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	backup, backupOK := readClipboardUnicode()
	if err := writeClipboardUnicode(text); err != nil {
		return winserver.ClipboardResp{Restored: false}, err
	}

	// Send Ctrl+V via existing keyCombo path.
	if err := keyCombo("ctrl+v"); err != nil {
		return winserver.ClipboardResp{Restored: false}, err
	}

	// Wait briefly for the paste to land before clobbering the clipboard
	// again. 150ms is the same pause Mac mode uses.
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
	// Read NUL-terminated UTF-16.
	const maxRead = 1 << 20 // 1 MiB hard cap
	buf := make([]uint16, 0, 256)
	for i := 0; i < maxRead; i++ {
		ch := *(*uint16)(unsafe.Pointer(p + uintptr(i*2)))
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
	for i, u := range utf16 {
		*(*uint16)(unsafe.Pointer(p + uintptr(i*2))) = u
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
	_ = keymap.VKReturn // silence unused import
	return nil
}
```

- [ ] **Step 6: Create `internal/winnative/native.go`**

```go
//go:build windows

package winnative

import (
	"context"

	"github.com/jeffwang/qdesk/internal/winserver"
)

// Native is the real winserver.Native implementation. Returned by New.
type Native struct{}

func New() *Native { return &Native{} }

func (Native) FrontApp(_ context.Context) (winserver.FrontApp, error) {
	return frontApp()
}
func (Native) Activate(_ context.Context, r winserver.ActivateReq) (winserver.ActivateResp, error) {
	return activate(r)
}
func (Native) Screenshot(_ context.Context) (winserver.Screenshot, error) {
	return screenshot()
}
func (Native) Click(_ context.Context, r winserver.ClickReq) error {
	return click(r)
}
func (Native) Type(_ context.Context, t string) error {
	return typeText(t)
}
func (Native) Key(_ context.Context, c string) error {
	return keyCombo(c)
}
func (Native) Scroll(_ context.Context, r winserver.ScrollReq) error {
	return scroll(r)
}
func (Native) ClipboardPaste(_ context.Context, t string) (winserver.ClipboardResp, error) {
	return clipboardPaste(t)
}
```

- [ ] **Step 7: Cross-compile to verify the build**

Run: `GOOS=windows GOARCH=amd64 go build ./cmd/qdesk-win/...`
Expected: clean — produces no output (or a binary in the working dir).

If it fails, fix imports and unused variables. The real correctness check is the E2E task.

- [ ] **Step 8: Commit**

```bash
git add internal/winnative/
git commit -m "feat(win): winnative — Win32 syscall implementation of Native"
```

---

## Task 12: cmd/qdesk-win main.go

**Files:**
- Create: `cmd/qdesk-win/main.go` (`//go:build windows`)

- [ ] **Step 1: Create main.go**

```go
//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jeffwang/qdesk/internal/winnative"
	"github.com/jeffwang/qdesk/internal/winserver"
)

func main() {
	listen := flag.String("listen",
		envOr("QDESK_WIN_LISTEN", "127.0.0.1:8765"),
		"HTTP listen address (e.g. 0.0.0.0:8765)")
	apiKey := flag.String("api-key",
		os.Getenv("QDESK_WIN_API_KEY"),
		"shared bearer token (env QDESK_WIN_API_KEY). Required.")
	trustedCIDR := flag.String("trusted-cidr",
		os.Getenv("QDESK_WIN_TRUSTED_CIDR"),
		"comma-separated CIDR allowlist (e.g. 100.64.0.0/10 for Tailscale)")
	trustTSHeaders := flag.Bool("trust-tailscale-headers", false,
		"trust Tailscale-User-Login / Tailscale-User-Name headers (set ONLY behind `tailscale serve`)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv := winserver.NewMCPServer(winnative.New())
	cfg := httpConfig{
		Listen:               *listen,
		APIKey:               *apiKey,
		TrustedCIDR:          *trustedCIDR,
		TrustTailscaleHeader: *trustTSHeaders,
	}
	logf("qdesk-win starting; listen=%s api_key=%v trusted_cidr=%q ts_headers=%v",
		cfg.Listen, cfg.APIKey != "", cfg.TrustedCIDR, cfg.TrustTailscaleHeader)
	if err := runHTTP(ctx, srv, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "qdesk-win: %v\n", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func logf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "qdesk-win: "+fmt.Sprintf(format, args...))
}
```

- [ ] **Step 2: Cross-compile from macOS**

Run: `GOOS=windows GOARCH=amd64 go build -o /tmp/qdesk-win.exe ./cmd/qdesk-win`
Expected: produces `/tmp/qdesk-win.exe`. Confirm with `file /tmp/qdesk-win.exe` showing `PE32+ executable (console) x86-64`.

- [ ] **Step 3: Run cross-platform build sanity check**

Run: `go build ./...`
Expected: all packages compile on macOS (winnative is windows-only and skipped; cmd/qdesk-win uses the stub from Task 9).

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/qdesk-win/main.go
git commit -m "feat(win): cmd/qdesk-win main — flags + HTTP boot"
```

---

## Task 13: Makefile target + smoke deploy script

**Files:**
- Modify: `Makefile`
- Create: `scripts/install-win.sh`

- [ ] **Step 1: Inspect existing Makefile to see existing target pattern**

Run: `grep -n -E '^[a-z-]+:' Makefile | head -20`

Pick a similar target (e.g. `mac-build`) and mirror its style.

- [ ] **Step 2: Add `win-build` and `win-deploy` targets**

Append to `Makefile`:

```make
QDESK_WIN_HOST ?= Administrator@192.168.0.127

.PHONY: win-build
win-build:
	GOOS=windows GOARCH=amd64 go build -o bin/qdesk-win.exe ./cmd/qdesk-win

.PHONY: win-deploy
win-deploy: win-build
	scp bin/qdesk-win.exe $(QDESK_WIN_HOST):qdesk-win.exe
	@echo "Deployed to $(QDESK_WIN_HOST). Start with:"
	@echo "  ssh $(QDESK_WIN_HOST) 'powershell -NoExit -Command ./qdesk-win.exe --listen 0.0.0.0:8765 --api-key YOURKEY'"
```

- [ ] **Step 3: Create `scripts/install-win.sh`**

```sh
#!/usr/bin/env bash
# Build qdesk-win.exe and scp it to a Windows host. Caller must have
# OpenSSH login set up. Usage:
#   QDESK_WIN_HOST=Administrator@192.168.0.127 ./scripts/install-win.sh
set -euo pipefail
HOST="${QDESK_WIN_HOST:-Administrator@192.168.0.127}"
GOOS=windows GOARCH=amd64 go build -o bin/qdesk-win.exe ./cmd/qdesk-win
scp bin/qdesk-win.exe "$HOST:qdesk-win.exe"
echo "Deployed. Generate an API key with: openssl rand -hex 32"
echo "Run on the host:"
echo "  qdesk-win.exe --listen 0.0.0.0:8765 --api-key <KEY>"
echo "  (allow inbound 8765 in Windows Defender Firewall first)"
```

- [ ] **Step 4: Make script executable**

```bash
chmod +x scripts/install-win.sh
```

- [ ] **Step 5: Commit**

```bash
git add Makefile scripts/install-win.sh
git commit -m "build(win): Makefile target + install script for windows host"
```

---

## Task 14: E2E smoke test on the VM

**Files:** none modified. This task validates real Win32 behavior on `Administrator@192.168.0.127`.

- [ ] **Step 1: Build & deploy**

```bash
make win-build
scp bin/qdesk-win.exe Administrator@192.168.0.127:qdesk-win.exe
```

Expected: scp completes without error.

- [ ] **Step 2: Open Windows Firewall port 8765**

```bash
ssh Administrator@192.168.0.127 'powershell -Command "New-NetFirewallRule -DisplayName qdesk-win -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8765"'
```

Expected: outputs `Name DisplayName ...` confirming the rule.

- [ ] **Step 3: Generate an API key locally**

```bash
KEY=$(openssl rand -hex 32)
echo "$KEY"
```

Save the value for the next steps.

- [ ] **Step 4: Start the server on the VM (background)**

```bash
ssh Administrator@192.168.0.127 "powershell -Command \"Start-Process -FilePath \$env:USERPROFILE\\qdesk-win.exe -ArgumentList '--listen','0.0.0.0:8765','--api-key','$KEY' -WindowStyle Hidden\""
```

Expected: returns immediately. Verify the process is up:

```bash
ssh Administrator@192.168.0.127 'powershell -Command "Get-Process qdesk-win -ErrorAction SilentlyContinue"'
```

Expected: a `qdesk-win` process row.

- [ ] **Step 5: Health check from the Mac**

```bash
curl -s http://192.168.0.127:8765/health
```

Expected: `{"ok":true}`.

- [ ] **Step 6: tools/list**

```bash
curl -s -X POST http://192.168.0.127:8765/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq '.result.tools[].name'
```

Expected: 8 names listed including `windows.front_app`, `windows.activate`, etc.

- [ ] **Step 7: front_app**

```bash
curl -s -X POST http://192.168.0.127:8765/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"windows.front_app","arguments":{}}}' | jq '.result.content[0].text'
```

Expected: a string like `"frontApp.exe=explorer.exe pid=... title=... hwnd=0x..."`.

- [ ] **Step 8: Open Notepad on the VM**

```bash
ssh Administrator@192.168.0.127 'powershell -Command "Start-Process notepad"'
```

- [ ] **Step 9: activate notepad**

```bash
curl -s -X POST http://192.168.0.127:8765/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"windows.activate","arguments":{"exe":"notepad.exe"}}}' | jq
```

Expected: `actually_foreground=true` in the result text.

- [ ] **Step 10: Type ASCII**

```bash
curl -s -X POST http://192.168.0.127:8765/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"windows.type","arguments":{"text":"hello qdesk\n","expected_exe":"notepad.exe"}}}' | jq
```

Expected: text says `path=unicode`.

- [ ] **Step 11: Type non-ASCII (Chinese)**

```bash
curl -s -X POST http://192.168.0.127:8765/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"windows.type","arguments":{"text":"你好 qdesk\n","expected_exe":"notepad.exe"}}}' | jq
```

Expected: text says `path=clipboard clipboard_restored=true`.

- [ ] **Step 12: screenshot — verify dimensions and that PNG decodes**

```bash
curl -s -X POST http://192.168.0.127:8765/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"windows.screenshot","arguments":{}}}' \
  | jq -r '.result.content[0].data' | base64 -d > /tmp/win-shot.png
file /tmp/win-shot.png
```

Expected: `PNG image data, NNN x NNN, ...`. Open it (`open /tmp/win-shot.png` on macOS) and visually verify Notepad shows both the ASCII and Chinese strings.

- [ ] **Step 13: ctrl+a then delete to clear**

```bash
curl -s -X POST http://192.168.0.127:8765/mcp \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"windows.key","arguments":{"combo":"ctrl+a","expected_exe":"notepad.exe"}}}'
curl -s -X POST http://192.168.0.127:8765/mcp \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"windows.key","arguments":{"combo":"delete","expected_exe":"notepad.exe"}}}'
```

Take another screenshot (step 12 again) — Notepad should be empty.

- [ ] **Step 14: Stop the server**

```bash
ssh Administrator@192.168.0.127 'powershell -Command "Stop-Process -Name qdesk-win -Force"'
```

- [ ] **Step 15: Document any deviations and commit notes**

If any step diverged (e.g. activate `actually_foreground=false`, or unicode path failed for some Win32 control), capture findings in a new memory file `feedback_windows_e2e_findings.md` under `/Users/jeff/.claude/projects/-Users-jeff-work-qdesk/memory/` and add a pointer to `MEMORY.md`. Mirror the structure of `feedback_wechat_e2e_findings.md`.

If no deviations, commit a brief "passed E2E" note in the spec or skip — git tag `qdesk-win-v0.1` instead:

```bash
git tag qdesk-win-v0.1
```

---

## Task 15: README + merge

**Files:**
- Modify: `README.md` — add a "Windows host mode (alpha)" section parallel to the Mac one
- Optionally: rename branch and merge

- [ ] **Step 1: Read current Mac section in README to mirror it**

Run: `grep -n 'Mac host mode' README.md`

- [ ] **Step 2: Add Windows section**

Below the Mac host mode section, add:

```markdown
## Windows host mode (alpha) — drive a Windows machine over HTTP

A single Go binary, `qdesk-win.exe`, exposes the same shape as `qdesk-mac --listen` for Windows. No sidecar — Win32 syscalls happen directly in Go.

```bash
make win-build
scp bin/qdesk-win.exe Administrator@your-windows-host:qdesk-win.exe
ssh Administrator@your-windows-host 'powershell -Command "Start-Process qdesk-win.exe -ArgumentList ''--listen'',''0.0.0.0:8765'',''--api-key'',''YOURKEY'' -WindowStyle Hidden"'
```

Tools live under `windows.*`: `front_app`, `activate`, `screenshot`, `click`, `type`, `key`, `scroll`, `clipboard_paste`. Each action accepts an optional `expected_exe` guard. `type` auto-routes non-ASCII through clipboard.

**v1 limitations:** primary monitor only; no UIA / accessibility tree; no service install (manual or `nssm` recommended); `SetForegroundWindow` may refuse focus changes (caller checks `actually_foreground` in the response). See `docs/superpowers/specs/2026-05-07-windows-host-mode-design.md`.
```

- [ ] **Step 3: Commit and PR**

```bash
git add README.md
git commit -m "docs(win): README section for Windows host mode (alpha)"
```

If merging via PR, push the branch:

```bash
git push -u origin feat/qdesk-win-host-mode-design
```

Otherwise merge to main locally:

```bash
git checkout main
git merge --no-ff feat/qdesk-win-host-mode-design -m "Merge feat/qdesk-win-host-mode-design — Windows host mode v1"
```

---

## Self-Review Notes

After this plan was drafted I reviewed against the spec:

- **Tool surface** — all 8 `windows.*` tools have a task (Tasks 4, 5, 6, 7).
- **Guard behavior** — Task 3 covers per-call `expected_exe`; Task 5 explicitly does NOT guard `activate`.
- **ASCII/non-ASCII routing** — Task 7 has both branches as separate test cases.
- **Clipboard backup/restore failure surfaces in result** — covered by `TestTypeReportsClipboardRestoreFailure` and the `ClipboardResp.Restored` plumbing in Tasks 1, 7, 11.
- **DPI awareness** — Task 11 step 1 sets `PER_MONITOR_AWARE_V2` in init.
- **`actually_foreground`** — Task 5 and Task 11's `activate` both expose it.
- **HTTP listen + Bearer + CIDR + Tailscale headers** — Task 10 mirrors mac http.go.
- **Build cleanliness on macOS dev** — Task 9 stub keeps `go build ./...` green.
- **E2E** — Task 14 covers the smoke sequence from the spec §8.2.

No `TBD`, no "implement later", no "similar to". Each step has runnable commands with expected output.
