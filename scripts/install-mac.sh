#!/usr/bin/env bash
set -euo pipefail

# Builds qdesk-mac + qdesk-mac-helper from source and installs them to
# /usr/local/bin. The helper path MUST be stable so macOS TCC doesn't
# re-prompt for Screen Recording / Accessibility on every rebuild.

if [[ "$(uname)" != "Darwin" ]]; then
  echo "qdesk-mac is macOS-only. This is $(uname)." >&2
  exit 1
fi

cd "$(dirname "$0")/.."
make mac-build

DEST="/usr/local/bin"
if [[ ! -d "$DEST" ]]; then
  sudo mkdir -p "$DEST"
fi

echo "Installing to $DEST (will prompt for sudo)..."
sudo install -m 0755 bin/qdesk-mac        "$DEST/qdesk-mac"
sudo install -m 0755 bin/qdesk-mac-helper "$DEST/qdesk-mac-helper"

echo
echo "Installed:"
ls -l "$DEST/qdesk-mac" "$DEST/qdesk-mac-helper"
echo
echo "Next: run \`qdesk-mac doctor\` to grant Screen Recording + Accessibility"
echo "to /usr/local/bin/qdesk-mac-helper."
