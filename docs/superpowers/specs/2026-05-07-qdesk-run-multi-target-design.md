# `qdesk run` multi-target design

**Date:** 2026-05-07
**Status:** design (user-approved direction; brainstorming complete)
**Author:** jeff (with Claude)

---

## 1. One-liner

Make `qdesk run X.yaml` work against any of three targets — the existing
Linux Docker sandbox, the Mac host (via `qdesk-mac`), and the Windows
host (via `qdesk-win`) — with **the same runner loop, the same trace/
report, the same vision LLM**. v0 ships Mac; Windows follows the
identical pattern in v0.1.

## 2. Why

- The `runner.Run()` agent loop (screenshot → llm.Act → execute → done?)
  is platform-agnostic; only "how to screenshot" and "how to send an
  action" couple it to Docker today.
- Mac and Windows host modes already expose those exact primitives over
  MCP. Adding two ~100-LOC adapters opens up native-app testing — a
  category Playwright/Selenium can't cover.
- CI and prosumer use cases diverge:
  - **CI / automation** want a yaml in their repo + `qdesk run`. Today
    only works for web; should also work for the WeChat reply on Mac,
    the Office macro on Windows, etc.
  - **Assistant integration** keeps using the MCP servers directly via
    Claude Code / Cursor. This proposal does **not** affect that path.

## 3. Scope

### v0 (this spec)

- New `target:` field in TestSpec, valid values: `linux-chrome` (default,
  current behavior renamed) and `mac-host`. `windows-host` is reserved
  but not implemented.
- New `Driver` interface inside `internal/runner` with two impls:
  `DockerDriver` (refactor of the current path) and `MacDriver` (new,
  HTTP-only client of `qdesk-mac --listen`).
- New top-level YAML block `mac:` with at least `endpoint`, `api_key_env`,
  `bundle_id`. The driver activates `bundle_id` on setup and passes it
  as `target_bundle_id` on every action.
- `protocol.Action` stays the same; the Mac driver translates each
  variant into the appropriate `mac.*` MCP tool call.
- `protocol.ActionDrag` is unsupported in host mode — driver returns a
  structured error; the runner records the failure same as any other.

### v0.1 (next)

- `windows-host` target + `windows:` block. Trivially copies the Mac
  pattern (since `windows.*` mirrors `mac.*`).

### Out of scope

