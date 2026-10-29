#!/usr/bin/env bash
# Install the binary to $KEEPQUIET_HOME/bin (default ~/.keepquiet/bin).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
[ -x "$ROOT/dist/keepquiet" ] || "$ROOT/scripts/build.sh"
H="${KEEPQUIET_HOME:-$HOME/.keepquiet}"
mkdir -p "$H/bin" && chmod 700 "$H"
install -m 0755 "$ROOT/dist/keepquiet" "$H/bin/keepquiet"
if [ ! -f "$H/config.json" ]; then
  CONFIG_TMP="$(mktemp "$H/config.XXXXXX")"
  trap 'rm -f "$CONFIG_TMP"' EXIT
  "$H/bin/keepquiet" config default > "$CONFIG_TMP"
  chmod 600 "$CONFIG_TMP"
  mv "$CONFIG_TMP" "$H/config.json"
fi
echo "installed $H/bin/keepquiet (config: $H/config.json)"
