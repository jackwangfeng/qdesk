# Mac host mode v1.1 (clipboard pivot) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace WeChat 4.x-broken AX-tree path with a clipboard+keyboard path: add a helper RPC `clipboardPaste` that pastes text via `NSPasteboard` + `cmd+v` and restores the prior clipboard, route `wechat.type` non-ASCII through it, rewrite `wechat.open_chat` to drive `cmd+f` + paste + `return`, and remove `wechat.list_chats` (no reliable non-vision substitute on 4.x).

**Architecture:** No structural changes — same Go MCP server + Swift sidecar topology from v1. New helper RPC `clipboardPaste` is one Swift function plus a dispatch case. Go-side tool changes are local to `internal/macserver`.

**Tech Stack:** Go 1.25, Swift 5.9 (macOS 14+), `NSPasteboard`, `CGEvent`. Same as v1.

**Spec:** `docs/superpowers/specs/2026-05-06-mac-host-mode-v1.1-clipboard.md`

**Branching:** Work on a new branch `feat/mac-host-mode-v1.1`.

---

## File Structure

Modified:

```
internal/macproto/rpc.go                                # add MethodClipboardPaste + ClipboardPasteRequest
internal/macproto/rpc_test.go                           # round-trip test for new type
cmd/qdesk-mac-helper/Sources/Helper/main.swift          # add clipboardPaste dispatch case
cmd/qdesk-mac-helper/Sources/Helper/Input.swift         # (no change; reused)
internal/macserver/mcp.go                               # remove wechat.list_chats; update open_chat description
internal/macserver/tools.go                             # toolType non-ASCII branch; toolOpenChat rewrite; remove list_chats
internal/macserver/tools_test.go                        # add type-routing tests; rewrite open_chat tests
README.md                                               # remove list_chats from tool list
examples/wechat-reply.md                                # tighten the now-canonical flow
```

Created:

```
cmd/qdesk-mac-helper/Sources/Helper/Clipboard.swift     # NSPasteboard backup + setString + cmd+v + restore
cmd/qdesk-mac-helper/Tests/HelperTests/ClipboardTests.swift   # backup/restore round-trip
```

Deleted:

```
internal/macserver/chats.go                             # AX-route logic dies with list_chats
internal/macserver/chats_test.go                        # tests for the deleted code
```

---

## Task 1: macproto — `ClipboardPasteRequest` + `MethodClipboardPaste`

**Files:**
- Modify: `internal/macproto/rpc.go`
- Modify: `internal/macproto/rpc_test.go`

- [ ] **Step 1: Append the failing test**

Append to `internal/macproto/rpc_test.go`:

```go
func TestClipboardPasteRequestRoundTrip(t *testing.T) {
	in := ClipboardPasteRequest{Text: "你好 hello"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ClipboardPasteRequest
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", out, in)
	}
}

func TestMethodClipboardPasteString(t *testing.T) {
	if MethodClipboardPaste != "clipboardPaste" {
		t.Errorf("MethodClipboardPaste must be %q so the Swift helper can match it", "clipboardPaste")
	}
}
```

- [ ] **Step 2: Run, confirm fails**

Run: `go test ./internal/macproto/... -v -run "TestClipboardPaste|TestMethodClipboardPaste"`
Expected: FAIL — symbol not defined.

- [ ] **Step 3: Add the new method constant + type to `internal/macproto/rpc.go`**

In the method-name block (after `MethodAXClick`):

```go
	MethodAXClick        = "axClick"
	MethodClipboardPaste = "clipboardPaste"
)
```

After the existing `AXClickRequest` definition, add:

```go
// ClipboardPasteRequest tells the helper to set the system pasteboard to
// `text`, post a cmd+v keyboard event, wait briefly for the paste to
// register, then restore the prior pasteboard contents.
//
// This is the v1.1 fallback for input that CGEvent unicode-mode cannot
// deliver to WeChat (Chinese, emoji, etc.). It pollutes the user's
// clipboard for ~150ms; the helper restores it before returning.
type ClipboardPasteRequest struct {
	Text string `json:"text"`
}
```

