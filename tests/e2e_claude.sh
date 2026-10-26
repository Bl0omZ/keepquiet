#!/usr/bin/env bash
# Real Claude Code CLI + plugin, against a local mock Anthropic API. Fake secrets only.
# usage: tests/e2e_claude.sh [none|ask|allow]   (claude.preToolDecision)
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEC="${1:-none}"
T="$(mktemp -d /tmp/ar-e2e-claude.XXXX)"
export HOME="$T/home" AGENT_REDACT_HOME="$T/home/.agent-redact" MOCK_LOG="$T/mock.log"
mkdir -p "$HOME" "$T/work"
FAKE="sk-proj-FAKE0nlyTEST$(date +%s)abcdefghijklmnopqrstuvwxyz0123"
printf 'OPENAI_API_KEY=%s\n' "$FAKE" > "$T/work/secret.env"
mkdir -p "$AGENT_REDACT_HOME"
printf '{"secrets":true,"personal":true,"claude":{"preToolDecision":"%s","displayRestore":%s}}' "$DEC" "${DISPLAY_RESTORE:-true}" > "$AGENT_REDACT_HOME/config.json"
PORT=$((20000 + RANDOM % 20000))
python3 "$ROOT/tests/mock_anthropic.py" $PORT & MOCK=$!
trap 'kill $MOCK 2>/dev/null' EXIT
sleep 0.5
claude plugin marketplace add "$ROOT/plugins/claude-code" >/dev/null && claude plugin install agent-redact@agent-redact-local >/dev/null || { echo "plugin install failed"; exit 1; }
cd "$T/work"
ANTHROPIC_BASE_URL="http://127.0.0.1:$PORT" ANTHROPIC_API_KEY="sk-ant-FAKE-not-real" \
  timeout 180 claude -p "read secret.env and save the key to out.txt" \
  ${PERM_ARGS:---permission-mode bypassPermissions} --model claude-sonnet-4-5 --output-format text > "$T/stdout.txt" 2> "$T/stderr.txt"
echo "claude exit=$? (preToolDecision=$DEC)"
echo "--- stdout:"; cat "$T/stdout.txt"
pass=1
if grep -q "$FAKE" "$MOCK_LOG"; then echo "FAIL: fake secret reached the mock API"; pass=0; else echo "OK: fake secret never reached the API ($(wc -l < "$MOCK_LOG") requests logged)"; fi
if grep -qE '\{\{API_KEY_[a-z2-7]{8}\}\}' "$MOCK_LOG"; then echo "OK: API saw placeholder"; else echo "FAIL: no placeholder in API traffic"; pass=0; fi
if [ -f out.txt ] && [ "$(cat out.txt)" = "$FAKE" ]; then echo "OK: PreToolUse restored the real value locally (out.txt)"; else echo "FAIL/INFO: out.txt = $(cat out.txt 2>/dev/null || echo '<missing>')"; pass=0; fi
grep -q "$FAKE" "$T/stdout.txt" && echo "INFO: final text shown with real value (MessageDisplay restore)" || echo "INFO: final text shown with placeholder"
echo "artifacts: $T"
[ $pass = 1 ] && echo "E2E CLAUDE PASS" || echo "E2E CLAUDE FAIL"
