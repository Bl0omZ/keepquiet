#!/usr/bin/env bash
# Build keepquiet and place the binary where the adapters expect it.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/core"
mkdir -p "$ROOT/dist"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$ROOT/dist/keepquiet" ./cmd/keepquiet
echo "built $ROOT/dist/keepquiet"
