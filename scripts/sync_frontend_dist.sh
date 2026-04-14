#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
FRONTEND_DIST="$PROJECT_ROOT/web-ui/dist"
EMBED_DIST="$PROJECT_ROOT/internal/web/dist"

if [[ ! -d "$FRONTEND_DIST" ]]; then
  echo "Frontend build output not found: $FRONTEND_DIST" >&2
  echo "Run 'npm run build' in web-ui first, or use 'make frontend-build'." >&2
  exit 1
fi

rm -rf "$EMBED_DIST"
mkdir -p "$EMBED_DIST"
cp -R "$FRONTEND_DIST"/. "$EMBED_DIST"/

echo "Embedded frontend prepared at $EMBED_DIST"
