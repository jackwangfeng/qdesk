# Mac host mode (WeChat) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `qdesk-mac` MCP server (Go) + `qdesk-mac-helper` Swift sidecar that lets an AI agent screenshot, click, type, and read the chat list of macOS WeChat through MCP tools, with a foreground-app guard.

**Architecture:** Two-binary stdio chain. MCP client ↔ `qdesk-mac` (Go, MCP server, hand-rolled JSON-RPC like the existing `qdesk-mcp`) ↔ `qdesk-mac-helper` (Swift, native APIs only). Helper is a child process spawned by `qdesk-mac`; communication is line-delimited JSON over stdio. Tool surface is namespaced under `wechat.*`. Foreground guard rejects action calls unless front app bundle ID == `com.tencent.xinWeChat`.

**Tech Stack:** Go 1.25 (existing module), Swift 5.9+ via Swift Package Manager (no Xcode project), ScreenCaptureKit, CGEvent, AXUIElement, NSWorkspace. macOS 13+ only (ScreenCaptureKit requirement).

**Spec:** `docs/superpowers/specs/2026-05-05-mac-host-mode-wechat-design.md`

## v1 deferrals from spec

Two items in the spec are intentionally not in this plan; they're cheap follow-ups once v1 is validated:

1. **Helper auto-restart** (spec §4.2): if the Swift helper crashes mid-session, the next tool call surfaces the read error as `IsError`; the user reconnects the MCP client. No silent restart in v1.
2. **`open_chat` cmd+f search fallback** (spec §4.5): if a chat name doesn't match any sidebar row, v1 returns `chat-not-found`. The LLM can drive `wechat.key("cmd+f")` + `wechat.type` itself if it wants search; the example doc points this out.

---

## File Structure

New files:

```
cmd/qdesk-mac/
  main.go                       # Go: MCP server entry point + CLI (run + doctor)
  doctor.go                     # `qdesk-mac doctor` subcommand
  doctor_test.go

cmd/qdesk-mac-helper/
  Package.swift                 # Swift Package manifest
  Sources/Helper/
    main.swift                  # JSON-RPC stdio loop, dispatch
    Protocol.swift              # Codable types for RPC envelopes
    Health.swift                # CGPreflight + AXIsProcessTrusted
    Foreground.swift            # NSWorkspace (frontApp, activate)
    Capture.swift               # ScreenCaptureKit (screenshot)
    Input.swift                 # CGEvent (click, type, key, scroll)
    Accessibility.swift         # AXUIElement (axTree, axClick)
  Tests/HelperTests/
    HealthTests.swift
    ForegroundTests.swift
    CaptureTests.swift
    InputTests.swift
    AccessibilityTests.swift

internal/macproto/
  rpc.go                        # Shared Go types: HelperRequest, HelperResponse, all methods
  rpc_test.go                   # Round-trip JSON marshalling tests

internal/macserver/
  supervisor.go                 # Spawn helper, JSON-RPC client over stdio, lifecycle
  supervisor_test.go            # Spawn real helper binary, call health, parse response
  fakehelper.go                 # Test helper: in-memory fake helper
  tools.go                      # MCP tool implementations (screenshot, click, etc.)
  tools_test.go                 # Tool dispatch tests using fakehelper
  guard.go                      # Foreground-app guard middleware
  guard_test.go
  chats.go                      # list_chats / open_chat (axTree → structured list)
  chats_test.go

examples/
  wechat-reply.md               # Example: Claude Code drives qdesk-mac to reply

.github/workflows/
  mac.yml                       # macOS CI (build + protocol-level tests)

scripts/
  install-mac.sh                # Installs both binaries to /usr/local/bin/
```

Modified files:

```
go.mod / go.sum                 # No new deps expected; verify
README.md                       # Add Mac host mode section
Makefile                        # Add `mac` target (build Go + Swift)
```

---

## Task 1: Project skeleton — Go cmd dir + Swift Package + Makefile target

**Files:**
- Create: `cmd/qdesk-mac/main.go`
- Create: `cmd/qdesk-mac-helper/Package.swift`
- Create: `cmd/qdesk-mac-helper/Sources/Helper/main.swift`
- Modify: `Makefile` (add `mac-build` target)

- [ ] **Step 1: Create minimal `cmd/qdesk-mac/main.go` that exits cleanly**

```go
// qdesk-mac is a Model Context Protocol (MCP) stdio server that lets an AI
// agent control the host macOS WeChat through generic input primitives plus
// a small set of Accessibility-API helpers.
//
// Wire protocol: JSON-RPC 2.0 over stdin/stdout (one JSON object per line).
// Spawns qdesk-mac-helper (Swift) as a child process for native macOS APIs.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		fmt.Fprintln(os.Stderr, "doctor: not yet implemented")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "qdesk-mac: not yet implemented")
	os.Exit(1)
}
```

- [ ] **Step 2: Create `cmd/qdesk-mac-helper/Package.swift`**

```swift
// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "qdesk-mac-helper",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(
            name: "Helper",
            path: "Sources/Helper"
        ),
        .testTarget(
            name: "HelperTests",
            dependencies: ["Helper"],
            path: "Tests/HelperTests"
        ),
    ]
)
```

- [ ] **Step 3: Create stub `cmd/qdesk-mac-helper/Sources/Helper/main.swift`**

```swift
import Foundation

FileHandle.standardError.write(Data("qdesk-mac-helper: not yet implemented\n".utf8))
exit(1)
```

- [ ] **Step 4: Add `mac-build` Makefile target**

Append to `Makefile`:

```makefile
.PHONY: mac-build
mac-build:
	go build -o bin/qdesk-mac ./cmd/qdesk-mac
	cd cmd/qdesk-mac-helper && swift build -c release
	cp cmd/qdesk-mac-helper/.build/release/Helper bin/qdesk-mac-helper
```

- [ ] **Step 5: Verify both binaries build**

Run: `make mac-build`
Expected: produces `bin/qdesk-mac` and `bin/qdesk-mac-helper`. Running each prints "not yet implemented" to stderr and exits 1.

- [ ] **Step 6: Commit**

```bash
git add cmd/qdesk-mac cmd/qdesk-mac-helper Makefile
git commit -m "feat(mac): cmd skeleton (Go + Swift Package) + mac-build target"
```

---

## Task 2: Internal RPC types — `internal/macproto`

**Files:**
- Create: `internal/macproto/rpc.go`
- Test: `internal/macproto/rpc_test.go`

- [ ] **Step 1: Write the failing test for round-trip JSON marshalling**

`internal/macproto/rpc_test.go`:

```go
package macproto

import (
	"encoding/json"
	"testing"
)

func TestHealthResponseRoundTrip(t *testing.T) {
	in := HealthResponse{
		OK:                       true,
		ScreenRecordingGranted:   true,
		AccessibilityGranted:     false,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out HealthResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", out, in)
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	req := Request{ID: 7, Method: MethodHealth, Params: json.RawMessage(`{}`)}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Request
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != req.ID || out.Method != req.Method {
		t.Errorf("envelope mismatch: got=%+v want=%+v", out, req)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/macproto/... -v`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement `internal/macproto/rpc.go`**

```go
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
	Path        string `json:"path"`        // opaque ID for axClick
	Role        string `json:"role"`
	Title       string `json:"title,omitempty"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
	Frame       Frame  `json:"frame"`
}

// Frame is in LOGICAL screen points.
type Frame struct {
	X, Y, W, H float64
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
const (
	CodeWeChatNotRunning      = "wechat-not-running"
	CodeWeChatNotForeground   = "wechat-not-foreground"
	CodePermScreenRecording   = "permission-screen-recording"
	CodePermAccessibility     = "permission-accessibility"
	CodeHelperCrashed         = "helper-crashed"
	CodeChatNotFound          = "chat-not-found"
	CodeAXTreeEmpty           = "ax-tree-empty"
	CodeInternal              = "internal"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/macproto/... -v`
Expected: PASS for both `TestHealthResponseRoundTrip` and `TestEnvelopeRoundTrip`.

- [ ] **Step 5: Commit**

```bash
git add internal/macproto
git commit -m "feat(mac): macproto package — RPC types between Go and Swift helper"
```

---

## Task 3: Swift helper — JSON-RPC stdio loop with health stub

**Files:**
- Modify: `cmd/qdesk-mac-helper/Sources/Helper/main.swift`
- Create: `cmd/qdesk-mac-helper/Sources/Helper/Protocol.swift`
- Create: `cmd/qdesk-mac-helper/Sources/Helper/Health.swift`
- Create: `cmd/qdesk-mac-helper/Tests/HelperTests/HealthTests.swift`

- [ ] **Step 1: Write the failing health test (Swift XCTest)**

`Tests/HelperTests/HealthTests.swift`:

```swift
import XCTest
@testable import Helper

final class HealthTests: XCTestCase {
    func testHealthReturnsBooleansForKnownPermissions() {
        // health() is allowed to return any booleans depending on the host's
        // TCC state — but it MUST NOT throw and MUST return a HealthResponse.
        let r = health()
        XCTAssertTrue(r.ok || !r.ok) // tautology; assertion is "did not throw"
        _ = r.screenRecordingGranted
        _ = r.accessibilityGranted
    }
}
```

- [ ] **Step 2: Run test to verify it fails (target doesn't exist)**

Run: `cd cmd/qdesk-mac-helper && swift test`
Expected: FAIL — `Helper` module has no `health()` function.

- [ ] **Step 3: Implement `Sources/Helper/Protocol.swift`**

```swift
import Foundation

struct RPCRequest: Decodable {
    let id: Int
    let method: String
    let params: Data?

    enum CodingKeys: String, CodingKey { case id, method, params }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(Int.self, forKey: .id)
        method = try c.decode(String.self, forKey: .method)
        if c.contains(.params) {
            // Re-encode the raw value so handlers can decode their own params type.
            let raw = try c.decode(JSONValue.self, forKey: .params)
            params = try JSONEncoder().encode(raw)
        } else {
            params = nil
        }
    }
}

struct RPCResponse: Encodable {
    let id: Int
    let result: JSONValue?
    let error: RPCError?
}

struct RPCError: Encodable {
    let code: String
    let message: String
}

// Minimal JSON value used to round-trip arbitrary param payloads.
enum JSONValue: Codable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null; return }
        if let v = try? c.decode(Bool.self) { self = .bool(v); return }
        if let v = try? c.decode(Double.self) { self = .number(v); return }
        if let v = try? c.decode(String.self) { self = .string(v); return }
        if let v = try? c.decode([JSONValue].self) { self = .array(v); return }
        if let v = try? c.decode([String: JSONValue].self) { self = .object(v); return }
        throw DecodingError.dataCorruptedError(in: c, debugDescription: "unknown JSON value")
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case .bool(let v): try c.encode(v)
        case .number(let v): try c.encode(v)
        case .string(let v): try c.encode(v)
        case .array(let v): try c.encode(v)
        case .object(let v): try c.encode(v)
        }
    }
}
```

- [ ] **Step 4: Implement `Sources/Helper/Health.swift`**

```swift
import Foundation
import ApplicationServices
import CoreGraphics

