#!/usr/bin/env bash
# Codex UserPromptSubmit: prompt with a fake secret must be blocked before any model request.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T="$(mktemp -d /tmp/ar-e2e-codex-prompt.XXXX)"
export HOME="$T/home" CODEX_HOME="$T/home/.codex" KEEPQUIET_HOME="$T/home/.keepquiet" MOCK_LOG="$T/mock.log"
mkdir -p "$CODEX_HOME" "$T/work"; : > "$MOCK_LOG"
FAKE="sk-proj-FAKE0nlyCXPROMPT$(date +%s)abcdefghijklmnopqrstuvwxyz0123"
"$ROOT/scripts/install-codex.sh" >/dev/null
PORT=$((20000 + RANDOM % 20000))
printf 'model = "mock-model"\nmodel_provider = "mock"\n[model_providers.mock]\nname = "mock"\nbase_url = "http://127.0.0.1:%s/v1"\nwire_api = "responses"\nenv_key = "MOCK_API_KEY"\n' $PORT > "$CODEX_HOME/config.toml"
python3 "$ROOT/tests/mock_responses.py" $PORT & MOCK=$!
trap 'kill $MOCK 2>/dev/null' EXIT
sleep 0.5; cd "$T/work"
MOCK_API_KEY=fake timeout 120 "${CODEX_BIN:-codex}" exec --dangerously-bypass-hook-trust --skip-git-repo-check "my key is $FAKE" > "$T/out.txt" 2>&1
echo "codex exit=$?"; grep -A4 "keepquiet" "$T/out.txt" | head -8 | sed 's/^/  | /'
reqs=$(grep -c "$FAKE" "$MOCK_LOG"); echo "requests containing secret: $reqs (total $(wc -l < "$MOCK_LOG"))"
n=$(grep -rl "$FAKE" "$CODEX_HOME" 2>/dev/null | wc -l); echo "INFO: files under CODEX_HOME containing the prompt secret: $n"
[ "$reqs" = 0 ] && echo "E2E CODEX PROMPT PASS" || echo "E2E CODEX PROMPT FAIL"
