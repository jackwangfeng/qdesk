#!/usr/bin/env bash
#
# scripts/release-notes.sh — print release notes for VERSION on stdout.
#
# Strategy:
#   1. If a CHANGELOG.md section for VERSION exists, print it.
#   2. Else build a "What's changed since last tag" list from git log.
#
# Used by scripts/release.sh.

set -euo pipefail

VERSION="${1:-}"
[[ -z "$VERSION" ]] && { echo "usage: $0 <vX.Y.Z>" >&2; exit 2; }

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# 1. Try CHANGELOG.md.
if [[ -f CHANGELOG.md ]]; then
  awk -v v="$VERSION" '
    /^## / { printing = 0 }
    /^## .*'"$VERSION"'/ { printing = 1; next }
    printing { print }
  ' CHANGELOG.md | sed '/./,$!d' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}'
  exit 0
fi

# 2. Fall back to git log since previous tag.
PREV_TAG="$(git describe --tags --abbrev=0 "${VERSION}^" 2>/dev/null || true)"
echo "## What's changed"
echo
if [[ -n "$PREV_TAG" ]]; then
  echo "Since \`$PREV_TAG\`:"
  echo
  git log --pretty=format:"- %s (%h)" "$PREV_TAG..HEAD"
else
  echo "Initial release."
  echo
  git log --pretty=format:"- %s (%h)" -n 30
fi
echo
echo
echo "Full changelog: <https://github.com/jeffwang/qdesk/compare/${PREV_TAG:-HEAD}...$VERSION>"