struct HealthResponse: Encodable {
    let ok: Bool
    let screenRecordingGranted: Bool
    let accessibilityGranted: Bool
}

func health() -> HealthResponse {
    let sr = CGPreflightScreenCaptureAccess()
    let ax = AXIsProcessTrusted()
    return HealthResponse(
        ok: sr && ax,
        screenRecordingGranted: sr,
        accessibilityGranted: ax
    )
}
```

- [ ] **Step 5: Replace `Sources/Helper/main.swift` with the JSON-RPC loop**

```swift
import Foundation

func writeResponse(_ resp: RPCResponse) {
    let enc = JSONEncoder()
    enc.outputFormatting = [.withoutEscapingSlashes]
    guard let data = try? enc.encode(resp) else { return }
    FileHandle.standardOutput.write(data)
    FileHandle.standardOutput.write(Data([0x0A])) // newline
}

func writeError(id: Int, code: String, message: String) {
    writeResponse(RPCResponse(id: id, result: nil, error: RPCError(code: code, message: message)))
}

func writeOK(id: Int) {
    writeResponse(RPCResponse(id: id, result: .object(["ok": .bool(true)]), error: nil))
}

func writeResult<T: Encodable>(id: Int, value: T) {
    let enc = JSONEncoder()
    guard let data = try? enc.encode(value),
          let obj = try? JSONDecoder().decode(JSONValue.self, from: data) else {
        writeError(id: id, code: "internal", message: "encode result")
        return
    }
    writeResponse(RPCResponse(id: id, result: obj, error: nil))
}

func dispatch(_ req: RPCRequest) {
    switch req.method {
    case "health":
        writeResult(id: req.id, value: health())
    default:
        writeError(id: req.id, code: "internal", message: "method not implemented: \(req.method)")
    }
}

// stdin loop: one JSON object per line.
let stdin = FileHandle.standardInput
let dec = JSONDecoder()
var buffer = Data()
while true {
    let chunk = stdin.availableData
    if chunk.isEmpty { break }
    buffer.append(chunk)
    while let nl = buffer.firstIndex(of: 0x0A) {
        let line = buffer.subdata(in: 0..<nl)
        buffer.removeSubrange(0...nl)
        if line.isEmpty { continue }
        do {
            let req = try dec.decode(RPCRequest.self, from: line)
            dispatch(req)
        } catch {
            FileHandle.standardError.write(Data("decode error: \(error)\n".utf8))
        }
    }
}
```

- [ ] **Step 6: Run Swift tests**

Run: `cd cmd/qdesk-mac-helper && swift test`
Expected: PASS for `HealthTests.testHealthReturnsBooleansForKnownPermissions`.

- [ ] **Step 7: Manual smoke test**

Run:
```bash
make mac-build
echo '{"id":1,"method":"health","params":{}}' | ./bin/qdesk-mac-helper
```

Expected: a single line of JSON containing `"id":1` and a `result` with `ok`, `screenRecordingGranted`, `accessibilityGranted` booleans.

- [ ] **Step 8: Commit**

```bash
git add cmd/qdesk-mac-helper
git commit -m "feat(mac-helper): JSON-RPC stdio loop + health RPC"
```

---

## Task 4: Go helper supervisor + RPC client

**Files:**
- Create: `internal/macserver/supervisor.go`
- Create: `internal/macserver/supervisor_test.go`

- [ ] **Step 1: Write failing test that spawns the real helper and calls health**

`internal/macserver/supervisor_test.go`:

```go
package macserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// helperBinaryPath returns the path to the built helper, skipping the test if
// the build hasn't been run yet.
func helperBinaryPath(t *testing.T) string {
	t.Helper()
	repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not in a git repo: %v", err)
	}
	p := filepath.Join(string(repoRoot[:len(repoRoot)-1]), "bin", "qdesk-mac-helper")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("helper binary not built (run `make mac-build`): %v", err)
	}
	return p
}

