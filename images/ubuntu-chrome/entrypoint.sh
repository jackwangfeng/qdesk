#!/usr/bin/env bash
set -euo pipefail

DISPLAY_NUM="${DISPLAY_NUM:-99}"
RES="${RES:-1920x1080x24}"

# Start Xvfb.
Xvfb ":${DISPLAY_NUM}" -screen 0 "${RES}" -ac +extension RANDR -nolisten tcp &
XVFB_PID=$!
export DISPLAY=":${DISPLAY_NUM}"

# Wait for X to be ready.
for _ in $(seq 1 30); do
    if xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then
        break
    fi
    sleep 0.2
done

# Start a minimal window manager (xfwm4 alone, no full xfce session).
xfwm4 --display "$DISPLAY" --replace &
WM_PID=$!

# Set a neutral root color so screenshots aren't black.
xsetroot -solid '#202020' -display "$DISPLAY"

# Cleanup on exit.
cleanup() {
    kill "$WM_PID" 2>/dev/null || true
    kill "$XVFB_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Hand off to qdesk-agentd in foreground.
exec /usr/local/bin/qdesk-agentd \
    --listen "0.0.0.0:7878" \
    --display ":${DISPLAY_NUM}"