- [ ] **Step 4: Run, confirm pass + vet clean**

Run: `go test ./internal/macproto/... -v && go vet ./internal/macproto/...`
Expected: PASS for new tests + earlier ones; vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/macproto/
git commit -m "feat(mac): macproto — ClipboardPasteRequest + MethodClipboardPaste"
```

---

## Task 2: Swift helper — `Clipboard.swift` with backup/paste/restore

**Files:**
- Create: `cmd/qdesk-mac-helper/Sources/Helper/Clipboard.swift`
- Modify: `cmd/qdesk-mac-helper/Sources/Helper/main.swift`
- Create: `cmd/qdesk-mac-helper/Tests/HelperTests/ClipboardTests.swift`

- [ ] **Step 1: Write failing test that backup+set+restore round-trips**

`Tests/HelperTests/ClipboardTests.swift`:

```swift
import XCTest
import AppKit
@testable import Helper

final class ClipboardTests: XCTestCase {
    func testBackupAndRestoreString() {
        // Seed the pasteboard with a known string.
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString("seed-value", forType: .string)

        // Backup, then mutate, then restore.
        let backup = backupPasteboard()
        pb.clearContents()
        pb.setString("transient", forType: .string)
        XCTAssertEqual(pb.string(forType: .string), "transient")
        restorePasteboard(backup)
        XCTAssertEqual(pb.string(forType: .string), "seed-value")
    }

    func testClipboardPasteRestoresOriginal() throws {
        let h = health()
        try XCTSkipUnless(h.accessibilityGranted, "Accessibility not granted; skipping (cmd+v post will fail without it)")

        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString("original", forType: .string)

        // clipboardPaste does NSPasteboard write + cmd+v + sleep + restore.
        // No app has focus that will accept the paste, but the function
        // must still complete and restore the original.
        try clipboardPaste(text: "transient-payload")

        XCTAssertEqual(pb.string(forType: .string), "original",
                       "clipboard not restored to original")
    }
}
```

- [ ] **Step 2: Run, confirm fails**

Run: `cd cmd/qdesk-mac-helper && swift test --filter ClipboardTests`
Expected: FAIL — symbols undefined.

- [ ] **Step 3: Implement `Sources/Helper/Clipboard.swift`**

```swift
import Foundation
import AppKit
import CoreGraphics

/// PasteboardBackup holds the string contents of the pasteboard and the
/// changeCount at the time of capture. Non-string items (images, files)
/// are not preserved by v1.1; document this in the README.
struct PasteboardBackup {
    let string: String?
    let changeCount: Int
}

func backupPasteboard() -> PasteboardBackup {
    let pb = NSPasteboard.general
    return PasteboardBackup(
        string: pb.string(forType: .string),
        changeCount: pb.changeCount
    )
}

func restorePasteboard(_ backup: PasteboardBackup) {
    let pb = NSPasteboard.general
    pb.clearContents()
    if let s = backup.string {
        pb.setString(s, forType: .string)
    }
}

