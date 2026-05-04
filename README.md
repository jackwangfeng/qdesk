# qdesk

**AI visual testing for any web app — including production builds, canvas-rendered UIs, and apps you can't instrument.**

Describe a test in plain English, an AI agent runs it inside a sandboxed Linux + Chromium container, get pass/fail with a screenshot trace and AI-written failure diagnosis.

> 👉 **AI assistants (Claude Code, Cursor, Copilot, …)** — qdesk ships an **[MCP server](.claude/mcp-install.md)**. Plus a [`SKILL.md`](./SKILL.md) for tools that prefer plain shell commands. Editing this codebase? Read **[`AGENTS.md`](./AGENTS.md)**.

---

## Why qdesk vs other AI testing tools

There's a healthy ecosystem of AI testing tools now. **qdesk is not trying to replace any of them — it covers the cases they can't.**

| | **qdesk** | [flutter-skill](https://github.com/ai-dashboad/flutter-skill) | Playwright + AI plugins |
|---|---|---|---|
| **Approach** | Real Chromium in Docker, screenshot + vision LLM | Dart VM Service (in-process Flutter) | DOM selectors + LLM grounding |
| **Speed per action** | 100-500 ms | **1-2 ms** ⭐ | 50-100 ms |
| **Cost per action** | ~$0.0005 (LLM screenshot) | ~free (no LLM call) ⭐ | varies |
| **Works on production builds** | **✅ yes** ⭐ | ❌ needs Dart VM Service (debug/profile only) | ✅ yes |
| **Works on non-Flutter apps** | **✅ any web app** ⭐ | ❌ Flutter-only | ✅ web only |
| **Works on canvas-rendered UIs** | **✅ vision sees pixels** ⭐ | ⚠️ widget tree only — misses CustomPainter content | ❌ canvas is a black box |
| **Sandbox isolation** | **✅ Docker** ⭐ | ❌ attaches to running app | ❌ same machine |
| **Setup** | 5 min (build image once) | <1 min ⭐ | <1 min |
| **Cross-platform** | Linux + Web today; Android/iOS planned | 10 platforms (Flutter, RN, native, web, …) ⭐ | Web (Chrome/FF/Safari) |
| **Best for** | Canvas apps, production builds, any-web-app, AI sandboxes | Day-to-day Flutter dev | DOM-heavy SPAs |

**TL;DR — pick the right tool for the job:**

- 🎨 **Canvas-rendered apps** (Figma, Excalidraw, Miro, custom Flutter painters) → **qdesk**
- 📱 **Day-to-day Flutter dev iteration** (debug builds) → **flutter-skill** (faster, cheaper, more tools)
- 🚀 **Production / release-build verification** (no Dart VM available) → **qdesk**
- 🌐 **Any non-Flutter web app** (React / Vue / Svelte / vanilla) → **qdesk** or **Playwright + LLM**
- 🖱 **DOM-heavy traditional web apps** → **Playwright** (fastest, cheapest, deterministic)
- 🤖 **Generic "AI agent computer-use" sandbox** (open URLs, screenshot, click, beyond testing) → **qdesk**

The healthy stack: **Playwright** for fast DOM CI + **flutter-skill** for Flutter dev loops + **qdesk** for the visual / production / canvas / any-app cases the others can't see.

---

## What it does

```bash
$ qdesk run tests/login.qdesk.yaml
▶ qdesk run tests/login.qdesk.yaml
✅ PASS  (3 step(s), 41s, ~$0.005)
  ✓ The screen shows "Sign in" prominently near the top-left
  ✓ A back arrow icon is visible at the top-left
  ✓ The brand logo is still visible
📄 report: file:///.../report.html
```

A `tests/login.qdesk.yaml` looks like:

```yaml
name: "Landing → sign in"
url: http://host.docker.internal:8888
goal: |
  On the welcome page, click the red "Get started" button to go to Sign in.
expect:
  - The screen shows "Sign in" near the top-left.
  - A back arrow (←) is visible at the top-left.
```

## How it works

```
qdesk run X.yaml
  │
  ├─ qdesk-control HTTP API (multi-session, SQLite, bearer auth)
  │     └─ Docker → qdesk/ubuntu-chrome:dev sandbox
  │           └─ qdesk-agentd (Xvfb + Chromium + xdotool + scrot)
  │
  └─ runner: loop { screenshot → LLM (Gemini/Claude/...) → action → done? }
       └─ HTML report + trace.json
```

**Key properties**
- 🔍 **Vision-based** — sees what the user sees, including canvas pixels and CustomPainter content
- 🐳 **Sandboxed** — clean Linux + Chromium per session, no app instrumentation needed
- 📦 **Production-build friendly** — works on any rendered UI, no debug protocols required
- 🌐 **App-agnostic** — Flutter Web, React, Vue, vanilla, internal tools, third-party SaaS
- 🤖 **Multi-model** — Gemini default (cheap), Claude/GPT-4o pluggable
- 📊 **HTML report** — every screenshot + agent reasoning + AI failure diagnosis
- 💰 **~$0.005 per test** with Gemini 2.5 Flash
- 🔌 **SDK-first** — HTTP API + Go client + MCP server; CLI is just one consumer
- 🪶 **Pure-Go runtime** (no CGO), single binary per component

## When you should NOT use qdesk

Honest list of where other tools win:

- ⚡ **Need 1-2 ms per action** for Flutter dev hot loops → use [flutter-skill](https://github.com/ai-dashboad/flutter-skill)
- 🧪 **DOM-precise assertions** ("element has class X") → use Playwright
- 🌍 **Cross-browser testing** (Firefox / Safari / Edge) → use Playwright
- 📊 **1000+ tests in CI per PR** → cost adds up; mix qdesk into the slow lane only
- 🔌 **Network mocking / interception** → Playwright
- 📱 **Day-to-day Flutter dev iteration on debug builds** → flutter-skill is the right tool

qdesk and these tools **complement each other** — most production teams should run more than one.

## Quickstart

**1. Build the sandbox image** (one time, ~1 min on warm cache):

```bash
docker build -t qdesk/ubuntu-chrome:dev -f images/ubuntu-chrome/Dockerfile .
```

**2. Build the binaries:**

```bash
go build -o /usr/local/bin/qdesk-agentd  ./cmd/qdesk-agentd
go build -o /usr/local/bin/qdesk-control ./cmd/qdesk-control
go build -o /usr/local/bin/qdesk         ./cmd/qdesk
go build -o /usr/local/bin/qdesk-mcp     ./cmd/qdesk-mcp     # MCP server for Claude Code / Cursor
```

Or one-line install (downloads the latest GitHub release):

```bash
curl -fsSL https://raw.githubusercontent.com/jackwangfeng/qdesk/main/scripts/install.sh | bash
```

**3. Run the control plane** (one terminal, leave running):

```bash
export QDESK_DEV_KEY=devkeysecret
qdesk-control --listen 127.0.0.1:8090 --dev-key $QDESK_DEV_KEY \
              --image qdesk/ubuntu-chrome:dev
```

**4. Run a test** (any terminal):

```bash
export GEMINI_API_KEY=AIza...
export QDESK_DEV_KEY=devkeysecret
qdesk run --control http://127.0.0.1:8090 examples/recompdaily-landing.qdesk.yaml
```

The example assumes a static server on port 8888 inside `host.docker.internal`.
Substitute any URL your app exposes.

## Direct sandbox usage (no LLM)

If you just want a programmable Chromium without AI:

```bash
docker run -d --rm --name qdesk-sbx -p 7878:7878 qdesk/ubuntu-chrome:dev

curl http://localhost:7878/health
curl http://localhost:7878/screenshot --output /tmp/screen.png
curl -X POST http://localhost:7878/actions \
    -H 'content-type: application/json' \
    -d '{"type":"click","x":100,"y":200}'

docker stop qdesk-sbx
```

Or:
```bash
./scripts/smoke-sandbox.sh
```

This makes qdesk usable as a **generic AI-agent sandbox** for non-testing use cases too — give an LLM a fresh Chromium it can drive over HTTP.

## Layout

```
pkg/protocol/         wire types — Action, Session, ActionResult, ...
pkg/client/           Go SDK for qdesk-control HTTP API
internal/agentd/      in-sandbox HTTP daemon (Xvfb-driven)
internal/control/     control plane: sessions, runtime, auth, proxy
internal/llm/         VisionAgent backends (gemini.go is the default)
internal/runner/      .qdesk parser, agent loop, HTML report
cmd/qdesk-agentd/     binary that runs INSIDE each sandbox
cmd/qdesk-control/    control plane binary (one per host / cluster)
cmd/qdesk/            CLI: `qdesk run …`
cmd/qdesk-mcp/        MCP server for Claude Code / Cursor / Aider
images/ubuntu-chrome/ Dockerfile + entrypoint for default sandbox
docs/superpowers/     design specs and implementation plans
docs/TEAM_QUICKSTART.md  team onboarding (5 min)
.claude/skills/       Claude Code skill bundle
SKILL.md              integration guide for AI assistants
AGENTS.md             conventions for AI assistants editing qdesk itself
```

## Local development

```bash
go test ./...                              # unit tests (13 currently)
./scripts/smoke-sandbox.sh                 # end-to-end sandbox smoke
make help                                  # all targets
```

Zero third-party Go dependencies for `qdesk-agentd` (stdlib only). Control
plane adds `modernc.org/sqlite` (pure Go) and `gopkg.in/yaml.v3`.

## Status

- ✅ Phase 0 — sandbox + control plane + Gemini agent loop + HTML report + MCP. Verified end-to-end on a real Flutter Web app.
- 🔄 Phase 1 — Replay mode + self-heal traces, web UI for the control plane, GitHub Action template.
- 🔮 Phase 2 — Android emulator template, macOS host with iOS simulator, Firecracker microVM, snapshots.
- 🔮 Phase 3 — Multi-tenant cloud SKU.

## Related projects

These solve adjacent problems and are worth knowing about:

- **[flutter-skill](https://github.com/ai-dashboad/flutter-skill)** — in-process Flutter testing via Dart VM Service. **Faster than qdesk for Flutter dev loops** but requires the app to expose VM Service (debug/profile builds). Best for daily Flutter iteration.
- **[Playwright](https://playwright.dev/)** — DOM-based browser automation, gold standard for traditional web E2E. Add LLM grounding plugins for AI-driven tests in DOM apps.
- **[Browserbase](https://browserbase.com/)** / **[E2B](https://e2b.dev/)** — managed cloud sandboxes for AI agents (browser-only / general). qdesk's control plane is similar in shape but open source.
- **[Anthropic Computer Use](https://docs.anthropic.com/claude/docs/computer-use)** — Claude's built-in screen+mouse capability. qdesk provides the "computer" that Computer Use drives.

## License

Apache 2.0 — see `LICENSE`.
