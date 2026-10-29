package redact_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bl0omz/keepquiet/internal/redact"
)

func TestMaskJSONGeminiToolResult(t *testing.T) {
	redact.Forget()
	const token = "OpaqueFakeGeminiToken1234567890"
	const key = "sk-proj-FAKEonlyGeminiabcdefghijklmnopqrstuvwxyz0123456789"
	input := `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"access_token":"` + token + `","url":"https://example.invalid/?key=` + key + `","count":1234567890123456789}}}]}]}`
	out, _ := redact.MaskJSON([]byte(input), redact.Options{Secrets: true, Personal: true})
	if !json.Valid(out) || strings.Contains(string(out), token) || strings.Contains(string(out), key) {
		t.Fatalf("tool result leaked: %s", out)
	}
	if !strings.Contains(string(out), `"name":"lookup"`) || !strings.Contains(string(out), "1234567890123456789") {
		t.Fatalf("protocol or count changed: %s", out)
	}
}
