# qdesk

**Open-source AI agent desktop sandbox.**

Give any AI agent a clean Linux + Chromium environment over HTTP. Take screenshots, send clicks/keys, install apps, watch the screen. Self-hostable, MCP-native, ~$0.005 per agent action with a vision LLM.

Use it for **testing, web automation, RPA, agent training, demos, scraping, computer-use evaluation** — anywhere an AI needs a real computer to drive.

> 👉 **AI assistants (Claude Code, Cursor, Aider, …)** — qdesk ships an **[MCP server](.claude/mcp-install.md)** with 4 tools you can call directly. See [`SKILL.md`](./SKILL.md). Editing this codebase? Read **[`AGENTS.md`](./AGENTS.md)**.

---

## What it gives you

```
┌──────────────────────────────────────────────────────────┐
│  AI agent (Claude / Gemini / GPT / your own loop)         │
└──────────────────────┬───────────────────────────────────┘
                       │ HTTPS — JSON actions
                       ▼
┌──────────────────────────────────────────────────────────┐
│  qdesk-control  (multi-session, SQLite, bearer auth)      │
│      └─ POST /v1/sessions  → spin up a sandbox            │
│      └─ GET  .../screenshot                                │
│      └─ POST .../actions  {click,type,key,scroll,drag}    │
└──────────────────────┬───────────────────────────────────┘
                       │ Docker
                       ▼
┌──────────────────────────────────────────────────────────┐
│  qdesk/ubuntu-chrome:dev                                   │
│  ┌────────────────────────────────────────────────────┐  │
│  │ Xvfb (virtual display)                              │  │
│  │ xfwm4 (window manager)                              │  │
│  │ Chromium                                             │  │
│  │ qdesk-agentd (HTTP daemon, /screenshot /actions)    │  │
│  └────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

A single sandbox boots in **~1 second** (Docker), responds to HTTP, returns
real PNG screenshots and accepts real input events. **No app instrumentation
needed** — the sandbox doesn't know or care what's running inside.

---

## Mac host mode (alpha) — control your local WeChat

In addition to the Linux Docker sandbox, qdesk now ships a **Mac host mode**
for AI assistants to drive native macOS apps. v1 targets WeChat.

```bash
./scripts/install-mac.sh
qdesk-mac doctor   # grants Screen Recording + Accessibility
claude mcp add --transport stdio qdesk-mac -- /usr/local/bin/qdesk-mac
```

The MCP tools live under `wechat.*`: `screenshot`, `click`, `type`, `key`,
`scroll`, `ensure_foreground`, `open_chat`. `wechat.type` automatically
falls back to clipboard paste for non-ASCII text. See
[`examples/wechat-reply.md`](./examples/wechat-reply.md).

**v1 limitations:** macOS 14+, single WeChat instance, action calls
require WeChat to be the foreground app, screenshots are full-screen
(includes other apps' windows). No code signing — TCC may re-prompt
after rebuild.

### Remote mode (HTTP transport)

Run `qdesk-mac` as an HTTP server so a client on another machine can
drive your Mac's WeChat. Same MCP JSON-RPC dispatch, just over HTTP.

```bash
export QDESK_MAC_API_KEY=$(openssl rand -hex 32)
qdesk-mac --listen 127.0.0.1:8765 --api-key "$QDESK_MAC_API_KEY"
# In another shell or another machine (with --listen 0.0.0.0:8765 + reverse proxy):
curl -X POST http://127.0.0.1:8765/mcp \
  -H "Authorization: Bearer $QDESK_MAC_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

While running in HTTP mode, qdesk-mac forks `caffeinate -di` to keep the
Mac awake and the display unlocked (suppress with `--no-caffeinate`).
**The Mac must remain logged in and unlocked** — macOS Secure Event
Input blocks all synthetic keyboard events from the lock screen, so
nothing in qdesk-mac can unlock the screen for you.

Endpoints:

