#!/usr/bin/env bash
# Install the Codex CLI hooks into ~/.codex/hooks.json (or print what to merge).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$ROOT/scripts/install-bin.sh"
H="${KEEPQUIET_HOME:-$HOME/.keepquiet}"
CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
mkdir -p "$CODEX_DIR"
OUT="$(sed "s#__KEEPQUIET_BIN__#$H/bin/keepquiet#g" "$ROOT/plugins/codex/hooks.json.tmpl")"
if [ -e "$CODEX_DIR/hooks.json" ]; then
  echo "$OUT" > "$CODEX_DIR/hooks.keepquiet.json"
  echo "已存在 $CODEX_DIR/hooks.json：请把 $CODEX_DIR/hooks.keepquiet.json 中的三个事件手动合并进去。"
else
  echo "$OUT" > "$CODEX_DIR/hooks.json"
  echo "已写入 $CODEX_DIR/hooks.json"
fi
echo "下一步：启动 codex，执行 /hooks 审核并信任这 3 个 hook（未信任的 hook 会被跳过）。"
