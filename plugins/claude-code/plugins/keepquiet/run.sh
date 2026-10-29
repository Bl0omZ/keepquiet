#!/usr/bin/env bash
set -euo pipefail
exec "${KEEPQUIET_HOME:-$HOME/.keepquiet}/bin/keepquiet" "$@"