func TestSupervisorHealthRoundTrip(t *testing.T) {
	bin := helperBinaryPath(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := Spawn(ctx, bin)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer s.Close()

	raw, err := s.Call(ctx, macproto.MethodHealth, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var h macproto.HealthResponse
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// h.OK depends on host TCC state; just verify the shape decoded.
	t.Logf("health: %+v", h)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/macserver/... -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement `internal/macserver/supervisor.go`**

```go
// Package macserver implements the Go side of qdesk-mac: spawning the Swift
// helper, marshalling JSON-RPC requests over stdio, and exposing the MCP
// tool surface to callers.
package macserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// Supervisor owns the helper child process and the JSON-RPC framing.
// All public methods are safe for concurrent use; Call serialises requests
// internally because the helper is single-threaded over stdio.
type Supervisor struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	mu       sync.Mutex // serialises Call
	nextID   atomic.Int64
	closed   atomic.Bool
	closeErr error
}

// Spawn starts the helper binary and returns a Supervisor wrapping it.
func Spawn(ctx context.Context, binary string) (*Supervisor, error) {
	cmd := exec.CommandContext(ctx, binary)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start helper: %w", err)
	}
	return &Supervisor{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		stderr: stderr,
	}, nil
}

// Call sends one request and waits for the response. Helper is single-threaded
// so this MUST be serialised.
func (s *Supervisor) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if s.closed.Load() {
		return nil, errors.New("supervisor closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id := int(s.nextID.Add(1))
	req := macproto.Request{ID: id, Method: method, Params: params}
	frame, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	frame = append(frame, '\n')

	// Default 30s deadline if caller didn't supply one.
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(30 * time.Second)
	}

	if _, err := s.stdin.Write(frame); err != nil {
		return nil, fmt.Errorf("write frame: %w", err)
	}

	// Read one response line.
	type readResult struct {
		line []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := s.stdout.ReadBytes('\n')
		ch <- readResult{line, err}
	}()
	var line []byte
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Until(deadline)):
		return nil, errors.New("helper response timeout")
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("read response: %w", r.err)
		}
		line = r.line
	}

	var resp macproto.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (line=%q)", err, string(line))
	}
	if resp.ID != id {
		return nil, fmt.Errorf("id mismatch: got=%d want=%d", resp.ID, id)
	}
	if resp.Error != nil {
		return nil, &HelperError{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	return resp.Result, nil
}

// Close terminates the helper. SIGTERM, then SIGKILL after 5 seconds.
func (s *Supervisor) Close() error {
	if s.closed.Swap(true) {
		return s.closeErr
	}
	_ = s.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
		s.closeErr = <-done
	case err := <-done:
		s.closeErr = err
	}
	return s.closeErr
}

// HelperError is returned when the helper sends a structured error response.
type HelperError struct {
	Code    string
	Message string
}

func (e *HelperError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make mac-build && go test ./internal/macserver/... -v -run TestSupervisorHealthRoundTrip`
Expected: PASS — health response decoded correctly.

- [ ] **Step 5: Commit**

```bash
git add internal/macserver/supervisor.go internal/macserver/supervisor_test.go
git commit -m "feat(mac): supervisor — spawn Swift helper + JSON-RPC client"
```

---

## Task 5: Swift helper — Foreground (NSWorkspace)

**Files:**
- Create: `cmd/qdesk-mac-helper/Sources/Helper/Foreground.swift`
- Modify: `cmd/qdesk-mac-helper/Sources/Helper/main.swift` (add dispatch cases)
- Create: `cmd/qdesk-mac-helper/Tests/HelperTests/ForegroundTests.swift`

- [ ] **Step 1: Write failing test for `frontApp()`**

`Tests/HelperTests/ForegroundTests.swift`:

```swift
import XCTest
@testable import Helper

final class ForegroundTests: XCTestCase {
    func testFrontAppReturnsNonEmptyBundleId() {
        // On any running macOS desktop something is in front (Finder at minimum).
        let r = frontApp()
        XCTAssertFalse(r.bundleId.isEmpty, "expected a non-empty front-app bundle ID")
        XCTAssertGreaterThan(r.pid, 0)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/qdesk-mac-helper && swift test --filter ForegroundTests`
Expected: FAIL — `frontApp` undefined.

- [ ] **Step 3: Implement `Sources/Helper/Foreground.swift`**

```swift
import Foundation
import AppKit

struct FrontAppResponse: Encodable {
    let bundleId: String
    let name: String
    let pid: Int
}

func frontApp() -> FrontAppResponse {
    let app = NSWorkspace.shared.frontmostApplication
    return FrontAppResponse(
        bundleId: app?.bundleIdentifier ?? "",
        name: app?.localizedName ?? "",
        pid: Int(app?.processIdentifier ?? 0)
    )
}

struct ActivateRequest: Decodable {
    let bundleId: String
}

func activate(_ req: ActivateRequest) throws {
    guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: req.bundleId).first else {
        throw HelperRPCError(code: "wechat-not-running",
                             message: "app with bundle ID \(req.bundleId) is not running")
    }
    app.activate(options: [.activateAllWindows])
}

struct HelperRPCError: Error {
    let code: String
    let message: String
}
```

- [ ] **Step 4: Wire `frontApp` and `activate` into `main.swift` dispatch**

In `dispatch(_:)`, replace the body with:

```swift
switch req.method {
case "health":
    writeResult(id: req.id, value: health())
case "frontApp":
    writeResult(id: req.id, value: frontApp())
case "activate":
    do {
        let p = try JSONDecoder().decode(ActivateRequest.self, from: req.params ?? Data("{}".utf8))
        try activate(p)
        writeOK(id: req.id)
    } catch let e as HelperRPCError {
        writeError(id: req.id, code: e.code, message: e.message)
    } catch {
        writeError(id: req.id, code: "internal", message: "\(error)")
    }
default:
    writeError(id: req.id, code: "internal", message: "method not implemented: \(req.method)")
}
```

- [ ] **Step 5: Run Swift tests**

Run: `cd cmd/qdesk-mac-helper && swift test`
Expected: PASS for `ForegroundTests` and previously-passing `HealthTests`.

- [ ] **Step 6: Manual smoke test from Go**

Add a small ad-hoc check (don't commit; just verify):

```bash
make mac-build
echo '{"id":1,"method":"frontApp","params":{}}' | ./bin/qdesk-mac-helper
```

Expected: JSON with `bundleId` (some non-empty string like `com.apple.finder` or your terminal's bundle ID).

- [ ] **Step 7: Commit**

```bash
git add cmd/qdesk-mac-helper
git commit -m "feat(mac-helper): frontApp + activate via NSWorkspace"
```

---

## Task 6: Swift helper — ScreenCaptureKit screenshot

**Files:**
- Create: `cmd/qdesk-mac-helper/Sources/Helper/Capture.swift`
- Modify: `cmd/qdesk-mac-helper/Sources/Helper/main.swift`
- Create: `cmd/qdesk-mac-helper/Tests/HelperTests/CaptureTests.swift`

- [ ] **Step 1: Write the failing capture test**

`Tests/HelperTests/CaptureTests.swift`:

```swift
import XCTest
@testable import Helper

final class CaptureTests: XCTestCase {
    func testScreenshotReturnsPNG() async throws {
        // Will throw if Screen Recording permission not granted on this host.
        // We keep the test optional: skip if perm missing.
        let h = health()
        try XCTSkipUnless(h.screenRecordingGranted, "Screen Recording not granted; skipping")

        let r = try await screenshot()
        XCTAssertGreaterThan(r.width, 0)
        XCTAssertGreaterThan(r.height, 0)
        guard let data = Data(base64Encoded: r.pngBase64) else {
            return XCTFail("invalid base64")
        }
        // PNG magic
        XCTAssertEqual(Array(data.prefix(4)), [0x89, 0x50, 0x4E, 0x47])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/qdesk-mac-helper && swift test --filter CaptureTests`
Expected: FAIL — `screenshot` undefined.

- [ ] **Step 3: Implement `Sources/Helper/Capture.swift`**

```swift
import Foundation
import ScreenCaptureKit
import CoreImage
import AppKit

struct ScreenshotResponse: Encodable {
    let pngBase64: String
    let width: Int
    let height: Int
    let scaleFactor: Double
}

func screenshot() async throws -> ScreenshotResponse {
    let content = try await SCShareableContent.excludingDesktopWindows(
        false, onScreenWindowsOnly: true)
    guard let display = content.displays.first else {
        throw HelperRPCError(code: "internal", message: "no display found")
    }
    let cfg = SCStreamConfiguration()
    let scale = NSScreen.main?.backingScaleFactor ?? 2.0
    cfg.width = Int(Double(display.width) * scale)
    cfg.height = Int(Double(display.height) * scale)
    cfg.pixelFormat = kCVPixelFormatType_32BGRA
    cfg.showsCursor = false
    cfg.scalesToFit = true

    let filter = SCContentFilter(display: display, excludingApplications: [], exceptingWindows: [])
    let cgImage = try await SCScreenshotManager.captureImage(
        contentFilter: filter, configuration: cfg)

    // Encode PNG
    let bitmap = NSBitmapImageRep(cgImage: cgImage)
    guard let png = bitmap.representation(using: .png, properties: [:]) else {
        throw HelperRPCError(code: "internal", message: "PNG encode failed")
    }
    return ScreenshotResponse(
        pngBase64: png.base64EncodedString(),
        width: display.width,
        height: display.height,
        scaleFactor: scale
    )
}
```

- [ ] **Step 4: Wire screenshot into dispatch**

In `main.swift`'s `dispatch`, add a case. Because it's `async`, wrap in a Task:

```swift
case "screenshot":
    let id = req.id
    Task {
        do {
            let r = try await screenshot()
            writeResult(id: id, value: r)
        } catch let e as HelperRPCError {
            writeError(id: id, code: e.code, message: e.message)
        } catch {
            writeError(id: id, code: "internal", message: "\(error)")
        }
    }
```

NOTE: introducing async dispatch breaks strict ordering of stdout writes if the
caller pipelines requests. We don't pipeline (Go side serialises), but
to be defensive, add a serial DispatchQueue around `writeResponse`. Add at
top of `main.swift`:

```swift
let writeQueue = DispatchQueue(label: "qdesk.helper.write")

func writeResponseSync(_ resp: RPCResponse) {
    writeQueue.sync {
        let enc = JSONEncoder()
        guard let data = try? enc.encode(resp) else { return }
        FileHandle.standardOutput.write(data)
        FileHandle.standardOutput.write(Data([0x0A]))
    }
}
```

Then change `writeResponse` calls in `writeError`/`writeOK`/`writeResult` to
use `writeResponseSync`.

Also we need the main thread to keep running while async tasks complete. The
current `while true` stdin loop blocks on `availableData`, which keeps the
process alive — that's fine. But the loop must run on a background queue or
the program should call `RunLoop.main.run()` after dispatch. Simplest: keep
the stdin reader synchronous on a background thread and run the main runloop:

Replace the bottom of `main.swift` with:

```swift
DispatchQueue.global(qos: .userInitiated).async {
    let stdin = FileHandle.standardInput
    let dec = JSONDecoder()
    var buffer = Data()
    while true {
        let chunk = stdin.availableData
        if chunk.isEmpty {
            exit(0)
        }
        buffer.append(chunk)
        while let nl = buffer.firstIndex(of: 0x0A) {
            let line = buffer.subdata(in: 0..<nl)
            buffer.removeSubrange(0...nl)
            if line.isEmpty { continue }
            do {
                let req = try dec.decode(RPCRequest.self, from: line)
                dispatch(req)
            } catch {
                FileHandle.standardError.write(Data("decode error: \(error)\n".utf8))
            }
        }
    }
}
RunLoop.main.run()
```

- [ ] **Step 5: Run Swift tests**

Run: `cd cmd/qdesk-mac-helper && swift test`
Expected: `CaptureTests.testScreenshotReturnsPNG` either passes or skips (depending on perm). All other tests still pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/qdesk-mac-helper
git commit -m "feat(mac-helper): screenshot via ScreenCaptureKit + async-safe stdio"
```

---

## Task 7: Swift helper — Input (CGEvent click/type/key/scroll)

**Files:**
- Create: `cmd/qdesk-mac-helper/Sources/Helper/Input.swift`
- Modify: `cmd/qdesk-mac-helper/Sources/Helper/main.swift`
- Create: `cmd/qdesk-mac-helper/Tests/HelperTests/InputTests.swift`

- [ ] **Step 1: Write failing test that input methods don't throw on harmless inputs**

`Tests/HelperTests/InputTests.swift`:

```swift
import XCTest
@testable import Helper

final class InputTests: XCTestCase {
    func testClickAtOffscreenIsNoOp() throws {
        let h = health()
        try XCTSkipUnless(h.accessibilityGranted, "Accessibility not granted; skipping")
        // Click far off-screen — should not crash, should not raise.
        try clickGlobal(x: -10_000, y: -10_000, button: "left", clicks: 1)
    }

    func testKeyComboParsesKnownKeys() throws {
        XCTAssertNoThrow(try resolveKeyCombo("return"))
        XCTAssertNoThrow(try resolveKeyCombo("escape"))
        XCTAssertNoThrow(try resolveKeyCombo("cmd+v"))
        XCTAssertThrowsError(try resolveKeyCombo("totally-not-a-key"))
    }
}
```

- [ ] **Step 2: Run test, confirm fails**

Run: `cd cmd/qdesk-mac-helper && swift test --filter InputTests`
Expected: FAIL — symbols undefined.

- [ ] **Step 3: Implement `Sources/Helper/Input.swift`**

```swift
import Foundation
import CoreGraphics

func clickGlobal(x: Double, y: Double, button: String, clicks: Int) throws {
    let pt = CGPoint(x: x, y: y)
    let mouseButton: CGMouseButton
    let downType: CGEventType
    let upType: CGEventType
    switch button {
    case "left":
        mouseButton = .left; downType = .leftMouseDown; upType = .leftMouseUp
    case "right":
        mouseButton = .right; downType = .rightMouseDown; upType = .rightMouseUp
    case "middle":
        mouseButton = .center; downType = .otherMouseDown; upType = .otherMouseUp
    default:
        throw HelperRPCError(code: "internal", message: "unknown button: \(button)")
    }
    for i in 1...max(1, clicks) {
        guard let down = CGEvent(mouseEventSource: nil, mouseType: downType,
                                 mouseCursorPosition: pt, mouseButton: mouseButton),
              let up = CGEvent(mouseEventSource: nil, mouseType: upType,
                               mouseCursorPosition: pt, mouseButton: mouseButton)
        else {
            throw HelperRPCError(code: "internal", message: "create CGEvent failed")
        }
        down.setIntegerValueField(.mouseEventClickState, value: Int64(i))
        up.setIntegerValueField(.mouseEventClickState, value: Int64(i))
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
    }
}

func typeText(_ text: String) throws {
    // Unicode mode: send each scalar via keyboard event with Unicode payload.
    // This bypasses the active IME, which is what we want for WeChat input
    // boxes that may otherwise interfere.
    for scalar in text.unicodeScalars {
        guard let down = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true),
              let up = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false)
        else {
            throw HelperRPCError(code: "internal", message: "create keyboard CGEvent failed")
        }
        let utf16 = Array(String(scalar).utf16)
        utf16.withUnsafeBufferPointer { buf in
            down.keyboardSetUnicodeString(stringLength: buf.count, unicodeString: buf.baseAddress)
            up.keyboardSetUnicodeString(stringLength: buf.count, unicodeString: buf.baseAddress)
        }
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
    }
}

func resolveKeyCombo(_ combo: String) throws -> (CGKeyCode, CGEventFlags) {
    var flags: CGEventFlags = []
    var keyName = combo.lowercased()
    let parts = combo.lowercased().split(separator: "+").map(String.init)
    if parts.count > 1 {
        for mod in parts.dropLast() {
            switch mod {
            case "cmd", "command": flags.insert(.maskCommand)
            case "shift": flags.insert(.maskShift)
            case "alt", "option", "opt": flags.insert(.maskAlternate)
            case "ctrl", "control": flags.insert(.maskControl)
            default:
                throw HelperRPCError(code: "internal", message: "unknown modifier: \(mod)")
            }
        }
        keyName = parts.last!
    }
    let keyCode: CGKeyCode
    switch keyName {
    case "return", "enter": keyCode = 0x24
    case "tab": keyCode = 0x30
    case "space": keyCode = 0x31
    case "escape", "esc": keyCode = 0x35
    case "delete", "backspace": keyCode = 0x33
    case "left": keyCode = 0x7B
    case "right": keyCode = 0x7C
    case "down": keyCode = 0x7D
    case "up": keyCode = 0x7E
    case "a": keyCode = 0x00
    case "c": keyCode = 0x08
    case "v": keyCode = 0x09
    case "x": keyCode = 0x07
    case "z": keyCode = 0x06
    default:
        throw HelperRPCError(code: "internal", message: "unknown key: \(keyName)")
    }
    return (keyCode, flags)
}

func sendKey(_ combo: String) throws {
    let (code, flags) = try resolveKeyCombo(combo)
    guard let down = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: true),
          let up = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: false)
    else {
        throw HelperRPCError(code: "internal", message: "create CGEvent failed")
    }
    down.flags = flags
    up.flags = flags
    down.post(tap: .cghidEventTap)
    up.post(tap: .cghidEventTap)
}

func scroll(x: Double, y: Double, dx: Double, dy: Double) throws {
    guard let ev = CGEvent(scrollWheelEvent2Source: nil,
                           units: .line, wheelCount: 2,
                           wheel1: Int32(dy), wheel2: Int32(dx),
                           wheel3: 0)
    else {
        throw HelperRPCError(code: "internal", message: "create scroll CGEvent failed")
    }
    ev.location = CGPoint(x: x, y: y)
    ev.post(tap: .cghidEventTap)
}
```

- [ ] **Step 4: Wire input methods into `main.swift` dispatch**

Add the cases:

```swift
case "click":
    do {
        struct P: Decodable { let x, y: Double; let button: String; let clicks: Int }
        let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
        try clickGlobal(x: p.x, y: p.y, button: p.button, clicks: p.clicks)
        writeOK(id: req.id)
    } catch let e as HelperRPCError {
        writeError(id: req.id, code: e.code, message: e.message)
    } catch {
        writeError(id: req.id, code: "internal", message: "\(error)")
    }
case "type":
    do {
        struct P: Decodable { let text: String }
        let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
        try typeText(p.text)
        writeOK(id: req.id)
    } catch let e as HelperRPCError {
        writeError(id: req.id, code: e.code, message: e.message)
    } catch {
        writeError(id: req.id, code: "internal", message: "\(error)")
    }
case "key":
    do {
        struct P: Decodable { let combo: String }
        let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
        try sendKey(p.combo)
        writeOK(id: req.id)
    } catch let e as HelperRPCError {
        writeError(id: req.id, code: e.code, message: e.message)
    } catch {
        writeError(id: req.id, code: "internal", message: "\(error)")
    }
case "scroll":
    do {
        struct P: Decodable { let x, y, dx, dy: Double }
        let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
        try scroll(x: p.x, y: p.y, dx: p.dx, dy: p.dy)
        writeOK(id: req.id)
    } catch let e as HelperRPCError {
        writeError(id: req.id, code: e.code, message: e.message)
    } catch {
        writeError(id: req.id, code: "internal", message: "\(error)")
    }
```

- [ ] **Step 5: Run Swift tests**

Run: `cd cmd/qdesk-mac-helper && swift test`
Expected: PASS or skip for `InputTests` (skip if no AX permission).

- [ ] **Step 6: Commit**

```bash
git add cmd/qdesk-mac-helper
git commit -m "feat(mac-helper): click/type/key/scroll via CGEvent"
```

---

## Task 8: Swift helper — Accessibility (axTree + axClick)

**Files:**
- Create: `cmd/qdesk-mac-helper/Sources/Helper/Accessibility.swift`
- Modify: `cmd/qdesk-mac-helper/Sources/Helper/main.swift`
- Create: `cmd/qdesk-mac-helper/Tests/HelperTests/AccessibilityTests.swift`

- [ ] **Step 1: Write failing test for axTree shape**

`Tests/HelperTests/AccessibilityTests.swift`:

```swift
import XCTest
@testable import Helper

final class AccessibilityTests: XCTestCase {
    func testAXTreeReturnsArrayShape() throws {
        let h = health()
        try XCTSkipUnless(h.accessibilityGranted, "Accessibility not granted; skipping")
        // Finder is always running. Query its full tree (no filter).
        let r = try axTree(bundleId: "com.apple.finder", query: "")
        // Either Finder has a window (nodes non-empty) or it doesn't —
        // we only assert the call returns without throwing.
        _ = r.nodes
    }

    func testQueryFilterRestrictsByRole() throws {
        let h = health()
        try XCTSkipUnless(h.accessibilityGranted, "Accessibility not granted; skipping")
        let r = try axTree(bundleId: "com.apple.finder", query: "role=AXWindow")
        for n in r.nodes {
            XCTAssertEqual(n.role, "AXWindow")
        }
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

Run: `cd cmd/qdesk-mac-helper && swift test --filter AccessibilityTests`
Expected: FAIL — `axTree` undefined.

- [ ] **Step 3: Implement `Sources/Helper/Accessibility.swift`**

```swift
import Foundation
import ApplicationServices
import AppKit

struct AXFrame: Encodable {
    let x, y, w, h: Double
}

struct AXNode: Encodable {
    let path: String
    let role: String
    let title: String?
    let value: String?
    let description: String?
    let frame: AXFrame
}

struct AXTreeResponse: Encodable {
    let nodes: [AXNode]
}

private func string(from el: AXUIElement, attr: String) -> String? {
    var raw: CFTypeRef?
    let r = AXUIElementCopyAttributeValue(el, attr as CFString, &raw)
    if r != .success { return nil }
    return raw as? String
}

private func frame(from el: AXUIElement) -> AXFrame {
    var pos: CFTypeRef?
    var size: CFTypeRef?
    AXUIElementCopyAttributeValue(el, kAXPositionAttribute as CFString, &pos)
    AXUIElementCopyAttributeValue(el, kAXSizeAttribute as CFString, &size)
    var p = CGPoint.zero
    var s = CGSize.zero
    if let pv = pos { AXValueGetValue(pv as! AXValue, .cgPoint, &p) }
    if let sv = size { AXValueGetValue(sv as! AXValue, .cgSize, &s) }
    return AXFrame(x: p.x, y: p.y, w: s.width, h: s.height)
}

private func walk(_ el: AXUIElement, path: String, into nodes: inout [AXNode],
                  filter: (String) -> Bool, depth: Int) {
    if depth > 200 { return }
    let role = string(from: el, attr: kAXRoleAttribute as String) ?? ""
    if filter(role) {
        nodes.append(AXNode(
            path: path,
            role: role,
            title: string(from: el, attr: kAXTitleAttribute as String),
            value: string(from: el, attr: kAXValueAttribute as String),
            description: string(from: el, attr: kAXDescriptionAttribute as String),
            frame: frame(from: el)
        ))
    }
    var children: CFTypeRef?
    AXUIElementCopyAttributeValue(el, kAXChildrenAttribute as CFString, &children)
    if let arr = children as? [AXUIElement] {
        for (i, child) in arr.enumerated() {
            walk(child, path: path.isEmpty ? "\(i)" : "\(path)/\(i)",
                 into: &nodes, filter: filter, depth: depth + 1)
        }
    }
}

func axTree(bundleId: String, query: String) throws -> AXTreeResponse {
    guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: bundleId).first else {
        throw HelperRPCError(code: "wechat-not-running",
                             message: "app \(bundleId) not running")
    }
    let appEl = AXUIElementCreateApplication(app.processIdentifier)
    var roleFilter: String? = nil
    if query.hasPrefix("role=") {
        roleFilter = String(query.dropFirst("role=".count))
    }
    let filter: (String) -> Bool = { role in
        if let rf = roleFilter { return role == rf }
        return true
    }
    var nodes: [AXNode] = []
    walk(appEl, path: "", into: &nodes, filter: filter, depth: 0)
    return AXTreeResponse(nodes: nodes)
}

func axClick(bundleId: String, path: String) throws {
    guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: bundleId).first else {
        throw HelperRPCError(code: "wechat-not-running",
                             message: "app \(bundleId) not running")
    }
    let appEl = AXUIElementCreateApplication(app.processIdentifier)
    let parts = path.split(separator: "/").compactMap { Int($0) }
    var cur: AXUIElement = appEl
    for idx in parts {
        var children: CFTypeRef?
        AXUIElementCopyAttributeValue(cur, kAXChildrenAttribute as CFString, &children)
        guard let arr = children as? [AXUIElement], idx < arr.count else {
            throw HelperRPCError(code: "internal",
                                 message: "AX path out of range at index \(idx)")
        }
        cur = arr[idx]
    }
    let r = AXUIElementPerformAction(cur, kAXPressAction as CFString)
    if r != .success {
        throw HelperRPCError(code: "internal", message: "AXPress failed: \(r.rawValue)")
    }
}
```

- [ ] **Step 4: Wire `axTree` and `axClick` into dispatch**

Add cases:

```swift
case "axTree":
    do {
        struct P: Decodable { let bundleId: String; let query: String }
        let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
        let r = try axTree(bundleId: p.bundleId, query: p.query)
        writeResult(id: req.id, value: r)
    } catch let e as HelperRPCError {
        writeError(id: req.id, code: e.code, message: e.message)
    } catch {
        writeError(id: req.id, code: "internal", message: "\(error)")
    }
case "axClick":
    do {
        struct P: Decodable { let bundleId: String; let path: String }
        let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
        try axClick(bundleId: p.bundleId, path: p.path)
        writeOK(id: req.id)
    } catch let e as HelperRPCError {
        writeError(id: req.id, code: e.code, message: e.message)
    } catch {
        writeError(id: req.id, code: "internal", message: "\(error)")
    }
```

- [ ] **Step 5: Run Swift tests**

Run: `cd cmd/qdesk-mac-helper && swift test`
Expected: PASS or skip (skip if AX not granted).

- [ ] **Step 6: Commit**

```bash
git add cmd/qdesk-mac-helper
git commit -m "feat(mac-helper): axTree + axClick via AXUIElement"
```

---

## Task 9: Go MCP server scaffolding (initialize + tools/list)

**Files:**
- Modify: `cmd/qdesk-mac/main.go`
- Create: `internal/macserver/mcp.go`
- Create: `internal/macserver/mcp_test.go`
- Create: `internal/macserver/fakehelper.go`

- [ ] **Step 1: Create `internal/macserver/fakehelper.go` (test double for the supervisor)**

```go
package macserver

import (
	"context"
	"encoding/json"
	"errors"
)

// FakeSupervisor implements the same Call/Close interface as Supervisor for
// tests. Use SetHandler to control responses per method.
type FakeSupervisor struct {
	handlers map[string]func(json.RawMessage) (json.RawMessage, error)
}

func NewFakeSupervisor() *FakeSupervisor {
	return &FakeSupervisor{handlers: map[string]func(json.RawMessage) (json.RawMessage, error){}}
}

func (f *FakeSupervisor) SetHandler(method string, fn func(json.RawMessage) (json.RawMessage, error)) {
	f.handlers[method] = fn
}

func (f *FakeSupervisor) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	h, ok := f.handlers[method]
	if !ok {
		return nil, errors.New("fake: no handler for " + method)
	}
	return h(params)
}

func (f *FakeSupervisor) Close() error { return nil }

// HelperClient is what tools.go depends on; both Supervisor and FakeSupervisor satisfy it.
type HelperClient interface {
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	Close() error
}
```

- [ ] **Step 2: Write failing test for tools/list**

`internal/macserver/mcp_test.go`:

```go
package macserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolsListIncludesExpectedTools(t *testing.T) {
	srv := NewMCPServer(NewFakeSupervisor())
	resp := srv.Handle(context.Background(), &RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	b, _ := json.Marshal(resp.Result)
	got := string(b)
	for _, want := range []string{
		"wechat.screenshot", "wechat.click", "wechat.type", "wechat.key",
		"wechat.scroll", "wechat.ensure_foreground",
		"wechat.list_chats", "wechat.open_chat",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tools/list missing %q; got=%s", want, got)
		}
	}
}

func TestInitializeReturnsServerInfo(t *testing.T) {
	srv := NewMCPServer(NewFakeSupervisor())
	resp := srv.Handle(context.Background(), &RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	b, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(b), "qdesk-mac") {
		t.Errorf("expected serverInfo.name=qdesk-mac; got %s", b)
	}
}
```

- [ ] **Step 3: Run test, confirm it fails**

Run: `go test ./internal/macserver/... -v -run "TestToolsList|TestInitialize"`
Expected: FAIL — types undefined.

- [ ] **Step 4: Implement `internal/macserver/mcp.go`**

```go
package macserver

import (
	"context"
	"encoding/json"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "qdesk-mac"
	wechatBundleID  = "com.tencent.xinWeChat"
)

// RPCRequest mirrors the existing qdesk-mcp envelope so we keep one shape.
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

// MCPServer is the stdio MCP server for qdesk-mac.
type MCPServer struct {
	helper HelperClient
}

func NewMCPServer(h HelperClient) *MCPServer {
	return &MCPServer{helper: h}
}

func (s *MCPServer) Handle(ctx context.Context, req *RPCRequest) *RPCResponse {
	resp := &RPCResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": "0.1.0"},
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
			Name:        "wechat.screenshot",
			Description: "Take a full-screen screenshot. Returns a PNG plus the bundle ID and name of the current foreground app — check this to see whether you need to call wechat.ensure_foreground first.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "wechat.ensure_foreground",
			Description: "Bring WeChat to the foreground. Returns an error if WeChat is not running (the user must launch it manually; this tool does not auto-launch apps).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "wechat.click",
			Description: "Click at LOGICAL global screen coordinates. Requires WeChat to be the foreground app — call wechat.ensure_foreground first if not.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":      map[string]any{"type": "number"},
					"y":      map[string]any{"type": "number"},
					"button": map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "default": "left"},
					"clicks": map[string]any{"type": "integer", "minimum": 1, "maximum": 3, "default": 1},
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "wechat.type",
			Description: "Type Unicode text (including Chinese) at the current focus. Bypasses IME via CGEvent unicode mode.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
				"required":   []string{"text"},
			},
		},
		{
			Name:        "wechat.key",
			Description: "Send a key combo, e.g. \"return\", \"escape\", \"cmd+v\".",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"combo": map[string]any{"type": "string"}},
				"required":   []string{"combo"},
			},
		},
		{
			Name:        "wechat.scroll",
			Description: "Wheel-scroll at LOGICAL screen point (x, y). Positive dy scrolls up.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":  map[string]any{"type": "number"},
					"y":  map[string]any{"type": "number"},
					"dy": map[string]any{"type": "number"},
					"dx": map[string]any{"type": "number", "default": 0},
				},
				"required": []string{"x", "y", "dy"},
			},
		},
		{
			Name:        "wechat.list_chats",
			Description: "Return WeChat sidebar chat list as structured JSON: name, unread_count, last_msg_preview. Uses the Accessibility API — no vision needed.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "wechat.open_chat",
			Description: "Open the conversation with the given chat name. Uses fuzzy matching on the sidebar; falls back to the cmd+f search field if no sidebar match.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []string{"name"},
			},
		},
	}
}

// callTool is implemented in tools.go.
```

- [ ] **Step 5: Add a stub `callTool` so package compiles**

Append to `mcp.go` (will be replaced in later tasks):

```go
func (s *MCPServer) callTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	return nil, errNotImplemented(name)
}

func errNotImplemented(name string) error {
	return &notImplErr{name: name}
}

type notImplErr struct{ name string }

func (e *notImplErr) Error() string { return "tool not implemented: " + e.name }
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/macserver/... -v -run "TestToolsList|TestInitialize"`
Expected: PASS.

- [ ] **Step 7: Wire into `cmd/qdesk-mac/main.go`**

Replace `cmd/qdesk-mac/main.go` with:

```go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jeffwang/qdesk/internal/macserver"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		os.Exit(runDoctor())
	}

	helperPath := flag.String("helper",
		envOr("QDESK_MAC_HELPER", defaultHelperPath()),
		"path to qdesk-mac-helper binary")
	flag.Parse()

	logf("qdesk-mac starting; helper=%s", *helperPath)

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	sup, err := macserver.Spawn(ctx, *helperPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "qdesk-mac: spawn helper failed: %v\n", err)
		os.Exit(1)
	}
	defer sup.Close()

	srv := macserver.NewMCPServer(sup)

	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, err := in.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, os.ErrClosed) || err.Error() == "EOF" {
				return
			}
			logf("read: %v", err)
			return
		}
		line = []byte(strings.TrimSpace(string(line)))
		if len(line) == 0 {
			continue
		}
		var req macserver.RPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			logf("invalid JSON-RPC: %v", err)
			continue
		}
		if len(req.ID) == 0 {
			continue // notifications: ignore
		}
		resp := srv.Handle(ctx, &req)
		b, _ := json.Marshal(resp)
		b = append(b, '\n')
		if _, err := out.Write(b); err != nil {
			logf("write: %v", err)
			return
		}
		_ = out.Flush()
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func defaultHelperPath() string {
	exe, err := os.Executable()
	if err == nil {
		// Side-by-side with qdesk-mac in the same directory.
		return filepath.Join(filepath.Dir(exe), "qdesk-mac-helper")
	}
	return "/usr/local/bin/qdesk-mac-helper"
}

func logf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "qdesk-mac: "+fmt.Sprintf(format, args...))
}

// runDoctor is implemented in doctor.go (Task 13).
func runDoctor() int { fmt.Fprintln(os.Stderr, "doctor: not yet implemented"); return 1 }
```

- [ ] **Step 8: Verify build + tests still pass**

Run: `make mac-build && go test ./...`
Expected: builds; mcp_test.go passes; supervisor_test.go skips if helper not yet rebuilt.

- [ ] **Step 9: Commit**

```bash
git add internal/macserver cmd/qdesk-mac/main.go
git commit -m "feat(mac): MCP server (initialize, tools/list, dispatch shell)"
```

---

## Task 10: Tool — `wechat.ensure_foreground` and `wechat.screenshot` + foreground guard

**Files:**
- Create: `internal/macserver/guard.go`
- Create: `internal/macserver/guard_test.go`
- Modify: `internal/macserver/mcp.go` (replace stub `callTool`)
- Create: `internal/macserver/tools.go`
- Create: `internal/macserver/tools_test.go`

- [ ] **Step 1: Write failing tests**

`internal/macserver/tools_test.go`:

```go
package macserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeffwang/qdesk/internal/macproto"
)

func newServerWithFake(setup func(*FakeSupervisor)) *MCPServer {
	f := NewFakeSupervisor()
	if setup != nil {
		setup(f)
	}
	return NewMCPServer(f)
}

func TestEnsureForegroundActivatesWeChat(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.apple.finder","name":"Finder","pid":1}`), nil
		})
		f.SetHandler(macproto.MethodActivate, func(p json.RawMessage) (json.RawMessage, error) {
			if !strings.Contains(string(p), "com.tencent.xinWeChat") {
				t.Errorf("activate not called with WeChat bundle: %s", p)
			}
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.ensure_foreground", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Errorf("unexpected error: %+v", out)
	}
}

