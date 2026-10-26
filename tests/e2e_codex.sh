#!/usr/bin/env bash
# Real Codex CLI (codex exec) + keepquiet hooks, against a local mock Responses API. Fake secrets only.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CODEX="${CODEX_BIN:-codex}"
T="$(mktemp -d /tmp/ar-e2e-codex.XXXX)"
export HOME="$T/home" CODEX_HOME="$T/home/.codex" KEEPQUIET_HOME="$T/home/.keepquiet" MOCK_LOG="$T/mock.log"
mkdir -p "$CODEX_HOME" "$T/work"; : > "$MOCK_LOG"
FAKE="sk-proj-FAKE0nlyCODEX$(date +%s)abcdefghijklmnopqrstuvwxyz0123"
printf 'OPENAI_API_KEY=%s\n' "$FAKE" > "$T/work/secret.env"
"$ROOT/scripts/install-codex.sh" >/dev/null
PORT=$((20000 + RANDOM % 20000))
cat > "$CODEX_HOME/config.toml" <<TOML
model = "mock-model"
model_provider = "mock"
approval_policy = "never"
sandbox_mode = "danger-full-access"
[model_providers.mock]
name = "mock"
base_url = "http://127.0.0.1:$PORT/v1"
wire_api = "responses"
env_key = "MOCK_API_KEY"
TOML
python3 "$ROOT/tests/mock_responses.py" $PORT & MOCK=$!
trap 'kill $MOCK 2>/dev/null' EXIT
sleep 0.5
cd "$T/work"
MOCK_API_KEY=fake-not-real timeout 180 "$CODEX" exec ${HOOK_TRUST_ARG---dangerously-bypass-hook-trust} --skip-git-repo-check \
  "read secret.env and save the key to out.txt" > "$T/stdout.txt" 2> "$T/stderr.txt"
echo "codex exit=$?"
echo "--- stdout:"; tail -5 "$T/stdout.txt"
pass=1
if grep -q "$FAKE" "$MOCK_LOG"; then echo "FAIL: fake secret reached the mock API"; pass=0; else echo "OK: fake secret never reached the API ($(wc -l < "$MOCK_LOG") requests)"; fi
grep -qE '\{\{API_KEY_[a-z2-7]{8}\}\}' "$MOCK_LOG" && echo "OK: API saw placeholder" || { echo "FAIL: no placeholder in API traffic"; pass=0; }
if [ -f out.txt ] && [ "$(cat out.txt)" = "$FAKE" ]; then echo "OK: PreToolUse restored the real value locally (out.txt)"; else echo "FAIL: out.txt = $(cat out.txt 2>/dev/null || echo '<missing>')"; pass=0; fi
echo "artifacts: $T"
[ $pass = 1 ] && echo "E2E CODEX PASS" || echo "E2E CODEX FAIL"
