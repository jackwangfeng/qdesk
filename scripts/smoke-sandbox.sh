#!/usr/bin/env bash
set -euo pipefail

IMAGE="${IMAGE:-qdesk/ubuntu-chrome:dev}"
NAME="qdesk-smoke-$$"
PORT=$(shuf -i 30000-39999 -n 1)

cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "==> starting container on port $PORT"
docker run -d --name "$NAME" -p "${PORT}:7878" "$IMAGE"

echo "==> waiting for /health"
for i in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
        echo "    ready after ${i} attempts"
        break
    fi
    sleep 0.5
    if [ "$i" = "30" ]; then echo "TIMEOUT"; docker logs "$NAME"; exit 1; fi
done

echo "==> /health"
curl -fsS "http://127.0.0.1:${PORT}/health" | tee /dev/stderr; echo

echo "==> /screenshot to /tmp/qdesk-smoke.png"
curl -fsS "http://127.0.0.1:${PORT}/screenshot" --output /tmp/qdesk-smoke.png
file /tmp/qdesk-smoke.png

echo "==> POST /actions { type: wait, ms: 100 }"
curl -fsS -X POST "http://127.0.0.1:${PORT}/actions" \
    -H 'content-type: application/json' \
    -d '{"type":"wait","ms":100}' | tee /dev/stderr; echo

echo "==> POST /actions { type: click, x: 50, y: 50 }"
curl -fsS -X POST "http://127.0.0.1:${PORT}/actions" \
    -H 'content-type: application/json' \
    -d '{"type":"click","x":50,"y":50}' | tee /dev/stderr; echo

echo "PASS"
