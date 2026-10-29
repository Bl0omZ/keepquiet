#!/usr/bin/env bash
# Install the native binary, then the plugin from GitHub.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$ROOT/scripts/install-bin.sh"
claude plugin validate "$ROOT/plugins/claude-code/plugins/keepquiet"
claude plugin marketplace add Bl0omZ/keepquiet
claude plugin install keepquiet@keepquiet
claude plugin list
