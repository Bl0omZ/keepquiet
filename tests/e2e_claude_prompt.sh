#!/usr/bin/env bash
# UserPromptSubmit: a prompt with a fake secret must be blocked before any API call.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T="$(mktemp -d /tmp/ar-e2e-claude-prompt.XXXX)"
export HOME="$T/home" KEEPQUIET_HOME="$T/home/.keepquiet" MOCK_LOG="$T/mock.log"
mkdir -p "$HOME" "$T/work"; : > "$MOCK_LOG"
FAKE="sk-proj-FAKE0nlyPROMPT$(date +%s)abcdefghijklmnopqrstuvwxyz0123"
PORT=$((20000 + RANDOM % 20000))
python3 "$ROOT/tests/mock_anthropic.py" $PORT & MOCK=$!
trap 'kill $MOCK 2>/dev/null' EXIT
sleep 0.5
claude plugin marketplace add "$ROOT/plugins/claude-code" >/dev/null && claude plugin install keepquiet@keepquiet-local >/dev/null
cd "$T/work"
ANTHROPIC_BASE_URL="http://127.0.0.1:$PORT" ANTHROPIC_API_KEY="sk-ant-FAKE-not-real" \
  timeout 120 claude -p "my key is $FAKE please use it" --permission-mode bypassPermissions --model claude-sonnet-4-5 > "$T/out.txt" 2>&1
echo "claude exit=$?"; sed 's/^/  | /' "$T/out.txt" | head -12
pass=1
grep -q "$FAKE" "$MOCK_LOG" && { echo "FAIL: prompt secret reached API"; pass=0; } || echo "OK: prompt secret never reached API ($(wc -l < "$MOCK_LOG") requests)"
grep -q "$FAKE" "$T/out.txt" && echo "INFO: block message contains the original prompt" || echo "OK: block message omits the original prompt (suppressOriginalPrompt)"
n=$(grep -rl "$FAKE" "$HOME/.claude" 2>/dev/null | wc -l); echo "INFO: files under ~/.claude still containing the prompt secret: $n"
[ $pass = 1 ] && echo "E2E CLAUDE PROMPT PASS" || echo "E2E CLAUDE PROMPT FAIL"
