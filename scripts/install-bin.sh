#!/usr/bin/env bash
# Install the binary to $AGENT_REDACT_HOME/bin (default ~/.agent-redact/bin).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
[ -x "$ROOT/dist/agent-redact" ] || "$ROOT/scripts/build.sh"
H="${AGENT_REDACT_HOME:-$HOME/.agent-redact}"
mkdir -p "$H/bin" && chmod 700 "$H"
install -m 0755 "$ROOT/dist/agent-redact" "$H/bin/agent-redact"
[ -f "$H/config.json" ] || { "$H/bin/agent-redact" config default > "$H/config.json"; chmod 600 "$H/config.json"; }
echo "installed $H/bin/agent-redact (config: $H/config.json)"
