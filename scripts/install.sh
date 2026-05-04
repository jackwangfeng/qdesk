#!/usr/bin/env bash
#
# qdesk one-line installer for team members.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/jackwangfeng/qdesk/main/scripts/install.sh | bash
#
# Or with a specific version:
#   curl -fsSL https://raw.githubusercontent.com/jackwangfeng/qdesk/main/scripts/install.sh | VERSION=v0.1.0 bash
#
# What it does:
#   1. Detect OS + architecture.
#   2. Download the matching qdesk binaries from GitHub releases.
#   3. Install to ~/.local/bin (no sudo needed).
#   4. Add ~/.local/bin to PATH if not already there.
#   5. Pull the sandbox docker image (or guide user to build it).
#   6. Print the next-step instructions (env vars, control plane startup).

set -euo pipefail

REPO="${REPO:-jackwangfeng/qdesk}"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
SANDBOX_IMAGE_LOCAL="qdesk/ubuntu-chrome"
SANDBOX_IMAGE_HUB="${SANDBOX_IMAGE_HUB:-jackwangfeng/qdesk-ubuntu-chrome}"

step()  { printf '\n\033[1;36m▶ %s\033[0m\n' "$*"; }
ok()    { printf '\033[0;32m  ✓ %s\033[0m\n' "$*"; }
warn()  { printf '\033[0;33m  ⚠ %s\033[0m\n' "$*"; }
fail()  { printf '\033[0;31m  ✗ %s\033[0m\n' "$*" >&2; exit 1; }

# 1. detect platform
step "Detecting platform"
case "$(uname -s)" in
  Linux)  os=linux  ;;
  Darwin) os=darwin ;;
  *) fail "unsupported OS: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) fail "unsupported arch: $(uname -m)" ;;
esac
ok "platform = ${os}/${arch}"

# 2. resolve version
if [[ "$VERSION" == "latest" ]]; then
  step "Resolving latest release"
  VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -oP '"tag_name":\s*"\K[^"]+' | head -1)" \
    || fail "could not resolve latest release"
  ok "VERSION = $VERSION"
fi

# 3. download
step "Downloading qdesk-${VERSION}-${os}-${arch}.tar.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
URL="https://github.com/$REPO/releases/download/$VERSION/qdesk-${VERSION}-${os}-${arch}.tar.gz"
curl -fL "$URL" -o "$TMP/qdesk.tar.gz" || fail "download failed: $URL"
tar -xzf "$TMP/qdesk.tar.gz" -C "$TMP"
ok "extracted to $TMP"

# 4. install
step "Installing to $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"
for bin in qdesk qdesk-control qdesk-mcp; do
  src="$TMP/${bin}-${os}-${arch}"
  [[ -f "$src" ]] || fail "missing $src in tarball"
  install -m 0755 "$src" "$INSTALL_DIR/$bin"
  ok "$INSTALL_DIR/$bin"
done

# 5. PATH
if ! printf "%s" ":$PATH:" | grep -q ":$INSTALL_DIR:"; then
  warn "$INSTALL_DIR not in PATH"
  echo "    Add this to your shell rc (~/.bashrc or ~/.zshrc):"
  echo "      export PATH=\"\$HOME/.local/bin:\$PATH\""
fi

# 6. docker image
step "Sandbox docker image"
if ! command -v docker >/dev/null 2>&1; then
  warn "docker is not installed; please install Docker Desktop or docker-engine first"
else
  if docker image inspect "$SANDBOX_IMAGE_LOCAL:$VERSION" >/dev/null 2>&1 \
     || docker image inspect "$SANDBOX_IMAGE_LOCAL:dev" >/dev/null 2>&1; then
    ok "image already present locally"
  else
    if docker pull "$SANDBOX_IMAGE_HUB:$VERSION" 2>/dev/null; then
      docker tag "$SANDBOX_IMAGE_HUB:$VERSION" "$SANDBOX_IMAGE_LOCAL:dev"
      ok "pulled $SANDBOX_IMAGE_HUB:$VERSION"
    else
      warn "image not on Docker Hub yet; build locally:"
      echo "      git clone https://github.com/$REPO.git"
      echo "      cd qdesk && make image"
    fi
  fi
fi

# 7. final instructions
cat <<'EOF'

╔══════════════════════════════════════════════════════════╗
║  qdesk installed. Three things left to do once.          ║
╠══════════════════════════════════════════════════════════╣

(1) Set environment variables in your shell rc:
    export GEMINI_API_KEY=AIza...        # from https://aistudio.google.com
    export QDESK_DEV_KEY=$(openssl rand -hex 16)   # any secret

(2) Start the control plane (once per machine, leave running):
    nohup qdesk-control \
        --listen 127.0.0.1:8090 \
        --dev-key "$QDESK_DEV_KEY" \
        --image qdesk/ubuntu-chrome:dev \
        > ~/.qdesk-control.log 2>&1 &

    # Or run it in a tmux/screen/foreground if you prefer.

(3) Register qdesk-mcp with Claude Code (once):
    claude mcp add --transport stdio qdesk -- qdesk-mcp \
        --control http://127.0.0.1:8090 \
        --api-key "$QDESK_DEV_KEY" \
        --gemini-key "$GEMINI_API_KEY"

After that, in any project, Claude Code can call qdesk_quick_test
and qdesk_screenshot to verify UI changes you make.

Verify:
    curl http://127.0.0.1:8090/v1/health   # should print {"status":"ok"}
    qdesk version

Docs: https://github.com/jackwangfeng/qdesk/blob/main/SKILL.md

╚══════════════════════════════════════════════════════════╝
EOF