- Drag emulation in host mode (would need a new `mac.drag` tool first).
- stdio MCP for the Mac driver — HTTP only.
- Concurrency control across runners hitting the same host (host mode
  is single-tenant by design; if two `qdesk run` invocations target the
  same Mac they'll conflict, but that's the user's problem).
- `setup:` block (precondition checks). Mac driver assumes the target
  app is already running, fails fast if `mac.activate` says it isn't.

## 4. YAML schema (v0)

```yaml
name: "Send WeChat message via qdesk run"
target: mac-host                       # "linux-chrome" (default) | "mac-host"

# Required when target == mac-host.
mac:
  endpoint: http://127.0.0.1:8765      # qdesk-mac --listen URL
  api_key_env: QDESK_MAC_API_KEY       # name of env var holding the bearer key
  bundle_id: com.tencent.xinWeChat     # app to drive — activated on setup

# Existing fields stay; for non-web targets, url/template are ignored.
# template:                            # ignored when target != linux-chrome
# url:                                 # ignored when target != linux-chrome

goal: |
  Open the conversation with "文件传输助手" and send "hi from qdesk run".

expect:
  - The most recent message in the chat reads "hi from qdesk run".

ttl_seconds: 180
max_steps: 12
```

For backward compatibility, missing `target:` defaults to `linux-chrome`
and the existing yaml shape (`template`, `url`) keeps working unchanged.
A `linux-chrome` test with a `mac:` block is rejected at parse time as
"target mismatch".

## 5. Driver interface

```go
// Driver abstracts "spin up a target environment, screenshot it, send
// actions to it, tear it down". One impl per target type.
type Driver interface {
    // Setup acquires the environment and returns a handle. The Run loop
    // calls Setup once before the step loop.
    Setup(ctx context.Context, spec *TestSpec) (DriverSession, error)
}

// DriverSession is one in-flight drive. Closed at the end of Run().
type DriverSession interface {
    Screenshot(ctx context.Context) ([]byte, error)
    Action(ctx context.Context, a *protocol.Action) error
    Close(ctx context.Context) error
}
```

`DockerDriver` keeps current behavior — calls `qdesk-control` to create
a session, hits `qdesk-agentd` for screenshot/action.

`MacDriver` opens an HTTP MCP session against `qdesk-mac --listen`,
calls `mac.activate {bundle_id}` once on Setup, then on each Action call
translates `protocol.Action` → matching `mac.*` MCP tool with
`target_bundle_id` guard. Close is a no-op (the qdesk-mac process keeps
running for next test).

## 6. Action translation (Mac)

| `protocol.Action` | `mac.*` MCP call |
|---|---|
| `Click {X, Y, Button}` | `mac.click {x, y, button, target_bundle_id}` |
| `Type {Text}` | `mac.type {text, target_bundle_id}` |
| `Key {Keys}` | `mac.key {combo: strings.Join(Keys, "+"), target_bundle_id}` |
| `Scroll {X, Y, DX, DY}` | `mac.scroll {x, y, dx, dy, target_bundle_id}` |
| `Drag {From, To}` | error: "drag is not supported on mac-host target in v0" |
| `Wait {MS}` | driver sleeps `MS` ms locally (no remote call) |

## 7. Coordinate system note

The runner's vision LLM today is prompted for "logical pixel
coordinates" of a 1024×768 Chromium viewport. On Mac host mode, the
screenshot dims will be the **user's full Mac display in logical
points** — possibly 1920×1200, 2560×1440, etc. The vision LLM handles
this fine since the prompt is "click on the X you see"; coords are
relative to whatever PNG was attached, and `mac.click` uses the same
logical-points space. **No prompt change needed.**

## 8. Trace + report

`Trace.Steps[].Screenshots` keep working the same way — they're just
PNGs. The HTML report doesn't care where the PNG came from. The trace
gets a new `target` field so post-hoc analysis can tell apart runs.

## 9. CLI / discovery

`qdesk run X.yaml` keeps the same flags. New optional flag:
`--mac-endpoint http://...` overrides yaml `mac.endpoint` (handy for
CI where the URL is a per-runner secret).

If `target: mac-host` and the yaml's `api_key_env` env var is unset,
the runner errors out before driver setup with a clear message. Same
shape as current `--api-key` handling.

## 10. Failure modes & honesty

- `mac.activate` fails (app not running) → Setup returns error, Run
  trace status = error, diagnosis = "<bundle_id> not running".
- `mac.click` returns `IsError=true` (e.g. foreground guard mismatch) →
  driver wraps as a Go error; runner step records `Failure: ...`.
- HTTP unreachable → driver returns connection error; ditto.

## 11. Testing

- **Unit:** `MacDriver` tests against a fake HTTP server that records
  the JSON-RPC bodies it received and returns canned tool results. No
  real Mac needed in CI.
- **E2E:** one new yaml `examples/mac-wechat-run.qdesk.yaml` actually
  exercises a real WeChat. Documented as "requires Mac + qdesk-mac
  running"; not run in default CI (gated by env var).

## 12. Implementation order

1. Refactor `runner.Run()` to use a `Driver` interface; existing path
   becomes `DockerDriver`. Tests still pass.
2. Add `target:` field + parser validation. Default = `linux-chrome`.
3. Add `MacDriver` + JSON-RPC client (8 methods we need); fake-HTTP
   tests.
4. Wire the CLI flag override + env-var resolution.
5. Manual E2E once against the user's WeChat (smoke).
6. Document in `docs/agents/host-mode-tools.md` (cross-link `qdesk run`
   path).

Each step is one TDD commit. Total ~600 LOC + tests.
