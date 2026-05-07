# qdesk host-mode tools — AI agent reference

**Audience:** the LLM driving an MCP client (Claude Code, Cursor, your own
agent loop). Skim once before your first call, then refer to the tool
table when picking actions.

> **TL;DR:** `qdesk-mac` and `qdesk-win` give you 8 mirror-image tools per
> platform to drive the user's GUI: `front_app`, `activate`, `screenshot`,
> `click`, `type`, `key`, `scroll`, `clipboard_paste`. You see pixels, you
> send coordinates, you compose actions. The server doesn't know what's
> on screen — that's your job via vision.

---

## When to use which

| You want to drive… | Use |
|---|---|
| A native app on the user's macOS (WeChat, Safari, Finder, Xcode) | `mac.*` — see `qdesk-mac` |
| A native app on a remote Windows host (Notepad, Office, an internal CRM) | `windows.*` — see `qdesk-win` |
| Throwaway Linux + Chromium per session, multi-tenant | `qdesk` Linux Docker sandbox (separate doc — `SKILL.md`) |

Mac mode and Windows mode are **single-tenant host-mode**: one user's
machine, one foreground app at a time. The tool surface is intentionally
symmetric so a single prompt can target either.

## The agent loop (canonical pattern)

```
loop:
  screenshot                       # pixels in
  ── if uncertain about state ──
  front_app                        # who's in front?
  ── pick next action ──
  activate <target> if needed      # bring target app to front
  click / type / key / scroll      # drive the UI
  ── verify ──
  screenshot                       # pixels out — did the action take effect?
  if goal met: stop
```

**Every action is best-effort.** The server tells you the truth — when
`actually_foreground=false` (Windows refused focus steal) or
`clipboard_restored=false` (clipboard backup failed) — but it does NOT
verify the user-visible outcome. Verification is your job via screenshot.

## Coordinate systems (CRITICAL)

| Platform | Coordinate space | What screenshot dimensions are |
|---|---|---|
| `mac.*` | **logical points** (Retina-aware, pre-scaling) | logical points |
| `windows.*` | **physical pixels** (PerMonitorV2 DPI-aware) | physical pixels |

In both cases **the click coordinate space matches the screenshot
dimensions** — measure on the PNG you got back, send those numbers. Don't
multiply or divide by anything.

## ASCII vs non-ASCII text

`mac.type` and `windows.type` accept any UTF-8. Internally:

- **Pure ASCII** (every rune ≤ 0x7F) → synthetic keyboard events
  (`CGEventKeyboardSetUnicodeString` on Mac; `SendInput KEYEVENTF_UNICODE`
  on Windows). Fast, no clipboard side-effect.
- **Anything non-ASCII** (Chinese, Japanese, emoji, em-dash, smart quotes,
  …) → automatically routed through `clipboard_paste`: copies text to the
  system clipboard, sends ⌘V / Ctrl+V, then restores the original
  clipboard. Adds ~150 ms latency.

You don't need to choose — the tool picks the right path. The result text
tells you which path ran (`path=unicode` vs `path=clipboard`).

**Side effect:** the clipboard fallback writes to the system pasteboard
and tries to restore it. If the user had a non-text item (image, file
copy), it cannot be restored — `clipboard_restored=false`. Avoid the
clipboard path during sensitive flows.

## Foreground guards (`expected_exe` / `target_bundle_id`)

Action tools (`click`, `type`, `key`, `scroll`, `clipboard_paste`) accept
an optional foreground guard. If set and the front app's identifier
doesn't match, the tool refuses to act and returns a structured error
naming what's actually in front.

Always pass it once you've activated the target. Without it, a stray
Cmd-Tab or notification can steal focus mid-sequence and your typing
lands in the wrong app.

```jsonc
// Mac
{"name": "mac.type", "arguments": {"text": "hello", "target_bundle_id": "com.tencent.xinWeChat"}}
// Windows
{"name": "windows.type", "arguments": {"text": "hello", "expected_exe": "notepad.exe"}}
```

