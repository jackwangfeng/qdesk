#!/usr/bin/env bash
#
# scripts/release.sh — cut a tagged release of qdesk.
#
# Usage:
#   VERSION=v0.1.0 ./scripts/release.sh                    # interactive
#   VERSION=v0.1.0 GH_USER=you DOCKER_USER=you ./scripts/release.sh  --no-prompt
#
# Steps (each step asks for confirmation by default):
#   1. Verify working tree is clean and on a release branch.
#   2. Run go test, go vet.
#   3. Build host binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64.
#   4. Build sandbox docker image (tagged with VERSION).
#   5. Run smoke test against the new image.
#   6. Tag git (signed if available).
#   7. Push tag to remote (asks).
#   8. Push docker image to Docker Hub (asks; needs `docker login`).
#   9. Create a GitHub release with the cross-built binaries (uses `gh` CLI; asks).
#
# This script is idempotent for non-destructive steps. Destructive steps
# (push, docker push, gh release create) always prompt unless --no-prompt
# is given AND a corresponding env var is set.

set -euo pipefail

# ---------- argument parsing ----------

NO_PROMPT=0
for arg in "$@"; do
  case "$arg" in
    --no-prompt) NO_PROMPT=1 ;;
    -h|--help) sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

VERSION="${VERSION:-}"
GH_USER="${GH_USER:-}"
DOCKER_USER="${DOCKER_USER:-}"
DOCKER_REPO="${DOCKER_REPO:-qdesk-ubuntu-chrome}"

if [[ -z "$VERSION" ]]; then
  echo "VERSION env var is required (e.g. VERSION=v0.1.0)" >&2
  exit 2
fi
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$ ]]; then
  echo "VERSION must be a semver tag like v0.1.0 or v0.1.0-rc1, got: $VERSION" >&2
  exit 2
fi

# ---------- helpers ----------

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DIST="$ROOT/dist"
SANDBOX_IMAGE="qdesk/ubuntu-chrome:$VERSION"
SANDBOX_IMAGE_LATEST="qdesk/ubuntu-chrome:latest"

step()  { printf '\n\033[1;36m▶ %s\033[0m\n' "$*"; }
ok()    { printf '\033[0;32m  ✓ %s\033[0m\n' "$*"; }
warn()  { printf '\033[0;33m  ⚠ %s\033[0m\n' "$*"; }
fail()  { printf '\033[0;31m  ✗ %s\033[0m\n' "$*" >&2; exit 1; }