func TestScreenshotPassesThrough(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat","name":"WeChat","pid":99}`), nil
		})
		f.SetHandler(macproto.MethodScreenshot, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"pngBase64":"aGVsbG8=","width":1440,"height":900,"scaleFactor":2}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.screenshot", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error: %+v", out)
	}
	// Result should include both an image content item and a text item with frontApp info.
	hasImage := false
	hasText := false
	for _, c := range out.Content {
		if c.Type == "image" && c.MIMEType == "image/png" && c.Data == "aGVsbG8=" {
			hasImage = true
		}
		if c.Type == "text" && strings.Contains(c.Text, "com.tencent.xinWeChat") {
			hasText = true
		}
	}
	if !hasImage || !hasText {
		t.Errorf("missing content: image=%v text=%v in %+v", hasImage, hasText, out.Content)
	}
}

func TestForegroundGuardRejectsWhenNotWeChat(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.apple.finder","name":"Finder","pid":1}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.click",
		json.RawMessage(`{"x":100,"y":200}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected isError; got: %+v", out)
	}
	if !strings.Contains(out.Content[0].Text, macproto.CodeWeChatNotForeground) {
		t.Errorf("expected wechat-not-foreground code in error; got %s", out.Content[0].Text)
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/macserver/... -v -run "TestEnsureForeground|TestScreenshotPassesThrough|TestForegroundGuard"`
Expected: FAIL — `callTool` is stubbed.

- [ ] **Step 3: Implement `internal/macserver/guard.go`**

```go
package macserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// requireWeChatForeground returns nil if WeChat is the front app, or a
// structured error otherwise. Used by every action tool.
func requireWeChatForeground(ctx context.Context, h HelperClient) error {
	raw, err := h.Call(ctx, macproto.MethodFrontApp, json.RawMessage(`{}`))
	if err != nil {
		return fmt.Errorf("frontApp: %w", err)
	}
	var fa macproto.FrontAppResponse
	if err := json.Unmarshal(raw, &fa); err != nil {
		return fmt.Errorf("decode frontApp: %w", err)
	}
	if fa.BundleID != wechatBundleID {
		return &guardErr{
			Code: macproto.CodeWeChatNotForeground,
			Msg:  fmt.Sprintf("front app is %s (%s); call wechat.ensure_foreground first", fa.BundleID, fa.Name),
		}
	}
	return nil
}

type guardErr struct {
	Code string
	Msg  string
}

func (e *guardErr) Error() string { return e.Code + ": " + e.Msg }
```

- [ ] **Step 4: Implement `internal/macserver/tools.go`**

```go
package macserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jeffwang/qdesk/internal/macproto"
)

func (s *MCPServer) callTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	switch name {
	case "wechat.ensure_foreground":
		return s.toolEnsureForeground(ctx)
	case "wechat.screenshot":
		return s.toolScreenshot(ctx)
	case "wechat.click", "wechat.type", "wechat.key", "wechat.scroll":
		// Action tools all share the foreground guard; implementations live
		// in the next task. For now return a typed not-implemented.
		if err := requireWeChatForeground(ctx, s.helper); err != nil {
			return errToolResult(err), nil
		}
		return errToolResult(fmt.Errorf("tool not yet implemented: %s", name)), nil
	case "wechat.list_chats", "wechat.open_chat":
		return errToolResult(fmt.Errorf("tool not yet implemented: %s", name)), nil
	default:
		return errToolResult(fmt.Errorf("unknown tool: %s", name)), nil
	}
}

func (s *MCPServer) toolEnsureForeground(ctx context.Context) (*ToolResult, error) {
	body, _ := json.Marshal(macproto.ActivateRequest{BundleID: wechatBundleID})
	if _, err := s.helper.Call(ctx, macproto.MethodActivate, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{
		Content: []ContentItem{{Type: "text", Text: "WeChat brought to foreground."}},
	}, nil
}

func (s *MCPServer) toolScreenshot(ctx context.Context) (*ToolResult, error) {
	// Get foreground info first so we can include it in the response.
	frontRaw, err := s.helper.Call(ctx, macproto.MethodFrontApp, json.RawMessage(`{}`))
	if err != nil {
		return errToolResult(err), nil
	}
	var front macproto.FrontAppResponse
	_ = json.Unmarshal(frontRaw, &front)

	shotRaw, err := s.helper.Call(ctx, macproto.MethodScreenshot, json.RawMessage(`{"format":"png"}`))
	if err != nil {
		return errToolResult(err), nil
	}
	var shot macproto.ScreenshotResponse
	if err := json.Unmarshal(shotRaw, &shot); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{
		Content: []ContentItem{
			{Type: "image", MIMEType: "image/png", Data: shot.PNGBase64},
			{Type: "text", Text: fmt.Sprintf(
				"frontApp.bundleId=%s name=%q  size=%dx%d (logical) scale=%.1f",
				front.BundleID, front.Name, shot.Width, shot.Height, shot.ScaleFactor)},
		},
	}, nil
}

func errToolResult(err error) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentItem{{Type: "text", Text: err.Error()}},
	}
}
```

- [ ] **Step 5: Run tests, verify pass**

Run: `go test ./internal/macserver/... -v`
Expected: PASS for all three new tests + all earlier tests.

- [ ] **Step 6: Commit**

```bash
git add internal/macserver/guard.go internal/macserver/tools.go internal/macserver/tools_test.go
git commit -m "feat(mac): wechat.screenshot + ensure_foreground + foreground guard"
```

---

## Task 11: Tools — `wechat.click`, `type`, `key`, `scroll`

**Files:**
- Modify: `internal/macserver/tools.go`
- Modify: `internal/macserver/tools_test.go`

- [ ] **Step 1: Write failing tests for action passthrough**

Append to `internal/macserver/tools_test.go`:

```go
func TestClickPassesXYButtonClicks(t *testing.T) {
	var captured json.RawMessage
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodClick, func(p json.RawMessage) (json.RawMessage, error) {
			captured = p
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.click",
		json.RawMessage(`{"x":12.5,"y":34.5,"button":"left","clicks":2}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected: %+v", out)
	}
	want := `"x":12.5`
	if !strings.Contains(string(captured), want) {
		t.Errorf("captured payload missing %s: %s", want, captured)
	}
	if !strings.Contains(string(captured), `"clicks":2`) {
		t.Errorf("clicks not propagated: %s", captured)
	}
}

