#!/usr/bin/env bash
#
# scripts/demo.sh — run the canonical qdesk demo end-to-end.
#
# What it does:
#   1. Build all binaries (if missing).
#   2. Ensure the sandbox image exists.
#   3. Start a static file server pointing at the RecompDaily Flutter Web
#      build (if found locally; otherwise prompts you for a URL).
#   4. Start qdesk-control on a free port.
#   5. Run examples/recompdaily-landing.qdesk.yaml.
#   6. Tear everything down.
#
# Required env vars:
#   GEMINI_API_KEY   — your Gemini API key.
#   QDESK_DEV_KEY    — bearer token for control plane (auto-generated if absent).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DIST="$ROOT/dist"
QDESK="$DIST/qdesk"
QDESK_CONTROL="$DIST/qdesk-control"
SANDBOX_IMAGE="${SANDBOX_IMAGE:-qdesk/ubuntu-chrome:dev}"
EXAMPLE_YAML="${EXAMPLE_YAML:-$ROOT/examples/recompdaily-landing.qdesk.yaml}"
RECOMPDAILY_BUILD="${RECOMPDAILY_BUILD:-$HOME/workdir/loss-weight/frontend/build/web}"

step()  { printf '\n\033[1;36m▶ %s\033[0m\n' "$*"; }
ok()    { printf '\033[0;32m  ✓ %s\033[0m\n' "$*"; }
warn()  { printf '\033[0;33m  ⚠ %s\033[0m\n' "$*"; }
fail()  { printf '\033[0;31m  ✗ %s\033[0m\n' "$*" >&2; exit 1; }

if [[ -z "${GEMINI_API_KEY:-}" ]]; then
  fail "GEMINI_API_KEY env var is required"
fi
export QDESK_DEV_KEY="${QDESK_DEV_KEY:-$(openssl rand -hex 16)}"

CONTROL_PORT="${CONTROL_PORT:-8090}"
STATIC_PORT="${STATIC_PORT:-8888}"

CLEANUP_PIDS=()
cleanup() {
  for pid in "${CLEANUP_PIDS[@]:-}"; do
    [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  done
  docker rm -f $(docker ps -aq --filter "name=qdesk-sbx_") 2>/dev/null || true
}
trap cleanup EXIT

# 1) build
step "1/5 build binaries"
if [[ ! -x "$QDESK" || ! -x "$QDESK_CONTROL" ]]; then
  make build >/dev/null
fi
ok "binaries ready in $DIST"

# 2) image
step "2/5 sandbox image present?"
if ! docker image inspect "$SANDBOX_IMAGE" >/dev/null 2>&1; then
  warn "image $SANDBOX_IMAGE not found; building (may take a few minutes)"
  make image
fi
ok "image present"

# 3) static server
step "3/5 static server for RecompDaily"
if [[ -d "$RECOMPDAILY_BUILD" ]]; then
  ( cd "$RECOMPDAILY_BUILD" && python3 -m http.server "$STATIC_PORT" --bind 0.0.0.0 \
        > /tmp/qdesk-demo-static.log 2>&1 ) &
  CLEANUP_PIDS+=("$!")
  sleep 1
  curl -fsS "http://127.0.0.1:$STATIC_PORT/index.html" >/dev/null \
    || fail "static server didn't come up; see /tmp/qdesk-demo-static.log"
  ok "serving $RECOMPDAILY_BUILD at :$STATIC_PORT"
else
  warn "RECOMPDAILY_BUILD not found at $RECOMPDAILY_BUILD"
  warn "demo will still run if the YAML's URL points elsewhere"
fi

# 4) control plane
step "4/5 control plane on :$CONTROL_PORT"
"$QDESK_CONTROL" \
  --listen "127.0.0.1:$CONTROL_PORT" \
  --image  "$SANDBOX_IMAGE" \
  --db     /tmp/qdesk-demo.db \
  --dev-key "$QDESK_DEV_KEY" \
  > /tmp/qdesk-demo-control.log 2>&1 &
CLEANUP_PIDS+=("$!")
for i in $(seq 1 20); do
  if curl -fsS "http://127.0.0.1:$CONTROL_PORT/v1/health" >/dev/null 2>&1; then
    ok "control plane ready"
    break
  fi
  sleep 0.3
  [[ $i -eq 20 ]] && fail "control plane didn't come up; tail /tmp/qdesk-demo-control.log"
done

# 5) run
step "5/5 qdesk run $EXAMPLE_YAML"
"$QDESK" run \
  --control "http://127.0.0.1:$CONTROL_PORT" \
  --gemini-key "$GEMINI_API_KEY" \
  "$EXAMPLE_YAML"