confirm() {
  local prompt="$1"
  if [[ $NO_PROMPT -eq 1 ]]; then return 0; fi
  read -r -p "  ❓ $prompt [y/N] " ans
  [[ "$ans" =~ ^[Yy]$ ]]
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

# ---------- step 1: preflight ----------

step "1/9 preflight: clean tree + branch sanity"
require_cmd git
require_cmd go
require_cmd docker

if ! git diff-index --quiet HEAD --; then
  fail "working tree has uncommitted changes; commit or stash first"
fi
if [[ -n "$(git status --porcelain)" ]]; then
  fail "untracked files present; clean them first (git status)"
fi

BRANCH="$(git rev-parse --abbrev-ref HEAD)"
ok "branch=$BRANCH version=$VERSION"

if [[ "$BRANCH" != "main" ]]; then
  warn "you are NOT on main"
  confirm "release from '$BRANCH' anyway?" || exit 1
fi

if git rev-parse "$VERSION" >/dev/null 2>&1; then
  fail "tag $VERSION already exists"
fi

# ---------- step 2: test + vet ----------

step "2/9 go vet + go test"
go vet ./... && ok "vet clean"
go test ./... && ok "tests pass"

# ---------- step 3: cross-build ----------

step "3/9 cross-build host binaries"
rm -rf "$DIST"
mkdir -p "$DIST"
LDFLAGS="-s -w \
  -X github.com/jeffwang/qdesk/pkg/version.Version=$VERSION \
  -X github.com/jeffwang/qdesk/pkg/version.Commit=$(git rev-parse --short HEAD)"

for plat in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os="${plat%/*}"; arch="${plat#*/}"
  for bin in qdesk qdesk-control qdesk-mcp; do
    out="$DIST/${bin}-${os}-${arch}"
    GOOS=$os GOARCH=$arch CGO_ENABLED=0 \
      go build -trimpath -ldflags="$LDFLAGS" -o "$out" "./cmd/$bin"
    ok "$out"
  done
done
# qdesk-agentd is only linux/amd64 (lives in the sandbox image).
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="$LDFLAGS" -o "$DIST/qdesk-agentd-linux-amd64" ./cmd/qdesk-agentd
ok "$DIST/qdesk-agentd-linux-amd64"

# Tarballs per platform.
for plat in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os="${plat%/*}"; arch="${plat#*/}"
  tarball="$DIST/qdesk-${VERSION}-${os}-${arch}.tar.gz"
  tar -C "$DIST" -czf "$tarball" \
    "qdesk-${os}-${arch}" "qdesk-control-${os}-${arch}" "qdesk-mcp-${os}-${arch}"
  ok "$tarball"
done

# Checksums.
( cd "$DIST" && sha256sum qdesk-${VERSION}-*.tar.gz > "checksums.txt" )
ok "$DIST/checksums.txt"

# ---------- step 4: docker image ----------

step "4/9 build sandbox docker image"
docker build \
  --build-arg APT_MIRROR="${APT_MIRROR:-mirrors.aliyun.com}" \
  --build-arg GOPROXY="${GOPROXY_BUILD:-https://goproxy.cn,direct}" \
  --build-arg VERSION="$VERSION" \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  -t "$SANDBOX_IMAGE" \
  -t "$SANDBOX_IMAGE_LATEST" \
  -t "qdesk/ubuntu-chrome:dev" \
  -f images/ubuntu-chrome/Dockerfile .
ok "tagged $SANDBOX_IMAGE + :latest + :dev"

# ---------- step 5: smoke ----------

step "5/9 smoke test"
IMAGE="$SANDBOX_IMAGE" ./scripts/smoke-sandbox.sh
ok "smoke PASS"

# ---------- step 6: tag ----------

step "6/9 git tag $VERSION"
if git config user.signingkey >/dev/null 2>&1; then
  git tag -s "$VERSION" -m "qdesk $VERSION"
  ok "signed tag created"
else
  git tag -a "$VERSION" -m "qdesk $VERSION"
  warn "unsigned annotated tag (no signing key configured)"
fi

# ---------- step 7: push tag ----------

step "7/9 push tag to remote"
if git remote -v | grep -q .; then
  if confirm "push $VERSION to origin?"; then
    git push origin "$VERSION"
    ok "tag pushed"
  else
    warn "skipped tag push (run 'git push origin $VERSION' later)"
  fi
else
  warn "no git remote configured; skipping push"
  warn "to publish: git remote add origin <url> && git push -u origin main && git push origin $VERSION"
fi

# ---------- step 8: docker push ----------

step "8/9 docker push"
if [[ -n "$DOCKER_USER" ]]; then
  USER_IMAGE="$DOCKER_USER/$DOCKER_REPO:$VERSION"
  USER_IMAGE_LATEST="$DOCKER_USER/$DOCKER_REPO:latest"
  if confirm "tag and push to $USER_IMAGE + :latest?"; then
    docker tag "$SANDBOX_IMAGE" "$USER_IMAGE"
    docker tag "$SANDBOX_IMAGE" "$USER_IMAGE_LATEST"
    if ! docker push "$USER_IMAGE"; then
      fail "docker push failed; check 'docker login' and DOCKER_USER"
    fi
    docker push "$USER_IMAGE_LATEST"
    ok "pushed $USER_IMAGE + :latest"
  else
    warn "skipped docker push"
  fi
else
  warn "DOCKER_USER not set; skipping Docker Hub push"
  warn "to publish: DOCKER_USER=youraccount ./scripts/release.sh"
fi

# ---------- step 9: github release ----------

step "9/9 github release"
if command -v gh >/dev/null 2>&1; then
  if confirm "create GitHub release for $VERSION with binaries?"; then
    gh release create "$VERSION" \
      --title "qdesk $VERSION" \
      --notes-file <(scripts/release-notes.sh "$VERSION" 2>/dev/null || echo "Release notes: see CHANGELOG / git log $VERSION") \
      "$DIST"/qdesk-${VERSION}-*.tar.gz \
      "$DIST/checksums.txt"
    ok "release created"
  else
    warn "skipped GitHub release"
  fi
else
  warn "'gh' CLI not on PATH; skipping GitHub release"
  warn "install: https://cli.github.com/  then run:  gh release create $VERSION $DIST/*.tar.gz"
fi

echo
echo "==========================================================="
ok "release $VERSION done"
echo "  binaries: $DIST/"
echo "  image:    $SANDBOX_IMAGE"
echo "==========================================================="