func TestTypeSendsText(t *testing.T) {
	var captured json.RawMessage
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodType, func(p json.RawMessage) (json.RawMessage, error) {
			captured = p
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.type",
		json.RawMessage(`{"text":"你好"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected: %+v", out)
	}
	if !strings.Contains(string(captured), "你好") {
		t.Errorf("text not propagated: %s", captured)
	}
}

func TestKeyAndScrollPassthrough(t *testing.T) {
	gotKey := ""
	gotScroll := ""
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodKey, func(p json.RawMessage) (json.RawMessage, error) {
			gotKey = string(p)
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodScroll, func(p json.RawMessage) (json.RawMessage, error) {
			gotScroll = string(p)
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "wechat.key",
		json.RawMessage(`{"combo":"return"}`)); err != nil {
		t.Fatalf("key err: %v", err)
	}
	if !strings.Contains(gotKey, "return") {
		t.Errorf("key combo not propagated: %s", gotKey)
	}
	if _, err := srv.callTool(context.Background(), "wechat.scroll",
		json.RawMessage(`{"x":100,"y":200,"dy":-3,"dx":0}`)); err != nil {
		t.Fatalf("scroll err: %v", err)
	}
	if !strings.Contains(gotScroll, `"dy":-3`) {
		t.Errorf("scroll dy not propagated: %s", gotScroll)
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/macserver/... -v -run "TestClick|TestType|TestKeyAndScroll"`
Expected: FAIL — those tools return "not yet implemented".

- [ ] **Step 3: Implement action passthrough in `tools.go`**

Replace the action-tools branch in `callTool` with:

```go
case "wechat.click":
	if err := requireWeChatForeground(ctx, s.helper); err != nil {
		return errToolResult(err), nil
	}
	return s.toolClick(ctx, args)
case "wechat.type":
	if err := requireWeChatForeground(ctx, s.helper); err != nil {
		return errToolResult(err), nil
	}
	return s.toolType(ctx, args)
case "wechat.key":
	if err := requireWeChatForeground(ctx, s.helper); err != nil {
		return errToolResult(err), nil
	}
	return s.toolKey(ctx, args)
case "wechat.scroll":
	if err := requireWeChatForeground(ctx, s.helper); err != nil {
		return errToolResult(err), nil
	}
	return s.toolScroll(ctx, args)
```

And add the implementations at the bottom of `tools.go`:

```go
func (s *MCPServer) toolClick(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		X, Y   float64
		Button string
		Clicks int
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	if in.Button == "" {
		in.Button = "left"
	}
	if in.Clicks == 0 {
		in.Clicks = 1
	}
	body, _ := json.Marshal(macproto.ClickRequest{
		X: in.X, Y: in.Y, Button: in.Button, Clicks: in.Clicks,
	})
	if _, err := s.helper.Call(ctx, macproto.MethodClick, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("clicked %s×%d at (%.1f, %.1f)", in.Button, in.Clicks, in.X, in.Y)}}}, nil
}

func (s *MCPServer) toolType(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ Text string }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.Marshal(macproto.TypeRequest{Text: in.Text})
	if _, err := s.helper.Call(ctx, macproto.MethodType, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("typed %d characters", len([]rune(in.Text)))}}}, nil
}

func (s *MCPServer) toolKey(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ Combo string }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.Marshal(macproto.KeyRequest{Combo: in.Combo})
	if _, err := s.helper.Call(ctx, macproto.MethodKey, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("sent key %q", in.Combo)}}}, nil
}

func (s *MCPServer) toolScroll(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ X, Y, DX, DY float64 }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.Marshal(macproto.ScrollRequest{X: in.X, Y: in.Y, DX: in.DX, DY: in.DY})
	if _, err := s.helper.Call(ctx, macproto.MethodScroll, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("scrolled (dx=%.1f dy=%.1f) at (%.1f, %.1f)", in.DX, in.DY, in.X, in.Y)}}}, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/macserver/... -v`
Expected: PASS for all 5 action-tool tests.

- [ ] **Step 5: Commit**

```bash
git add internal/macserver/tools.go internal/macserver/tools_test.go
git commit -m "feat(mac): wechat.click/type/key/scroll tools"
```

---

## Task 12: Tools — `wechat.list_chats` and `wechat.open_chat`

**Files:**
- Create: `internal/macserver/chats.go`
- Create: `internal/macserver/chats_test.go`
- Modify: `internal/macserver/tools.go` (replace stubs)

- [ ] **Step 1: Write failing tests with a fixture AX tree**

`internal/macserver/chats_test.go`:

```go
package macserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// fixtureSidebar simulates the AX tree the helper would return for the
// WeChat chat sidebar. Each chat row is an AXRow with title="Name" and
// description="last message preview". Unread badge is in value="N".
const fixtureSidebar = `{
  "nodes": [
    {"path":"0/1/2/0","role":"AXRow","title":"张三","value":"2","description":"晚点到，10分钟","frame":{"x":0,"y":40,"w":280,"h":56}},
    {"path":"0/1/2/1","role":"AXRow","title":"李四","value":"","description":"OK","frame":{"x":0,"y":96,"w":280,"h":56}},
    {"path":"0/1/2/2","role":"AXRow","title":"产品讨论群","value":"5","description":"小王: 周三发版","frame":{"x":0,"y":152,"w":280,"h":56}}
  ]
}`

func TestListChatsParsesAXTree(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodAXTree, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(fixtureSidebar), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.list_chats", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error: %+v", out)
	}
	body := out.Content[0].Text
	for _, want := range []string{"张三", "李四", "产品讨论群", "unread_count\":2", "unread_count\":5"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in body: %s", want, body)
		}
	}
}

func TestOpenChatExactMatch(t *testing.T) {
	clicked := ""
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodAXTree, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(fixtureSidebar), nil
		})
		f.SetHandler(macproto.MethodAXClick, func(p json.RawMessage) (json.RawMessage, error) {
			clicked = string(p)
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "wechat.open_chat",
		json.RawMessage(`{"name":"张三"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(clicked, `"path":"0/1/2/0"`) {
		t.Errorf("expected to click 张三 (path 0/1/2/0); clicked=%s", clicked)
	}
}

func TestOpenChatNotFoundReturnsError(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodAXTree, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(fixtureSidebar), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.open_chat",
		json.RawMessage(`{"name":"陈八"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected error result; got %+v", out)
	}
	if !strings.Contains(out.Content[0].Text, macproto.CodeChatNotFound) {
		t.Errorf("missing chat-not-found code: %s", out.Content[0].Text)
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/macserver/... -v -run "TestListChats|TestOpenChat"`
Expected: FAIL — tools return "not yet implemented".

- [ ] **Step 3: Implement `internal/macserver/chats.go`**

```go
package macserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// ChatRow is the structured representation we surface to the LLM.
type ChatRow struct {
	Name           string `json:"name"`
	UnreadCount    int    `json:"unread_count"`
	LastMsgPreview string `json:"last_msg_preview"`
	axPath         string // not serialised; used internally for open_chat
}

func (s *MCPServer) toolListChats(ctx context.Context) (*ToolResult, error) {
	rows, err := s.fetchChats(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.MarshalIndent(rows, "", "  ")
	return &ToolResult{Content: []ContentItem{{Type: "text", Text: string(body)}}}, nil
}

func (s *MCPServer) toolOpenChat(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ Name string }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if in.Name == "" {
		return errToolResult(errors.New("name is required")), nil
	}
	rows, err := s.fetchChats(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	match := matchChat(rows, in.Name)
	if match == nil {
		// v1 simplification: no automatic cmd+f search-box fallback.
		// The LLM can compose that itself: wechat.key cmd+f, wechat.type, wechat.key return.
		return errToolResult(&HelperError{
			Code:    macproto.CodeChatNotFound,
			Message: fmt.Sprintf("no chat matched %q in sidebar (%d shown). Try cmd+f search box manually.", in.Name, len(rows)),
		}), nil
	}
	clickBody, _ := json.Marshal(macproto.AXClickRequest{
		BundleID: wechatBundleID, Path: match.axPath,
	})
	if _, err := s.helper.Call(ctx, macproto.MethodAXClick, clickBody); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("opened chat %q", match.Name)}}}, nil
}

func (s *MCPServer) fetchChats(ctx context.Context) ([]ChatRow, error) {
	body, _ := json.Marshal(macproto.AXTreeRequest{
		BundleID: wechatBundleID, Query: "role=AXRow",
	})
	raw, err := s.helper.Call(ctx, macproto.MethodAXTree, body)
	if err != nil {
		return nil, err
	}
	var tree macproto.AXTreeResponse
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	if len(tree.Nodes) == 0 {
		return nil, &HelperError{
			Code:    macproto.CodeAXTreeEmpty,
			Message: "WeChat sidebar AX tree is empty (open the main window first: cmd+1)",
		}
	}
	out := make([]ChatRow, 0, len(tree.Nodes))
	for _, n := range tree.Nodes {
		if n.Title == "" {
			continue
		}
		unread := 0
		if n.Value != "" {
			if v, err := strconv.Atoi(strings.TrimSpace(n.Value)); err == nil {
				unread = v
			}
		}
		out = append(out, ChatRow{
			Name:           n.Title,
			UnreadCount:    unread,
			LastMsgPreview: n.Description,
			axPath:         n.Path,
		})
	}
	return out, nil
}

// matchChat finds the best chat row for the given query name.
// Order: exact match > prefix match > substring match. Returns nil if none.
func matchChat(rows []ChatRow, name string) *ChatRow {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	for i := range rows {
		if strings.HasPrefix(rows[i].Name, name) {
			return &rows[i]
		}
	}
	for i := range rows {
		if strings.Contains(rows[i].Name, name) {
			return &rows[i]
		}
	}
	return nil
}
```

- [ ] **Step 4: Wire into `callTool`**

Replace the `wechat.list_chats` / `wechat.open_chat` branch in `tools.go`:

```go
case "wechat.list_chats":
	if err := requireWeChatForeground(ctx, s.helper); err != nil {
		return errToolResult(err), nil
	}
	return s.toolListChats(ctx)
case "wechat.open_chat":
	if err := requireWeChatForeground(ctx, s.helper); err != nil {
		return errToolResult(err), nil
	}
	return s.toolOpenChat(ctx, args)
```

- [ ] **Step 5: Run tests, verify pass**

Run: `go test ./internal/macserver/... -v`
Expected: PASS for all chat tests + all earlier tests.

- [ ] **Step 6: Commit**

```bash
git add internal/macserver/chats.go internal/macserver/chats_test.go internal/macserver/tools.go
git commit -m "feat(mac): wechat.list_chats + open_chat via AXTree"
```

---

## Task 13: `qdesk-mac doctor` subcommand

**Files:**
- Create: `cmd/qdesk-mac/doctor.go`
- Create: `cmd/qdesk-mac/doctor_test.go`

- [ ] **Step 1: Write failing test**

`cmd/qdesk-mac/doctor_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestFormatHealthIncludesPermissionPanelHints(t *testing.T) {
	out := formatHealthReport(false, false)
	if !strings.Contains(out, "Screen Recording") {
		t.Errorf("missing Screen Recording mention: %s", out)
	}
	if !strings.Contains(out, "Accessibility") {
		t.Errorf("missing Accessibility mention: %s", out)
	}
	if !strings.Contains(out, "x-apple.systempreferences:") {
		t.Errorf("missing settings panel URL: %s", out)
	}
}

func TestFormatHealthAllGreen(t *testing.T) {
	out := formatHealthReport(true, true)
	if !strings.Contains(out, "All permissions granted") {
		t.Errorf("expected green message; got %s", out)
	}
}
```

- [ ] **Step 2: Run, confirm failure**

Run: `go test ./cmd/qdesk-mac/... -v`
Expected: FAIL.

- [ ] **Step 3: Implement `cmd/qdesk-mac/doctor.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jeffwang/qdesk/internal/macproto"
	"github.com/jeffwang/qdesk/internal/macserver"
)

const (
	srPanelURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	axPanelURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
)

// runDoctor (replaces the stub in main.go) probes the helper for
// permission status and prints a remediation report.
func runDoctorReal() int {
	helperPath := defaultHelperPath()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sup, err := macserver.Spawn(ctx, helperPath)
	if err != nil {
		fmt.Println("ERROR: cannot spawn helper at", helperPath)
		fmt.Println("  ", err)
		fmt.Println("Make sure qdesk-mac-helper is installed alongside qdesk-mac.")
		return 1
	}
	defer sup.Close()

	raw, err := sup.Call(ctx, macproto.MethodHealth, json.RawMessage(`{}`))
	if err != nil {
		fmt.Println("ERROR: health RPC failed:", err)
		return 1
	}
	var h macproto.HealthResponse
	if err := json.Unmarshal(raw, &h); err != nil {
		fmt.Println("ERROR: cannot decode health:", err)
		return 1
	}
	report := formatHealthReport(h.ScreenRecordingGranted, h.AccessibilityGranted)
	fmt.Println(report)
	if !h.ScreenRecordingGranted {
		_ = exec.Command("open", srPanelURL).Run()
	}
	if !h.AccessibilityGranted {
		_ = exec.Command("open", axPanelURL).Run()
	}
	if h.ScreenRecordingGranted && h.AccessibilityGranted {
		return 0
	}
	return 2
}

func formatHealthReport(sr, ax bool) string {
	if sr && ax {
		return "All permissions granted. qdesk-mac is ready."
	}
	var b strings.Builder
	b.WriteString("qdesk-mac: missing permissions\n\n")
	b.WriteString("Helper binary path (grant TCC permissions to THIS path):\n")
	b.WriteString("  " + defaultHelperPath() + "\n\n")
	if !sr {
		b.WriteString("[ ] Screen Recording — needed to capture screenshots\n")
		b.WriteString("    Open: " + srPanelURL + "\n")
		b.WriteString("    Add the helper binary above to the list and enable it.\n\n")
	}
	if !ax {
		b.WriteString("[ ] Accessibility — needed for click/type/key and AX tree access\n")
		b.WriteString("    Open: " + axPanelURL + "\n")
		b.WriteString("    Add the helper binary above to the list and enable it.\n\n")
	}
	b.WriteString("After granting, run `qdesk-mac doctor` again.")
	return b.String()
}
```

- [ ] **Step 4: Replace the stub in `main.go`**

In `cmd/qdesk-mac/main.go`, change:

```go
func runDoctor() int { fmt.Fprintln(os.Stderr, "doctor: not yet implemented"); return 1 }
```

to:

```go
func runDoctor() int { return runDoctorReal() }
```

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/qdesk-mac/... -v`
Expected: PASS.

- [ ] **Step 6: Manual smoke test**

Run:
```bash
make mac-build
./bin/qdesk-mac doctor
```

Expected: prints either "All permissions granted" or a checklist with helper path + Settings panel URLs (and opens System Settings panes).

- [ ] **Step 7: Commit**

```bash
git add cmd/qdesk-mac
git commit -m "feat(mac): qdesk-mac doctor — permission probe + Settings hints"
```

---

## Task 14: Install script + README + example

**Files:**
- Create: `scripts/install-mac.sh`
- Create: `examples/wechat-reply.md`
- Modify: `README.md`

- [ ] **Step 1: Create `scripts/install-mac.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

# Builds qdesk-mac + qdesk-mac-helper from source and installs them to
# /usr/local/bin. The helper path MUST be stable so macOS TCC doesn't
# re-prompt for Screen Recording / Accessibility on every rebuild.

if [[ "$(uname)" != "Darwin" ]]; then
  echo "qdesk-mac is macOS-only. This is $(uname)." >&2
  exit 1
fi

cd "$(dirname "$0")/.."
make mac-build

DEST="/usr/local/bin"
if [[ ! -d "$DEST" ]]; then
  sudo mkdir -p "$DEST"
fi

echo "Installing to $DEST (will prompt for sudo)..."
sudo install -m 0755 bin/qdesk-mac        "$DEST/qdesk-mac"
sudo install -m 0755 bin/qdesk-mac-helper "$DEST/qdesk-mac-helper"

echo
echo "Installed:"
ls -l "$DEST/qdesk-mac" "$DEST/qdesk-mac-helper"
echo
echo "Next: run \`qdesk-mac doctor\` to grant Screen Recording + Accessibility"
echo "to /usr/local/bin/qdesk-mac-helper."
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x scripts/install-mac.sh`

- [ ] **Step 3: Create `examples/wechat-reply.md`**

```markdown
# Example: AI replies a WeChat message

This example uses Claude Code as the MCP client. Other MCP-aware tools
(Cursor, etc.) work the same way.

## One-time setup

1. Build and install:
   ```
   ./scripts/install-mac.sh
   ```
2. Grant permissions:
   ```
   qdesk-mac doctor
   ```
   System Settings opens twice (Screen Recording + Accessibility). Add
   `/usr/local/bin/qdesk-mac-helper` to each list and enable it. Run
   `doctor` again to verify.
3. Register with Claude Code:
   ```
   claude mcp add --transport stdio qdesk-mac -- /usr/local/bin/qdesk-mac
   ```

## Replying to a chat

Open WeChat and log in. Then in Claude Code:

> Use qdesk-mac. Find the chat with 张三 in WeChat and send the message
> "晚点到，10分钟". Confirm it was sent.

Claude will typically:

1. Call `wechat.ensure_foreground` to bring WeChat to front.
2. Call `wechat.list_chats` to see available chats (no vision needed).
3. Call `wechat.open_chat` with `{"name": "张三"}`.
4. Call `wechat.screenshot` to verify the chat opened and find the input box.
5. Call `wechat.click` on the input box, then `wechat.type` with the message.
6. Call `wechat.key` with `{"combo": "return"}` to send.

If something goes wrong (unread badge, wrong window), Claude can call
`wechat.screenshot` again to re-orient.

## Cost

~5–10 tool calls × ~1 screenshot = one Claude API request per call. With
Sonnet, expect $0.01–$0.03 per reply session.

## Limitations (v1)

- Single account only (whichever WeChat is currently logged in).
- Does not auto-launch WeChat — you must start it manually first.
- Screenshots include your full desktop. Don't run on a screen with sensitive
  windows you don't want the model to see.
```

- [ ] **Step 4: Add README section**

Insert into `README.md` (after the existing "Use cases" / before "Project structure" sections — exact location to be chosen by the engineer based on current README):

```markdown
## Mac host mode (alpha) — control your local WeChat

In addition to the Linux Docker sandbox, qdesk now ships a **Mac host mode**
for AI assistants to drive native macOS apps. v1 targets WeChat.

```bash
./scripts/install-mac.sh
qdesk-mac doctor   # grants Screen Recording + Accessibility
claude mcp add --transport stdio qdesk-mac -- /usr/local/bin/qdesk-mac
```

The MCP tools live under `wechat.*`: `screenshot`, `click`, `type`, `key`,
`scroll`, `ensure_foreground`, `list_chats`, `open_chat`. See
[`examples/wechat-reply.md`](./examples/wechat-reply.md).

**v1 limitations:** macOS 13+, single WeChat instance, action calls
require WeChat to be the foreground app, screenshots are full-screen
(includes other apps' windows). No code signing — TCC may re-prompt
after rebuild.
```

- [ ] **Step 5: Manual verification**

Run:
```bash
./scripts/install-mac.sh
qdesk-mac doctor
```

Expected: install completes; doctor prints status. If permissions are
granted and WeChat is open, manually try one tool via:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize"}' | qdesk-mac
echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | qdesk-mac
```

Expected: valid JSON-RPC responses on stdout.

- [ ] **Step 6: Commit**

```bash
git add scripts/install-mac.sh examples/wechat-reply.md README.md
git commit -m "docs(mac): install script + README section + wechat-reply example"
```

---

## Task 15: macOS CI workflow

**Files:**
- Create: `.github/workflows/mac.yml`

- [ ] **Step 1: Inspect the existing CI workflow for style**

Run: `cat .github/workflows/*.yml`
Expected: see existing Go lint+test workflow. Match its style for naming, action versions, etc.

- [ ] **Step 2: Create `.github/workflows/mac.yml`**

```yaml
name: mac

on:
  push:
    branches: [main]
    paths:
      - "cmd/qdesk-mac/**"
      - "cmd/qdesk-mac-helper/**"
      - "internal/macproto/**"
      - "internal/macserver/**"
      - ".github/workflows/mac.yml"
      - "Makefile"
  pull_request:
    paths:
      - "cmd/qdesk-mac/**"
      - "cmd/qdesk-mac-helper/**"
      - "internal/macproto/**"
      - "internal/macserver/**"
      - ".github/workflows/mac.yml"

jobs:
  build-and-test:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Build
        run: make mac-build

      - name: Go tests
        # Supervisor + tools tests; helper is built so supervisor_test will run.
        # AX-dependent helper tests will skip without TCC permission.
        run: go test -v ./internal/macproto/... ./internal/macserver/... ./cmd/qdesk-mac/...

      - name: Swift tests
        # Tests that require TCC (Capture/Input/Accessibility) skip via XCTSkipUnless.
        # Health + JSON encoding tests run unconditionally.
        run: cd cmd/qdesk-mac-helper && swift test

      - name: Smoke — initialize + tools/list
        run: |
          out=$(echo '{"jsonrpc":"2.0","id":1,"method":"initialize"}
          {"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./bin/qdesk-mac)
          echo "$out"
          echo "$out" | grep -q '"name":"qdesk-mac"'
          echo "$out" | grep -q 'wechat.screenshot'
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/mac.yml
git commit -m "ci: macos workflow — build + Go/Swift tests + MCP smoke"
```

- [ ] **Step 4: Push and verify CI**

Run: `git push origin <branch>` (or merge to main per project conventions).
Expected: GitHub Actions `mac` workflow runs and goes green.

---

## Task 16: End-to-end smoke (manual) + bugfix pass

**Files:**
- May modify any of the above as bugs are found.

- [ ] **Step 1: Reinstall fresh**

Run:
```bash
./scripts/install-mac.sh
qdesk-mac doctor
```

Re-grant TCC permissions if prompted (helper path may have changed signature).

- [ ] **Step 2: Register MCP server with Claude Code**

Run: `claude mcp add --transport stdio qdesk-mac -- /usr/local/bin/qdesk-mac`
Verify with: `claude mcp list`
Expected: `qdesk-mac` listed.

- [ ] **Step 3: Open WeChat and pin a test conversation (e.g., file transfer to self)**

Manual. Confirm WeChat is launched and main window visible.

- [ ] **Step 4: Drive end-to-end via Claude Code**

In Claude Code:

> Use qdesk-mac to: (1) ensure WeChat is foreground, (2) list my chats,
> (3) open the "File Transfer" / "文件传输助手" chat, (4) click the input
> box, (5) type "hello from qdesk", (6) press return. Then take a
> screenshot and confirm the message is visible.

- [ ] **Step 5: Record results in `examples/wechat-reply.md` if needed**

If actual behavior diverges from the predicted flow, update the example
prose. If a real bug surfaces (typo not registering, AX tree shape
unexpected, etc.), file a small fix and re-run.

- [ ] **Step 6: Commit any fixes**

If fixes are needed, scope each commit narrowly:

```bash
git add <fixed file>
git commit -m "fix(mac): <specific bug>"
```

If no fixes, this task ends with no commit.

---

## Definition of Done

All of the following are true:

- [ ] `make mac-build` produces `bin/qdesk-mac` and `bin/qdesk-mac-helper`.
- [ ] `go test ./internal/macproto/... ./internal/macserver/... ./cmd/qdesk-mac/...` is green.
- [ ] `cd cmd/qdesk-mac-helper && swift test` is green (with TCC-dependent tests skipping cleanly when permissions are absent).
- [ ] `qdesk-mac doctor` reports correct status and opens the right Settings panes when permissions are missing.
- [ ] On a Mac with WeChat open and TCC granted, Claude Code can drive WeChat through the 8 MCP tools end-to-end (manual verification per Task 16).
- [ ] `.github/workflows/mac.yml` is green on `macos-latest`.
- [ ] `README.md` advertises Mac host mode; `examples/wechat-reply.md` walks a new user through the install + first message in ≤ 10 minutes.
