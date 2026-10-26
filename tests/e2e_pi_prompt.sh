#!/usr/bin/env bash
# Pi: a prompt containing a fake secret is rewritten (masked) in the provider payload, not blocked.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T="$(mktemp -d /tmp/ar-e2e-pi-prompt.XXXX)"
export HOME="$T/home" PI_CODING_AGENT_DIR="$T/home/.pi/agent" KEEPQUIET_HOME="$T/home/.keepquiet" MOCK_LOG="$T/mock.log" KEEPQUIET_BIN="${KEEPQUIET_BIN:-$ROOT/dist/keepquiet}"
mkdir -p "$PI_CODING_AGENT_DIR" "$T/work"; : > "$MOCK_LOG"
FAKE="sk-proj-FAKE0nlyPIPROMPT$(date +%s)abcdefghijklmnopqrstuvwxyz0123"
"$ROOT/scripts/install-pi.sh" >/dev/null
PORT=$((20000 + RANDOM % 20000))
printf '{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:%s"}}}\n' $PORT > "$PI_CODING_AGENT_DIR/models.json"
python3 "$ROOT/tests/mock_anthropic.py" $PORT & MOCK=$!
trap 'kill $MOCK 2>/dev/null' EXIT
sleep 0.5; cd "$T/work"
ANTHROPIC_API_KEY="sk-ant-FAKE-not-real" timeout 120 "${PI_BIN:-pi}" --print --no-session --no-tools --model anthropic/claude-sonnet-4-5 "my key is $FAKE" > "$T/out.txt" 2>&1
echo "pi exit=$?"
pass=1
grep -q "$FAKE" "$MOCK_LOG" && { echo "FAIL: prompt secret reached API"; pass=0; } || echo "OK: prompt secret never reached API ($(wc -l < "$MOCK_LOG") requests)"
grep -qE 'my key is \{\{API_KEY_[a-z2-7]{8}\}\}' "$MOCK_LOG" && echo "OK: API saw masked prompt" || { echo "FAIL: masked prompt not seen"; pass=0; }
[ $pass = 1 ] && echo "E2E PI PROMPT PASS" || echo "E2E PI PROMPT FAIL"