/// clipboardPaste:
///   1. Backup pasteboard.
///   2. Write `text` to pasteboard.
///   3. Post cmd+v to the focused app.
///   4. Wait 150 ms for the paste to consume the pasteboard.
///   5. Restore the original pasteboard contents.
///
/// The active app must accept paste. Caller is responsible for foreground
/// guard (Go side does this).
func clipboardPaste(text: String) throws {
    let backup = backupPasteboard()

    let pb = NSPasteboard.general
    pb.clearContents()
    pb.setString(text, forType: .string)

    // cmd+v: keycode 0x09 ('v') with Command modifier.
    let cmdV: CGKeyCode = 0x09
    guard let down = CGEvent(keyboardEventSource: nil, virtualKey: cmdV, keyDown: true),
          let up = CGEvent(keyboardEventSource: nil, virtualKey: cmdV, keyDown: false)
    else {
        restorePasteboard(backup)
        throw HelperRPCError(code: "internal", message: "create cmd+v CGEvent failed")
    }
    down.flags = .maskCommand
    up.flags = .maskCommand
    down.post(tap: .cghidEventTap)
    up.post(tap: .cghidEventTap)

    // Let the focused app consume the paste before we overwrite the
    // pasteboard. 150 ms is empirical — long enough for WeChat's input
    // box, short enough to feel instant.
    Thread.sleep(forTimeInterval: 0.15)

    restorePasteboard(backup)
}
```

- [ ] **Step 4: Wire `clipboardPaste` into `main.swift` dispatch**

In the `switch req.method { ... }` in `dispatch(_:)`, add a new case before `default:`:

```swift
case "clipboardPaste":
    do {
        struct P: Decodable { let text: String }
        let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
        try clipboardPaste(text: p.text)
        writeOK(id: req.id)
    } catch let e as HelperRPCError {
        writeError(id: req.id, code: e.code, message: e.message)
    } catch {
        writeError(id: req.id, code: "internal", message: "\(error)")
    }
