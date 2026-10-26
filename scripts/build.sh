#!/usr/bin/env bash
# Build agent-redact and place the binary where the adapters expect it.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/core"
mkdir -p "$ROOT/dist"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$ROOT/dist/agent-redact" ./cmd/agent-redact
cp "$ROOT/dist/agent-redact" "$ROOT/plugins/claude-code/plugins/agent-redact/bin/agent-redact"
echo "built $ROOT/dist/agent-redact"
