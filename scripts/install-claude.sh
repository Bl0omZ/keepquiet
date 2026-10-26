#!/usr/bin/env bash
# Install the Claude Code plugin from the local marketplace in this repo.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$ROOT/scripts/install-bin.sh"
[ -x "$ROOT/plugins/claude-code/plugins/keepquiet/bin/keepquiet" ] || "$ROOT/scripts/build.sh"
claude plugin validate "$ROOT/plugins/claude-code/plugins/keepquiet"
claude plugin marketplace add "$ROOT/plugins/claude-code"
claude plugin install keepquiet@keepquiet-local
claude plugin list