```

- [ ] **Step 5: Run Swift tests**

Run: `cd cmd/qdesk-mac-helper && swift test`
Expected: all tests pass; `ClipboardTests` runs (Accessibility is granted on this machine), `CaptureTests` skips.

- [ ] **Step 6: Manual smoke**

Run from repo root:

```bash
make mac-build
# Set the system clipboard to a known sentinel via pbcopy.
printf 'pre-v1.1' | pbcopy
# Drive the helper.
echo '{"id":1,"method":"clipboardPaste","params":{"text":"sandbox-test-中文"}}' | ./bin/qdesk-mac-helper
# After helper exits, verify clipboard restored.
pbpaste
```

Expected: helper prints `{"id":1,"result":{"ok":true}}`. `pbpaste` prints `pre-v1.1`. (The cmd+v will land somewhere — possibly Terminal — but pasteboard is restored either way.)

- [ ] **Step 7: Commit**

```bash
git add cmd/qdesk-mac-helper/
git commit -m "feat(mac-helper): clipboardPaste — backup + setString + cmd+v + restore"
```

---

## Task 3: Go `toolType` — non-ASCII fallback to `clipboardPaste`

**Files:**
- Modify: `internal/macserver/tools.go`
- Modify: `internal/macserver/tools_test.go`

- [ ] **Step 1: Append failing tests**

Append to `internal/macserver/tools_test.go`:

```go
func TestTypeRoutesASCIIThroughCGEventType(t *testing.T) {
	gotType := false
	gotPaste := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodType, func(_ json.RawMessage) (json.RawMessage, error) {
			gotType = true
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodClipboardPaste, func(_ json.RawMessage) (json.RawMessage, error) {
			gotPaste = true
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "wechat.type",
		json.RawMessage(`{"text":"hello world"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !gotType {
		t.Errorf("ASCII text should route through MethodType")
	}
	if gotPaste {
		t.Errorf("ASCII text should NOT trigger clipboardPaste")
	}
}

func TestTypeRoutesNonASCIIThroughClipboardPaste(t *testing.T) {
	var pasteText string
	gotType := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodType, func(_ json.RawMessage) (json.RawMessage, error) {
			gotType = true
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodClipboardPaste, func(p json.RawMessage) (json.RawMessage, error) {
			var v struct{ Text string }
			_ = json.Unmarshal(p, &v)
			pasteText = v.Text
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "wechat.type",
		json.RawMessage(`{"text":"你好"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotType {
		t.Errorf("non-ASCII text should NOT route through MethodType")
	}
	if pasteText != "你好" {
		t.Errorf("clipboardPaste payload mismatch: got=%q want=%q", pasteText, "你好")
	}
}
```

- [ ] **Step 2: Run, confirm fails**

Run: `go test ./internal/macserver/... -v -run "TestTypeRoutes"`
Expected: FAIL — both tests assert routing behavior that doesn't exist yet (the current `toolType` always uses MethodType).

- [ ] **Step 3: Modify `toolType` in `internal/macserver/tools.go`**

Replace the entire existing `toolType` function:

```go
func (s *MCPServer) toolType(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ Text string }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if isASCII(in.Text) {
		body, _ := json.Marshal(macproto.TypeRequest{Text: in.Text})
		if _, err := s.helper.Call(ctx, macproto.MethodType, body); err != nil {
			return errToolResult(err), nil
		}
		return &ToolResult{Content: []ContentItem{{Type: "text",
			Text: fmt.Sprintf("typed %d characters (CGEvent unicode)", len([]rune(in.Text)))}}}, nil
	}
	// Non-ASCII: WeChat's IME drops CGEvent unicode chars; route through
	// the helper's clipboard-paste path which restores the prior clipboard.
	body, _ := json.Marshal(macproto.ClipboardPasteRequest{Text: in.Text})
	if _, err := s.helper.Call(ctx, macproto.MethodClipboardPaste, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("pasted %d characters (clipboard fallback for non-ASCII)", len([]rune(in.Text)))}}}, nil
}

// isASCII returns true iff every rune in s is in 0x00-0x7F.
func isASCII(s string) bool {
	for _, r := range s {
		if r > 0x7F {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `go test ./internal/macserver/... -v`
Expected: all tests pass — including the two new routing tests AND the existing `TestTypeSendsText` (which uses `"你好"` — verify it still passes; if it asserts MethodType was hit it will fail and need updating).

If `TestTypeSendsText` breaks (because it asserts MethodType received the Chinese payload but now MethodClipboardPaste does), update it to expect MethodClipboardPaste:

```go
func TestTypeSendsText(t *testing.T) {
	var captured json.RawMessage
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodClipboardPaste, func(p json.RawMessage) (json.RawMessage, error) {
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
```

- [ ] **Step 5: Re-run, confirm all green**

Run: `go test ./internal/macserver/... -v`
Expected: every test passes.

- [ ] **Step 6: Commit**

```bash
git add internal/macserver/tools.go internal/macserver/tools_test.go
git commit -m "feat(mac): wechat.type — non-ASCII falls back to clipboardPaste"
```

---

## Task 4: Go `toolOpenChat` — rewrite to `cmd+f` + paste + return

**Files:**
- Modify: `internal/macserver/tools.go` (move `toolOpenChat` here from `chats.go`; we delete `chats.go` in Task 5)
- Modify: `internal/macserver/tools_test.go` (replace old AX-based open_chat tests)

- [ ] **Step 1: Rewrite the failing tests for the cmd+f flow**

In `internal/macserver/tools_test.go`, REMOVE any existing `TestOpenChat*` tests (they were in `chats_test.go`; if any remain in `tools_test.go` from earlier edits, drop them). Then ADD:

```go
func TestOpenChatSequencesCmdFAndPasteAndReturn(t *testing.T) {
	type call struct {
		method string
		params string
	}
	var calls []call
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		record := func(method string) func(json.RawMessage) (json.RawMessage, error) {
			return func(p json.RawMessage) (json.RawMessage, error) {
				calls = append(calls, call{method, string(p)})
				return json.RawMessage(`{"ok":true}`), nil
			}
		}
		f.SetHandler(macproto.MethodKey, record(macproto.MethodKey))
		f.SetHandler(macproto.MethodClipboardPaste, record(macproto.MethodClipboardPaste))
		f.SetHandler(macproto.MethodType, record(macproto.MethodType))
	})
	if _, err := srv.callTool(context.Background(), "wechat.open_chat",
		json.RawMessage(`{"name":"文件传输助手"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 helper calls (cmd+f, paste, return); got %d: %+v", len(calls), calls)
	}
	if calls[0].method != macproto.MethodKey || !strings.Contains(calls[0].params, "cmd+f") {
		t.Errorf("call 1 should be key cmd+f; got %+v", calls[0])
	}
	if calls[1].method != macproto.MethodClipboardPaste || !strings.Contains(calls[1].params, "文件传输助手") {
		t.Errorf("call 2 should be clipboardPaste with name; got %+v", calls[1])
	}
	if calls[2].method != macproto.MethodKey || !strings.Contains(calls[2].params, "return") {
		t.Errorf("call 3 should be key return; got %+v", calls[2])
	}
}

func TestOpenChatASCIINameUsesDirectType(t *testing.T) {
	usedPaste := false
	usedType := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodKey, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodType, func(_ json.RawMessage) (json.RawMessage, error) {
			usedType = true
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodClipboardPaste, func(_ json.RawMessage) (json.RawMessage, error) {
			usedPaste = true
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "wechat.open_chat",
		json.RawMessage(`{"name":"hello"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !usedType {
		t.Errorf("ASCII chat name should use MethodType")
	}
	if usedPaste {
		t.Errorf("ASCII chat name should NOT use clipboardPaste")
	}
}

func TestOpenChatRequiresName(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.open_chat",
		json.RawMessage(`{"name":""}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.IsError {
		t.Errorf("empty name must return isError; got %+v", out)
	}
}
```

- [ ] **Step 2: Run, confirm fails**

Run: `go test ./internal/macserver/... -v -run "TestOpenChat"`
Expected: FAIL — current `toolOpenChat` (in `chats.go`) uses AX route, doesn't issue these calls.

- [ ] **Step 3: Add the new `toolOpenChat` to `internal/macserver/tools.go`**

Append to `tools.go` (we delete the old `toolOpenChat` from `chats.go` in Task 5):

```go
// toolOpenChat opens a WeChat conversation by name via the keyboard
// search bar (cmd+f). Bypasses the Accessibility tree, which WeChat 4.x
// no longer exposes for the chat sidebar.
//
// Sequence:
//   1. key cmd+f                         (open WeChat search; ~300ms to render)
//   2. type / clipboardPaste <name>      (search filters live)
//   3. key return                        (open the top match)
//
// We do NOT verify which chat actually opened — WeChat's own search
// matching is opaque. The LLM should call wechat.screenshot afterward
// to confirm the right chat is in front.
func (s *MCPServer) toolOpenChat(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ Name string }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if in.Name == "" {
		return errToolResult(fmt.Errorf("name is required")), nil
	}

	// 1. cmd+f
	cmdfBody, _ := json.Marshal(macproto.KeyRequest{Combo: "cmd+f"})
	if _, err := s.helper.Call(ctx, macproto.MethodKey, cmdfBody); err != nil {
		return errToolResult(err), nil
	}
	time.Sleep(300 * time.Millisecond)

	// 2. type the name (route Chinese through clipboardPaste)
	if isASCII(in.Name) {
		body, _ := json.Marshal(macproto.TypeRequest{Text: in.Name})
		if _, err := s.helper.Call(ctx, macproto.MethodType, body); err != nil {
			return errToolResult(err), nil
		}
	} else {
		body, _ := json.Marshal(macproto.ClipboardPasteRequest{Text: in.Name})
		if _, err := s.helper.Call(ctx, macproto.MethodClipboardPaste, body); err != nil {
			return errToolResult(err), nil
		}
	}
	time.Sleep(200 * time.Millisecond)

	// 3. return to open the top result
	retBody, _ := json.Marshal(macproto.KeyRequest{Combo: "return"})
	if _, err := s.helper.Call(ctx, macproto.MethodKey, retBody); err != nil {
		return errToolResult(err), nil
	}

	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("issued cmd+f / paste %q / return — verify with wechat.screenshot which chat actually opened", in.Name)}}}, nil
}
```

This requires adding `"time"` to the import block of `tools.go`. If it's not there, add it.

- [ ] **Step 4: Run, confirm pass (the three new tests)**

Run: `go test ./internal/macserver/... -v -run "TestOpenChat"`
Expected: PASS for the three new `TestOpenChat*` tests. The OLD `chats_test.go` tests will still be running and FAILING (they check AX-based behavior we no longer implement) — we delete them in Task 5.

- [ ] **Step 5: Commit**

```bash
git add internal/macserver/tools.go internal/macserver/tools_test.go
git commit -m "feat(mac): wechat.open_chat — cmd+f + paste + return (replaces AX route)"
```

(Note: package may not compile cleanly here because `chats.go` still defines `toolOpenChat` — duplicate. Task 5 deletes it. If you can't commit Task 4 without Task 5, COMBINE the commits and skip this commit step.)

**If duplicate-symbol error blocks commit:** proceed straight to Task 5 first, then commit Tasks 4+5 together with the message:

```bash
git add internal/macserver/tools.go internal/macserver/tools_test.go internal/macserver/chats.go internal/macserver/chats_test.go internal/macserver/mcp.go
git commit -m "feat(mac): wechat.open_chat — cmd+f + paste + return; remove wechat.list_chats"
```

---

## Task 5: Remove `wechat.list_chats` and the AX chats code

**Files:**
- Delete: `internal/macserver/chats.go`
- Delete: `internal/macserver/chats_test.go`
- Modify: `internal/macserver/mcp.go` (drop the tool def + bundle ID const stays)
- Modify: `internal/macserver/tools.go` (drop `wechat.list_chats` switch case)

- [ ] **Step 1: Delete the chats files**

```bash
rm internal/macserver/chats.go internal/macserver/chats_test.go
```

- [ ] **Step 2: Drop `wechat.list_chats` from `tools()` in `internal/macserver/mcp.go`**

Find the entry:

```go
		{
			Name:        "wechat.list_chats",
			Description: "Return WeChat sidebar chat list as structured JSON: name, unread_count, last_msg_preview. Uses the Accessibility API — no vision needed.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
```

DELETE it entirely.

Also update `wechat.open_chat`'s description to match the new behavior. Find:

```go
			Description: "Open the conversation with the given chat name. Uses fuzzy matching on the sidebar (exact > prefix > substring). Returns chat-not-found if no sidebar match — v1 has no automatic cmd+f search fallback; the caller can compose that manually with wechat.key + wechat.type if needed.",
```

REPLACE with:

```go
			Description: "Open the conversation with the given chat name. Drives WeChat's own search bar (cmd+f) — does not depend on the Accessibility tree, which WeChat 4.x no longer exposes for the sidebar. Does not guarantee the right chat opens; verify with wechat.screenshot.",
```

- [ ] **Step 3: Drop `wechat.list_chats` from the dispatch in `internal/macserver/tools.go`**

In `callTool`, delete this block:

```go
	case "wechat.list_chats":
		if err := requireWeChatForeground(ctx, s.helper); err != nil {
			return errToolResult(err), nil
		}
		return s.toolListChats(ctx)
```

- [ ] **Step 4: Update `TestToolsListIncludesExpectedTools` in `mcp_test.go`**

In `internal/macserver/mcp_test.go`, find the `for _, want := range []string{ ... }` block in `TestToolsListIncludesExpectedTools`. REMOVE `"wechat.list_chats"` from the slice. The remaining list:

```go
	for _, want := range []string{
		"wechat.screenshot", "wechat.click", "wechat.type", "wechat.key",
		"wechat.scroll", "wechat.ensure_foreground", "wechat.open_chat",
	} {
```

Also add an explicit anti-test that `list_chats` is GONE:

```go
func TestToolsListDoesNotIncludeListChats(t *testing.T) {
	srv := NewMCPServer(NewFakeSupervisor())
	resp := srv.Handle(context.Background(), &RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	})
	b, _ := json.Marshal(resp.Result)
	if strings.Contains(string(b), "wechat.list_chats") {
		t.Errorf("wechat.list_chats was removed in v1.1; it must not appear in tools/list")
	}
}
```

- [ ] **Step 5: Build + run all tests**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: all clean. The `TestOpenChat*` tests from Task 4 still pass; the new `TestToolsListDoesNotIncludeListChats` passes; nothing references `chats.go` symbols anymore.

If anything fails to compile (orphaned imports in `tools.go`, missing `time` import, etc.), fix the imports before committing.

- [ ] **Step 6: Manual smoke**

```bash
make mac-build
printf '%s\n%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize"}' '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./bin/qdesk-mac | grep -o '"name":"[^"]*"' | sort -u
```

Expected output (one line per unique tool name plus serverInfo):
```
"name":"qdesk-mac"
"name":"wechat.click"
"name":"wechat.ensure_foreground"
"name":"wechat.key"
"name":"wechat.open_chat"
"name":"wechat.screenshot"
"name":"wechat.scroll"
"name":"wechat.type"
```

Confirm `wechat.list_chats` is absent.

- [ ] **Step 7: Commit**

```bash
git add internal/macserver/
git commit -m "refactor(mac): remove wechat.list_chats — no reliable non-vision substitute on WeChat 4.x"
```

(If you combined Tasks 4+5 because of duplicate-symbol issues, skip this commit and use the combined message from Task 4.)

---

## Task 6: Docs — README + example reflect v1.1 surface

**Files:**
- Modify: `README.md`
- Modify: `examples/wechat-reply.md`

- [ ] **Step 1: Update the tool list in `README.md`**

Find the existing line (in the Mac host mode section):

```markdown
The MCP tools live under `wechat.*`: `screenshot`, `click`, `type`, `key`,
`scroll`, `ensure_foreground`, `list_chats`, `open_chat`. See
[`examples/wechat-reply.md`](./examples/wechat-reply.md).
```

REPLACE with:

```markdown
The MCP tools live under `wechat.*`: `screenshot`, `click`, `type`, `key`,
`scroll`, `ensure_foreground`, `open_chat`. `wechat.type` automatically
falls back to clipboard paste for non-ASCII text. See
[`examples/wechat-reply.md`](./examples/wechat-reply.md).
```

- [ ] **Step 2: Tighten `examples/wechat-reply.md`**

The v1 example doc has a section called "Replying to a chat" with a "3a/3b" branch and a heavy disclaimer about list_chats. With v1.1 the flow is uniform — there is no AX route to fall back to. Replace the entire "Replying to a chat" section (currently lines 24-65 or so; verify line numbers when editing) with:

```markdown
## Replying to a chat

Open WeChat and log in. Then in Claude Code:

> Use qdesk-mac. Send "晚点到，10分钟" to 张三 in WeChat.

Claude will typically:

1. `wechat.ensure_foreground` — bring WeChat to front.
2. `wechat.open_chat` with `{"name": "张三"}` — drives the cmd+f
   search bar; matches whatever WeChat itself surfaces as the top hit.
3. `wechat.screenshot` — verify the right chat opened (the only
   reliable check; WeChat's search may pick the wrong contact for
   ambiguous names).
4. `wechat.click` on the input box, then `wechat.type` "晚点到，10分钟".
   The type call automatically uses the clipboard fallback for
   non-ASCII text.
5. `wechat.key` `{"combo": "return"}` — send.

If the screenshot in step 3 shows the wrong chat, Claude can press
`escape` and retry with a more specific name.
```

Also remove the now-stale `## Limitations` bullets about `list_chats` and Chinese typing — both are gone in v1.1. Replace the `## Limitations (v1)` section with:

```markdown
## Limitations (v1.1)

- Single account only (whichever WeChat is currently logged in).
- Does not auto-launch WeChat — you must start it manually first.
- Screenshots include your full desktop. Don't run on a screen with
  sensitive windows you don't want the model to see.
- `wechat.open_chat` does not verify which chat actually opened —
  Claude must check with `wechat.screenshot` before sending anything.
- Clipboard fallback for non-ASCII text temporarily replaces your
  pasteboard contents (~150ms) and restores them. Non-string clipboard
  items (images, files) are NOT preserved — they will be replaced by
  empty contents after a non-ASCII type call.
```

- [ ] **Step 3: Commit**

```bash
git add README.md examples/wechat-reply.md
git commit -m "docs(mac): v1.1 — drop list_chats, document open_chat cmd+f flow + clipboard caveats"
```

---

## Task 7: E2E rerun (manual)

**No file changes. Verify the v1.1 flow against real WeChat.**

- [ ] **Step 1: Reinstall**

```bash
./scripts/install-mac.sh
```

- [ ] **Step 2: Verify TCC perms**

```bash
qdesk-mac doctor
```

Accessibility must still be granted to `/usr/local/bin/qdesk-mac-helper`. Screen Recording is optional (only `wechat.screenshot` needs it).

- [ ] **Step 3: With WeChat open + logged in, drive a real send via the helper**

Bundle the whole sequence into one helper invocation (so iTerm doesn't reclaim focus between calls):

```bash
NAME="文件传输助手"
MSG="qdesk v1.1 — clipboard path"

# Start helper, feed the JSON-RPC sequence.
{
  echo '{"id":1,"method":"activate","params":{"bundleId":"com.tencent.xinWeChat"}}'
  sleep 0.6
  echo '{"id":2,"method":"key","params":{"combo":"cmd+f"}}'
  sleep 0.4
  printf '{"id":3,"method":"clipboardPaste","params":{"text":"%s"}}\n' "$NAME"
  sleep 0.3
  echo '{"id":4,"method":"key","params":{"combo":"return"}}'
  sleep 0.8
  printf '{"id":5,"method":"clipboardPaste","params":{"text":"%s"}}\n' "$MSG"
  sleep 0.3
  echo '{"id":6,"method":"key","params":{"combo":"return"}}'
} | ./bin/qdesk-mac-helper
```

Expected: 6 lines of `{"id":N,"result":{"ok":true}}`. Open WeChat — File Transfer Assistant should now contain the message.

- [ ] **Step 4: Verify clipboard restored**

```bash
pbpaste
```

Expected: whatever was on the clipboard before step 3 (NOT the message text).

- [ ] **Step 5: If anything failed, file a focused commit**

If something went wrong (helper crash, message not sent, clipboard not restored), gather the symptom + likely root cause and either:

- File a fix commit on this branch (small, named `fix(mac): <what>`).
- Or, if it's a deeper redesign, capture in a v1.2 spec stub at `docs/superpowers/specs/`.

If it worked, no commit.

- [ ] **Step 6: Final all-tests run**

```bash
go test ./...
cd cmd/qdesk-mac-helper && swift test
```

Both green.

---

## Definition of Done

- [ ] `make mac-build` succeeds.
- [ ] `go test ./internal/macproto/... ./internal/macserver/... ./cmd/qdesk-mac/...` is green.
- [ ] `cd cmd/qdesk-mac-helper && swift test` is green (CaptureTests skip OK without Screen Recording).
- [ ] On a Mac with WeChat 4.x open and logged in, the bundled helper sequence in Task 7 sends a message to File Transfer Assistant and the clipboard is restored.
- [ ] `wechat.list_chats` no longer appears in `tools/list`.
- [ ] `README.md` and `examples/wechat-reply.md` describe the v1.1 surface accurately (no stale references to AX-based list_chats or to Chinese-via-CGEvent).
