#!/usr/bin/env bash
# Real Pi CLI (pi --print) + keepquiet extension, anthropic baseUrl pointed at a local mock. Fake secrets only.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PI="${PI_BIN:-pi}"
T="$(mktemp -d /tmp/ar-e2e-pi.XXXX)"
export HOME="$T/home" PI_CODING_AGENT_DIR="$T/home/.pi/agent" KEEPQUIET_HOME="$T/home/.keepquiet" MOCK_LOG="$T/mock.log"
export KEEPQUIET_BIN="$ROOT/dist/keepquiet"
mkdir -p "$PI_CODING_AGENT_DIR" "$T/work"; : > "$MOCK_LOG"
FAKE="sk-proj-FAKE0nlyPI$(date +%s)abcdefghijklmnopqrstuvwxyz0123"
printf 'OPENAI_API_KEY=%s\n' "$FAKE" > "$T/work/secret.env"
"$ROOT/scripts/install-pi.sh" >/dev/null
PORT=$((20000 + RANDOM % 20000))
printf '{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:%s"}}}\n' $PORT > "$PI_CODING_AGENT_DIR/models.json"
python3 "$ROOT/tests/mock_anthropic.py" $PORT & MOCK=$!
trap 'kill $MOCK 2>/dev/null' EXIT
sleep 0.5; cd "$T/work"
ANTHROPIC_API_KEY="sk-ant-FAKE-not-real" timeout 180 "$PI" --print --no-session --model anthropic/claude-sonnet-4-5 \
  "read secret.env and save the key to out.txt" > "$T/stdout.txt" 2> "$T/stderr.txt"
echo "pi exit=$?"; echo "--- stdout:"; tail -3 "$T/stdout.txt"; tail -5 "$T/stderr.txt" | sed 's/^/  stderr| /'
pass=1
if grep -q "$FAKE" "$MOCK_LOG"; then echo "FAIL: fake secret reached the mock API"; pass=0; else echo "OK: fake secret never reached the API ($(wc -l < "$MOCK_LOG") requests)"; fi
grep -qE '\{\{API_KEY_[a-z2-7]{8}\}\}' "$MOCK_LOG" && echo "OK: API saw placeholder" || { echo "FAIL: no placeholder in API traffic"; pass=0; }
if [ -f out.txt ] && [ "$(cat out.txt)" = "$FAKE" ]; then echo "OK: tool_call restored the real value locally (out.txt)"; else echo "FAIL: out.txt = $(cat out.txt 2>/dev/null || echo '<missing>')"; pass=0; fi
grep -q "$FAKE" "$T/stdout.txt" && echo "INFO: final text shown with real value (message_end restore)" || echo "INFO: final text shown with placeholder"
echo "artifacts: $T"
[ $pass = 1 ] && echo "E2E PI PASS" || echo "E2E PI FAIL"
