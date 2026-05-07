#!/usr/bin/env bash
# Build qdesk-win.exe and scp it to a Windows host. Caller must have
# OpenSSH login set up. Usage:
#   QDESK_WIN_HOST=Administrator@192.168.0.127 ./scripts/install-win.sh
set -euo pipefail
HOST="${QDESK_WIN_HOST:-Administrator@192.168.0.127}"
GOOS=windows GOARCH=amd64 go build -o bin/qdesk-win.exe ./cmd/qdesk-win
scp bin/qdesk-win.exe "$HOST:qdesk-win.exe"
echo "Deployed. Generate an API key with: openssl rand -hex 32"
echo "Run on the host:"
echo "  qdesk-win.exe --listen 0.0.0.0:8765 --api-key <KEY>"
echo "  (allow inbound 8765 in Windows Defender Firewall first)"
