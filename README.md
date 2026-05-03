# qdesk

**AI-native visual UI testing.** Describe a test in plain English, an AI agent runs it in a sandboxed Chromium, get a pass/fail with a screenshot trace.

> 👉 **AI assistants (Claude Code, Cursor, Copilot, …)** — qdesk ships an **[MCP server](.claude/mcp-install.md)** so Claude Code etc. can call it as a native tool, plus a [`SKILL.md`](./SKILL.md) for tools that prefer plain shell commands. Editing this codebase? Read **[`AGENTS.md`](./AGENTS.md)** for project conventions.

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

**Key features**
- 🤖 Multi-model agent (Gemini default; Claude/GPT-4o pluggable)
- 🐳 Cloud-friendly sandbox (Docker today, Firecracker later)
- 📊 Per-test HTML report with every screenshot + agent reasoning
- 🩺 AI auto-diagnosis on failure
- 💰 ~$0.005 per test with Gemini 2.5 Flash
- 🔌 SDK-first: HTTP API + Go client; CLI is built on top
- 🪶 Pure-Go runtime (no CGO), single binary per component

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
images/ubuntu-chrome/ Dockerfile + entrypoint for default sandbox
docs/superpowers/     design specs and implementation plans
.claude/skills/       Claude Code skill bundle
SKILL.md              integration guide for AI assistants
AGENTS.md             conventions for AI assistants editing qdesk itself
```

## Local development

```bash
go test ./...                              # unit tests (13 currently)
./scripts/smoke-sandbox.sh                 # end-to-end sandbox smoke
```

Zero third-party Go dependencies for `qdesk-agentd` (stdlib only). Control
plane adds `modernc.org/sqlite` (pure Go) and `gopkg.in/yaml.v3`.

## Status

- ✅ Phase 0 — sandbox + control plane + Gemini agent loop + HTML report. Verified end-to-end on a real Flutter Web app.
- 🔄 Phase 1 — Replay mode + self-heal traces, Web UI for the control plane, GitHub Action.
- 🔮 Phase 2 — Firecracker microVM, snapshots, multi-tenant cloud SKU.

## License

Apache 2.0 — see `LICENSE`.
