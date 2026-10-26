#!/usr/bin/env bash
# Install the Claude Code plugin from the local marketplace in this repo.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$ROOT/scripts/install-bin.sh"
[ -x "$ROOT/plugins/claude-code/plugins/agent-redact/bin/agent-redact" ] || "$ROOT/scripts/build.sh"
claude plugin validate "$ROOT/plugins/claude-code/plugins/agent-redact"
claude plugin marketplace add "$ROOT/plugins/claude-code"
claude plugin install agent-redact@agent-redact-local
claude plugin list