- `GET /health` — no auth, returns `{"ok": true}`. Liveness probe.
- `POST /mcp` — bearer auth required. Body is one JSON-RPC request,
  response is one JSON-RPC response.

Clients that prefer SSE framing (Claude Desktop's legacy SSE
transport, some Cursor builds) get a single-event stream instead of
plain JSON when they send `Accept: text/event-stream`. No new
endpoint, no streaming behavior change.

Hardening before exposing to the public internet:

- Front with TLS (caddy / nginx / `tailscale serve`). qdesk-mac speaks plain HTTP.
- Use a strong `--api-key` (32+ random bytes). It's the only auth.
- Bind to `127.0.0.1` and tunnel via SSH or Tailscale, OR bind to
  `0.0.0.0` only behind a reverse proxy with ACLs. Don't expose the
  raw port.

#### Tailscale setup (recommended)

```bash
# 1. Bind only to your Tailscale IP, restrict to the Tailscale CGNAT range:
qdesk-mac --listen $(tailscale ip -4):8765 \
          --api-key "$QDESK_MAC_API_KEY" \
          --trusted-cidr 100.64.0.0/10

# 2. (optional) Front with `tailscale serve` for HTTPS + identity:
tailscale serve --bg --https=443 http://localhost:8765
qdesk-mac --listen 127.0.0.1:8765 \
          --api-key "$QDESK_MAC_API_KEY" \
          --trusted-cidr 127.0.0.0/8 \
          --trust-tailscale-headers
```

`--trusted-cidr` rejects any connection whose source IP is outside the
listed ranges (multiple comma-separated, e.g. `100.64.0.0/10,10.0.0.0/8`).
`X-Forwarded-For` is honored only when the immediate peer is loopback,
so a remote attacker can't spoof their source. `--trust-tailscale-headers`
logs `Tailscale-User-Login` for every request — only enable when you
front qdesk-mac with `tailscale serve`, otherwise an attacker can fake
the headers.

---

## Use cases

qdesk gives you a **primitive**: an AI-controllable Linux desktop. What you
build on top is up to you.

### 🤖 1. AI agent computer use
Give Claude / Gemini / your own agent a real computer to drive. Open URLs,
click, type, observe, decide. Same shape as Anthropic Computer Use, but
**you control the computer** and **it's open source**.

### 🧪 2. Visual UI testing (especially production builds + canvas)
Describe a test in English; an AI agent verifies it. Works on **any rendered
UI** — production builds, canvas-rendered apps (Figma, Excalidraw, custom
Flutter painters), apps you can't instrument. Companion to dedicated tools
like [Maestro](https://maestro.dev) (mobile dev iteration) and
[flutter-skill](https://github.com/ai-dashboad/flutter-skill) (Flutter Dart-VM
testing) which are faster on their home turf.

```yaml
# tests/login.qdesk.yaml
name: "Landing → sign in"
url: http://host.docker.internal:8888
goal: Click "Get started" on the welcome page.
expect:
  - The screen shows "Sign in" near the top-left.
```
```bash
$ qdesk run tests/login.qdesk.yaml
✅ PASS  (3 step(s), 41s, ~$0.005)
📄 report: file:///.../report.html
```

### 🌐 3. AI-driven web automation / scraping
Site has anti-bot measures? Or just complex JS? Drive a real Chromium with
an LLM choosing each action. Slower than Playwright but **handles cases
DOM-based scrapers can't** (canvas, dynamic flows, captchas you have to
visually decide on).

### 🦾 4. RPA / driving apps that have no API
Internal Linux desktop tools, legacy ERP web frontends, anything that's
"only a UI." `apt install` it inside the sandbox image, then have the AI
drive it.

### 🎬 5. Demos / tutorials
"Show me how to use Photoshop / Figma / our internal admin panel." AI
performs the steps in the sandbox while recording — generate animated
guides without manual screen-recording.

### 📊 6. Agent training data / eval
Record trajectories (screenshots + actions + outcomes) of agents
performing tasks. Use as supervised fine-tuning data or as eval harness.