`activate` and `screenshot` are exempt — `activate` is what you call when
the guard fails, and `screenshot` is always safe.

## Activation: by-name beats vision

The cheapest way to put a known app in front is by name, not by clicking
its dock/taskbar icon:

```jsonc
// Mac — by bundle ID (most reliable)
{"name": "mac.activate", "arguments": {"bundle_id": "com.apple.Safari"}}

// Windows — by exe basename
{"name": "windows.activate", "arguments": {"exe": "notepad.exe"}}
// Or by HWND if you got it from front_app earlier
{"name": "windows.activate", "arguments": {"hwnd": 393316}}
// Or by window title regex (last resort — slower, ambiguous)
{"name": "windows.activate", "arguments": {"title_regex": "Untitled.*Notepad"}}
```

Neither tool **launches** apps. The target must already be running. If
`activate` returns "no window matched", ask the user to launch it.

`windows.activate` returns `actually_foreground` — Windows can refuse
focus steal even when SetForegroundWindow returns success. Always check
the value; if false, retry once or fall back to vision-based clicking on
the taskbar.

## Reading text off the screen

Neither host mode exposes accessibility-tree (UIA on Windows, AX on Mac)
in v1. To read text from the GUI:

1. `screenshot` → PNG
2. Use vision yourself (the LLM you're running on)

Why: AX/UIA was designed for static apps. Modern apps (Electron,
DirectX/Skia rendered, Flutter/SwiftUI without identifiers) expose
nothing useful. The vision path is more uniform and what's already proven
to work — see `feedback_wechat_e2e_findings.md` and
`feedback_windows_e2e_findings.md` for the empirical reasoning.

## Common pitfalls

1. **Sending input to the wrong app.** Always set the foreground guard.
   When in doubt, `screenshot` first to confirm what's in front.

2. **Not verifying the outcome.** "I clicked Save" + no follow-up
   screenshot is a hallucination. Take a second screenshot and
   re-vision.

3. **Coordinate drift on small targets.** Vision models routinely drift
   ±5–10 px on dense UI. For a target smaller than ~30 px, take an
   extra screenshot after clicking and re-localize if the visible state
   didn't change.

4. **Mac: app must be in `Applications` for AX.** If `mac.click` does
   nothing, the user may not have granted Accessibility / Screen
   Recording permissions. Tell them to run `qdesk-mac doctor`.

5. **Windows: must have an active interactive desktop session.** If
   `windows.front_app` returns "no foreground window" or `windows.screenshot`
   fails with "BitBlt failed", the Windows host has no connected RDP /
   console session. Tell the user to RDP in and keep the connection open.
   See README "Windows session isolation".

6. **Windows: `actually_foreground=false`.** Windows refuses focus steal
   from a non-foreground process. Retry `activate` once. If it still
   fails, fall back to vision-clicking the taskbar entry.

7. **Don't drag.** v1 has no `drag` tool on either platform. To move a
   slider or scrollbar, use `scroll` (wheel) or click incrementally. To
   select text, click + shift+click is your only path; sequential
   `click` + `key shift+End` style.

8. **Don't expect the screen to stay still.** The user might be typing
   in another app. If a sequence is critical, take screenshots between
   every action.

9. **Clipboard pollution.** A non-ASCII `type` call momentarily owns the
   clipboard. Don't run during a flow where the user is mid-paste.

## Per-tool reference (compact)

### `mac.front_app` / `windows.front_app`
- Args: `{}`
- Returns: text describing the foreground app's identifier and window
  title.
- Use to: figure out what's in front before deciding next action.
- Cost: cheap (~5 ms).

### `mac.activate` / `windows.activate`
- Mac args: `{bundle_id: "com.tencent.xinWeChat"}`
- Windows args: `{exe?: "notepad.exe", hwnd?: 393316, title_regex?: "..."}`
   (priority hwnd > exe > title_regex; at least one required)
- Returns: success text plus `actually_foreground` on Windows.
- Use to: switch to a known app by identifier (cheaper than clicking).
- Caveats: doesn't launch; doesn't always succeed on Windows
  (`actually_foreground=false`).

### `mac.screenshot` / `windows.screenshot`
- Args: `{}`
- Returns: PNG (base64) + dimensions + foreground exe/title.
- Use to: get pixels. **Always your input for vision.**
- Caveats: full primary screen — includes other apps' windows. Don't
  surface to the end-user without their consent.
- Cost: 30–100 KB PNG; vision call adds ~$0.001–0.003 in tokens.

### `mac.click` / `windows.click`
- Args: `{x, y, button?: "left", clicks?: 1, modifiers?: ["ctrl","shift",...], target_bundle_id?: "...", expected_exe?: "..."}`
   (Mac uses `clicks: 1|2|3` for double/triple click; Windows uses `double: true` for double; modifiers on Windows: "ctrl shift alt win".)
- Returns: success text.
- Use to: click anywhere on the **same coordinate plane** as the
  screenshot.
- Always pass the foreground guard.

### `mac.type` / `windows.type`
- Args: `{text, target_bundle_id?: "..." (Mac) | expected_exe?: "..." (Win)}`
- Returns: text describing path taken (`unicode` or `clipboard`) and
  `clipboard_restored` (when clipboard path).
- Use to: send text at the current focus.
- Caveats: ASCII-detection auto-routes; clipboard side-effect for
  non-ASCII.

### `mac.key` / `windows.key`
- Args: `{combo: "return" | "escape" | "cmd+v" | "ctrl+f" | "win+r" | "alt+tab" | ...}`
- Returns: success text.
- Use to: send named-key sequences and chord combos. Modifier set:
  Mac `cmd / opt / shift / ctrl`; Windows `ctrl / shift / alt / win`.
- ASCII letters and digits work as the main key (`a-z`, `0-9`).

### `mac.scroll` / `windows.scroll`
- Args: `{x, y, dy, dx?: 0, target_bundle_id?: ... | expected_exe?: ...}`
- Returns: success text.
- Use to: wheel-scroll at a point. **Positive dy scrolls up.**
- Caveats: horizontal `dx` is accepted but many apps ignore it.

### `mac.clipboard_paste` / `windows.clipboard_paste`
- Args: `{text, target_bundle_id?: ... | expected_exe?: ...}`
- Returns: text + `clipboard_restored`.
- Use to: explicitly paste text. **Most of the time you don't need
  this** — `type` auto-routes non-ASCII through here.
- When you do need it: pasting very long strings (≥ ~500 chars) is
  faster via clipboard than per-character SendInput.

## Compose actions, don't over-design

- Skip multi-step "click File → Save As" workflows when ⌘S / Ctrl+S
  works. Keys are faster, more reliable, and don't need vision.
- Use `activate` before *any* action that targets a specific app. Don't
  trust that the previous action left the right app in front.
- Use `expected_exe` / `target_bundle_id` aggressively. The cost of a
  false guard error is one extra `activate` call; the cost of an action
  going to the wrong app is silent corruption.

## Cost model (rough, in tokens for Claude Sonnet 4)

| Operation | Tokens (input + output) | Wall time |
|---|---|---|
| 1× screenshot (1024×768 PNG) | ~3 000 input | <100 ms server-side |
| 1× tool_call decision (no vision needed) | ~200 input + 50 output | <1 s |
| 1× tool_call with vision verification | ~3 200 input + 200 output | ~2 s |
| Full agent loop (~5 actions w/ vision) | ~20 000 tokens | 8–15 s |

A real flow ("send 'on my way' to Bob in WeChat") typically runs
8–12 tool calls and ~$0.01–0.03 on Sonnet.

## See also

- `examples/wechat-reply.md` — concrete walk-through of a Mac WeChat
  workflow.
- `docs/superpowers/specs/2026-05-05-mac-host-mode-wechat-design.md` —
  Mac host mode design.
- `docs/superpowers/specs/2026-05-07-windows-host-mode-design.md` —
  Windows host mode design.
- `docs/superpowers/plans/2026-05-07-windows-host-mode.md` — Windows
  host mode implementation plan.
