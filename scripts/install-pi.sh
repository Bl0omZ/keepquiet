#!/usr/bin/env bash
# Install the Pi extension as a user extension directory.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$ROOT/scripts/install-bin.sh"
D="${PI_CODING_AGENT_DIR:-$HOME/.pi/agent}/extensions/agent-redact"
mkdir -p "$D"
cp "$ROOT/plugins/pi/agent-redact/index.ts" "$ROOT/plugins/pi/agent-redact/package.json" "$D/"
echo "已安装到 $D（Pi 启动时自动加载）"