### 🔍 7. Computer-use eval / red-teaming
Test how a frontier model handles unusual UIs, multi-step flows, or
adversarial pages. Sandbox is disposable — agent can't escape into your
host.

---

## How it compares (honest table)

There's a healthy 2026 ecosystem. **qdesk doesn't try to win every cell.**

| | qdesk | [Browserbase](https://browserbase.com) | [E2B](https://e2b.dev) | [Maestro](https://maestro.dev) | [flutter-skill](https://github.com/ai-dashboad/flutter-skill) | [Playwright](https://playwright.dev) |
|---|---|---|---|---|---|---|
| **Scope** | Full Linux desktop | Browser only | Code execution + light desktop | Mobile + web testing | Flutter dev (Dart VM) | Web DOM automation |
| **Open source** | ✅ Apache 2.0 | ❌ SaaS | Open-core | ✅ | ✅ | ✅ |
| **Self-hostable** | ✅ | ❌ | Partial | ✅ | ✅ | ✅ |
| **MCP server** | ✅ built-in | Via partners | ✅ | ✅ | ✅ | Via plugins |
| **Funded** | ❌ open-source side project | $66M | $20M+ | $$$ | community | F500-funded |
| **Best at** | Self-hosted desktop sandbox | Managed browser sandbox | Cloud code+browser | Mobile testing | Flutter dev iteration | Web DOM CI |
| **Worst at** | Speed (LLM screenshot loop) | Anything outside browser | Pure desktop apps | Canvas-only assertions | Production builds | Canvas content |

**TL;DR:** qdesk is the "open-source self-hosted desktop sandbox" cell. If
you want managed cloud and only browser → Browserbase. If you want
mobile-specific testing → Maestro. If you want fast Flutter dev loops →
flutter-skill. If you want a Linux computer your AI can drive, that you
control end-to-end, that's open source — qdesk.

---

## Quickstart

**1. Build the sandbox image** (~1 min on warm cache):

```bash
docker build -t qdesk/ubuntu-chrome:dev -f images/ubuntu-chrome/Dockerfile .
```

**2. Build / install binaries:**

```bash
make build && sudo make install
# Or one-line via the GitHub release:
curl -fsSL https://raw.githubusercontent.com/jackwangfeng/qdesk/main/scripts/install.sh | bash
```

Binaries:
- `qdesk-agentd` — runs **inside** each sandbox (HTTP daemon)
- `qdesk-control` — multi-session control plane
- `qdesk` — CLI runner (testing use case)
- `qdesk-mcp` — MCP server for AI assistants

**3. Run the control plane** (one terminal):

```bash
export QDESK_DEV_KEY=$(openssl rand -hex 16)
qdesk-control --listen 127.0.0.1:8090 --dev-key "$QDESK_DEV_KEY" \
              --image qdesk/ubuntu-chrome:dev
```

**4. Drive a sandbox via plain HTTP** (no LLM):

```bash
# Spin up a session
SESSION=$(curl -s -X POST http://127.0.0.1:8090/v1/sessions \
    -H "Authorization: Bearer $QDESK_DEV_KEY" \
    -H "Content-Type: application/json" \
    -d '{"open_url":"https://example.com"}' | jq -r .session_id)

# Take a screenshot
curl http://127.0.0.1:8090/v1/sessions/$SESSION/screenshot \
    -H "Authorization: Bearer $QDESK_DEV_KEY" \
    --output /tmp/screen.png

# Click somewhere
curl -X POST http://127.0.0.1:8090/v1/sessions/$SESSION/actions \
    -H "Authorization: Bearer $QDESK_DEV_KEY" \
    -H "Content-Type: application/json" \
    -d '{"type":"click","x":500,"y":300}'

# Tear down
curl -X DELETE http://127.0.0.1:8090/v1/sessions/$SESSION \
    -H "Authorization: Bearer $QDESK_DEV_KEY"
```

**5. Or: use it as an AI testing tool with the bundled runner:**

```bash
export GEMINI_API_KEY=AIza...
qdesk run --control http://127.0.0.1:8090 examples/recompdaily-landing.qdesk.yaml
```

**6. Or: register with Claude Code via MCP:**

```bash
claude mcp add --transport stdio qdesk -- qdesk-mcp \
    --control http://127.0.0.1:8090 \
    --api-key "$QDESK_DEV_KEY" \
    --gemini-key "$GEMINI_API_KEY"
```

After this, Claude Code can call `qdesk_screenshot`, `qdesk_quick_test`,
etc. naturally inside a project.

See [`docs/TEAM_QUICKSTART.md`](docs/TEAM_QUICKSTART.md) for the full
team-onboarding flow (5 minutes).

---

## Layout

```
pkg/protocol/         wire types — Action, Session, ActionResult, ...
pkg/client/           Go SDK for qdesk-control HTTP API
internal/agentd/      in-sandbox HTTP daemon (Xvfb-driven)
internal/control/     control plane: sessions, runtime, auth, proxy
internal/llm/         VisionAgent backends (Gemini default; Claude/GPT pluggable)
internal/runner/      .qdesk parser, agent loop, HTML report (testing use case)
cmd/qdesk-agentd/     binary that runs INSIDE each sandbox
cmd/qdesk-control/    control plane binary (one per host / cluster)
cmd/qdesk/            CLI runner for testing
cmd/qdesk-mcp/        MCP server for AI assistants
images/ubuntu-chrome/ Dockerfile + entrypoint for default sandbox
docs/superpowers/     design specs and implementation plans
docs/TEAM_QUICKSTART.md  team onboarding (5 min)
.claude/skills/       Claude Code skill bundle
SKILL.md              integration guide for AI assistants
AGENTS.md             conventions for AI assistants editing qdesk itself
```

## Local development

```bash
make help                    # all targets
make build test smoke        # build + unit tests + e2e smoke
go test ./...                # unit tests only
```

Pure-Go runtime; only third-party deps are `modernc.org/sqlite` (pure Go)
and `gopkg.in/yaml.v3`.

## Status

- ✅ **v0.1** — sandbox + control plane + Gemini agent loop + HTML report + MCP server. Verified end-to-end on a real Flutter Web app.
- 🔄 **v0.2 (planned)** — replay mode + self-heal traces, web UI for the control plane, browser cookies/auth persistence, GPU sandbox template.
- 🔮 **v0.3+ (planned)** — Android emulator template, macOS/iOS simulator template (positioned as agent sandboxes, not testing-only), Firecracker microVM, agent trajectory recording for training data.

## Related projects

Adjacent tools, all worth knowing:

- **Managed AI sandbox SaaS** — [Browserbase](https://browserbase.com) (browser only), [E2B](https://e2b.dev) (code+light desktop), [Anchor Browser](https://anchorbrowser.io), [Hyperbrowser](https://hyperbrowser.ai)
- **Testing — mobile** — [Maestro](https://maestro.dev) (mobile + web, MCP-native, gold standard)
- **Testing — Flutter dev iteration** — [flutter-skill](https://github.com/ai-dashboad/flutter-skill) (Dart VM in-process)
- **Testing — web DOM** — [Playwright](https://playwright.dev) (DOM automation), [Cypress](https://cypress.io)
- **AI agent computer use** — [Anthropic Computer Use](https://docs.anthropic.com/claude/docs/computer-use) (agent side; qdesk provides the computer)
- **Browser-as-agent libraries** — [Browser Use](https://github.com/browser-use/browser-use), [Stagehand](https://github.com/browserbase/stagehand), [Skyvern](https://github.com/Skyvern-AI/skyvern)

If you want managed, polished, well-funded — pick one of those for your
specific need. If you want **open source, self-hosted, full-Linux, that
you can fork and shape** — that's qdesk.

## License

Apache 2.0 — see `LICENSE`.
