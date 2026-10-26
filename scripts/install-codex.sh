#!/usr/bin/env bash
# Install the Codex CLI hooks into ~/.codex/hooks.json (or print what to merge).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$ROOT/scripts/install-bin.sh"
H="${AGENT_REDACT_HOME:-$HOME/.agent-redact}"
CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
mkdir -p "$CODEX_DIR"
OUT="$(sed "s#__AGENT_REDACT_BIN__#$H/bin/agent-redact#g" "$ROOT/plugins/codex/hooks.json.tmpl")"
if [ -e "$CODEX_DIR/hooks.json" ]; then
  echo "$OUT" > "$CODEX_DIR/hooks.agent-redact.json"
  echo "已存在 $CODEX_DIR/hooks.json：请把 $CODEX_DIR/hooks.agent-redact.json 中的三个事件手动合并进去。"
else
  echo "$OUT" > "$CODEX_DIR/hooks.json"
  echo "已写入 $CODEX_DIR/hooks.json"
fi
echo "下一步：启动 codex，执行 /hooks 审核并信任这 3 个 hook（未信任的 hook 会被跳过）。"
