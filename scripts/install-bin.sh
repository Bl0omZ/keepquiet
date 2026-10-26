#!/usr/bin/env bash
# Install the binary to $KEEPQUIET_HOME/bin (default ~/.keepquiet/bin).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
[ -x "$ROOT/dist/keepquiet" ] || "$ROOT/scripts/build.sh"
H="${KEEPQUIET_HOME:-$HOME/.keepquiet}"
mkdir -p "$H/bin" && chmod 700 "$H"
install -m 0755 "$ROOT/dist/keepquiet" "$H/bin/keepquiet"
[ -f "$H/config.json" ] || { "$H/bin/keepquiet" config default > "$H/config.json"; chmod 600 "$H/config.json"; }
echo "installed $H/bin/keepquiet (config: $H/config.json)"
