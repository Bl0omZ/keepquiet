package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// All values below are fake test fixtures.
const fakeKey = "sk-proj-FAKE0nlyUNITabcdefghijklmnopqrstuvwxyz0123456789"

var phRe = regexp.MustCompile(`\{\{API_KEY_[a-z2-7]{8}\}\}`)

func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "keepquiet-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", d)
	os.Setenv("KEEPQUIET_HOME", filepath.Join(d, ".keepquiet"))
	code := m.Run()
	os.RemoveAll(d)
	os.Exit(code)
}

func writeConfig(t *testing.T, cfg string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEEPQUIET_CONFIG", p)
}

func hook(t *testing.T, agent, event string, in any) (map[string]any, int) {
	t.Helper()
	b, _ := json.Marshal(in)
	var w bytes.Buffer
	code := runHook(agent, event, bytes.NewReader(b), &w)
	if w.Len() == 0 {
		return nil, code
	}
	var out map[string]any
	d := json.NewDecoder(&w)
	d.UseNumber()
	if err := d.Decode(&out); err != nil {
		t.Fatalf("bad hook output %q: %v", w.String(), err)
	}
	return out, code
}

func dump(v any) string { b, _ := json.Marshal(v); return string(b) }

// maskedPlaceholder runs a PostToolUse to learn the placeholder for fakeKey.
func maskedPlaceholder(t *testing.T) string {
	out, _ := hook(t, "claude", "PostToolUse", map[string]any{
		"tool_name": "Bash", "tool_input": map[string]any{"command": "cat .env"},
		"tool_response": map[string]any{"stdout": "KEY=" + fakeKey, "stderr": "", "interrupted": false, "isImage": false},
	})
	p := phRe.FindString(dump(out))
	if p == "" {
		t.Fatalf("no placeholder in %s", dump(out))
	}
	return p
}

func TestClaudePostToolUseMasksAndKeepsShape(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true}`)
	out, code := hook(t, "claude", "PostToolUse", map[string]any{
		"tool_name": "Bash", "tool_input": map[string]any{"command": "cat .env"},
		"tool_response": map[string]any{"stdout": "KEY=" + fakeKey + "\nn=12345678901234567890", "stderr": "", "interrupted": false, "isImage": false},
	})
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	s := dump(out)
	if strings.Contains(s, fakeKey) || !phRe.MatchString(s) {
		t.Fatalf("not masked: %s", s)
	}
	resp := out["hookSpecificOutput"].(map[string]any)["updatedToolOutput"].(map[string]any)
	for _, k := range []string{"stdout", "stderr", "interrupted", "isImage"} {
		if _, ok := resp[k]; !ok {
			t.Fatalf("shape lost key %s: %s", k, s)
		}
	}
}

func TestClaudePostToolUseCleanIsSilent(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true}`)
	out, code := hook(t, "claude", "PostToolUse", map[string]any{
		"tool_name": "Bash", "tool_response": map[string]any{"stdout": "hello", "stderr": ""},
	})
	if code != 0 || out != nil {
		t.Fatalf("expected no output, got %d %s", code, dump(out))
	}
}

func TestHooksMaskStructuredCredentials(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true}`)
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			secret := "OpaqueFakeStructuredToken12345" + agent
			out, code := hook(t, agent, "PostToolUse", map[string]any{
				"tool_name": "mcp__demo__credentials",
				"tool_response": map[string]any{"structuredContent": map[string]any{
					"access_token": secret, "token_id": "3f2b6c1e8a4d4f7e9b1a2c3d4e5f6a7b",
					"count": 1234567890123456789,
				}},
			})
			if code != 0 || out == nil || strings.Contains(dump(out), secret) {
				t.Fatalf("structured credential not masked: %s", dump(out))
			}
			if !strings.Contains(dump(out), "3f2b6c1e8a4d4f7e9b1a2c3d4e5f6a7b") || !strings.Contains(dump(out), "1234567890123456789") {
				t.Fatalf("identifier or number changed: %s", dump(out))
			}
		})
	}
}

func TestHooksWarnOnUnchangedToolOutput(t *testing.T) {
	writeConfig(t, `{"personal":true,"actions":{"EMAIL":"warn"}}`)
	for _, agent := range []string{"claude", "codex"} {
		out, code := hook(t, agent, "PostToolUse", map[string]any{
			"tool_name": "Bash", "tool_response": map[string]any{"stdout": "Contact: fake@acme-corp.cn"},
		})
		if code != 0 || out["systemMessage"] == nil || out["decision"] != nil || strings.Contains(dump(out), "fake@acme-corp.cn") {
			t.Fatalf("%s: expected a warning without changing the output: %s", agent, dump(out))
		}
	}
}

func TestHooksStopOnBlockedToolOutput(t *testing.T) {
	writeConfig(t, `{"secrets":true,"actions":{"PRIVATE_KEY":"block"}}`)
	const privateKey = "-----BEGIN PRIVATE KEY-----\nFAKE-only-test-data\n-----END PRIVATE KEY-----"
	for _, agent := range []string{"claude", "codex"} {
		out, code := hook(t, agent, "PostToolUse", map[string]any{
			"tool_name": "Bash", "tool_response": map[string]any{"stdout": privateKey},
		})
		if code != 0 || out["continue"] != false || out["stopReason"] == nil || strings.Contains(dump(out), privateKey) {
			t.Fatalf("%s: block must stop with a safe reason: %s", agent, dump(out))
		}
	}
}

func TestClaudePreToolUseRestores(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true}`)
	p := maskedPlaceholder(t)
	out, _ := hook(t, "claude", "PreToolUse", map[string]any{
		"tool_name": "Bash", "tool_input": map[string]any{"command": "echo " + p},
	})
	h := out["hookSpecificOutput"].(map[string]any)
	if h["updatedInput"].(map[string]any)["command"] != "echo "+fakeKey {
		t.Fatalf("not restored: %s", dump(out))
	}
	if _, ok := h["permissionDecision"]; ok {
		t.Fatalf("default preToolDecision=none must not set permissionDecision: %s", dump(out))
	}
}

func TestClaudePreToolUseDecisionAsk(t *testing.T) {
	writeConfig(t, `{"secrets":true,"claude":{"preToolDecision":"ask"}}`)
	p := maskedPlaceholder(t)
	out, _ := hook(t, "claude", "PreToolUse", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "echo " + p}})
	if out["hookSpecificOutput"].(map[string]any)["permissionDecision"] != "ask" {
		t.Fatalf("want ask: %s", dump(out))
	}
}

func TestClaudePreToolUseDenyListNotRestored(t *testing.T) {
	writeConfig(t, `{"secrets":true}`)
	p := maskedPlaceholder(t)
	out, _ := hook(t, "claude", "PreToolUse", map[string]any{"tool_name": "WebFetch", "tool_input": map[string]any{"url": "https://x.test/?k=" + p}})
	if out != nil {
		t.Fatalf("WebFetch must not be restored: %s", dump(out))
	}
}

func TestClaudeUserPromptSubmitBlocks(t *testing.T) {
	writeConfig(t, `{"secrets":true}`)
	out, _ := hook(t, "claude", "UserPromptSubmit", map[string]any{"prompt": "key " + fakeKey})
	s := dump(out)
	if out["decision"] != "block" || strings.Contains(s, fakeKey) || !phRe.MatchString(s) {
		t.Fatalf("bad block: %s", s)
	}
	if out["hookSpecificOutput"].(map[string]any)["suppressOriginalPrompt"] != true {
		t.Fatalf("missing suppressOriginalPrompt: %s", s)
	}
}

func TestClaudeUserPromptSubmitWarn(t *testing.T) {
	writeConfig(t, `{"secrets":true,"prompt":"warn"}`)
	out, _ := hook(t, "claude", "UserPromptSubmit", map[string]any{"prompt": "key " + fakeKey})
	if out["decision"] != nil || out["systemMessage"] == nil || strings.Contains(dump(out), fakeKey) {
		t.Fatalf("bad warn: %s", dump(out))
	}
}

func TestClaudeMessageDisplayRestores(t *testing.T) {
	writeConfig(t, `{"secrets":true}`)
	p := maskedPlaceholder(t)
	out, _ := hook(t, "claude", "MessageDisplay", map[string]any{"delta": "the key is " + p})
	if out["hookSpecificOutput"].(map[string]any)["displayContent"] != "the key is "+fakeKey {
		t.Fatalf("not restored: %s", dump(out))
	}
	writeConfig(t, `{"secrets":true,"claude":{"displayRestore":false}}`)
	if out, _ := hook(t, "claude", "MessageDisplay", map[string]any{"delta": "the key is " + p}); out != nil {
		t.Fatalf("displayRestore=false must be silent: %s", dump(out))
	}
}

func TestClaudeFailClosed(t *testing.T) {
	writeConfig(t, `{"secrets":true}`)
	var w bytes.Buffer
	runHook("claude", "PreToolUse", strings.NewReader("{not json"), &w)
	if !strings.Contains(w.String(), `"deny"`) {
		t.Fatalf("garbage PreToolUse should deny: %q", w.String())
	}
	writeConfig(t, `{"secrets":true,"bogus":1}`)
	out, _ := hook(t, "claude", "UserPromptSubmit", map[string]any{"prompt": "hi"})
	if out["decision"] != "block" {
		t.Fatalf("bad config should block prompt: %s", dump(out))
	}
	out, _ = hook(t, "claude", "PostToolUse", map[string]any{"tool_name": "Bash", "tool_response": map[string]any{"stdout": fakeKey}})
	if strings.Contains(dump(out), fakeKey) {
		t.Fatalf("bad config PostToolUse leaked: %s", dump(out))
	}
}

func TestCodexPostToolUseBlockCarriesMaskedOutput(t *testing.T) {
	writeConfig(t, `{"secrets":true}`)
	out, _ := hook(t, "codex", "PostToolUse", map[string]any{
		"tool_name": "Bash", "tool_input": map[string]any{"command": "cat .env"}, "tool_response": "KEY=" + fakeKey,
	})
	s := dump(out)
	if out["decision"] != "block" || strings.Contains(s, fakeKey) || !phRe.MatchString(out["reason"].(string)) {
		t.Fatalf("bad codex PostToolUse: %s", s)
	}
}

func TestCodexPreToolUseAllowsWithUpdatedInput(t *testing.T) {
	writeConfig(t, `{"secrets":true}`)
	p := maskedPlaceholder(t)
	out, _ := hook(t, "codex", "PreToolUse", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "echo " + p}})
	h := out["hookSpecificOutput"].(map[string]any)
	if h["permissionDecision"] != "allow" || h["updatedInput"].(map[string]any)["command"] != "echo "+fakeKey {
		t.Fatalf("bad codex PreToolUse: %s", dump(out))
	}
}

func TestCodexUserPromptSubmitBlocks(t *testing.T) {
	writeConfig(t, `{"secrets":true}`)
	out, _ := hook(t, "codex", "UserPromptSubmit", map[string]any{"prompt": "key " + fakeKey})
	if out["decision"] != "block" || strings.Contains(dump(out), fakeKey) {
		t.Fatalf("bad codex prompt: %s", dump(out))
	}
}

func TestCodexFailClosedOnGarbage(t *testing.T) {
	writeConfig(t, `{"secrets":true}`)
	for _, ev := range []string{"UserPromptSubmit", "PreToolUse", "PostToolUse"} {
		var w bytes.Buffer
		runHook("codex", ev, strings.NewReader("{oops"), &w)
		if !strings.Contains(w.String(), `"block"`) && !strings.Contains(w.String(), `"deny"`) {
			t.Fatalf("%s: garbage must block/deny, got %q", ev, w.String())
		}
	}
}

func TestServeMaskRequestAndRestore(t *testing.T) {
	writeConfig(t, `{"secrets":true}`)
	req := `{"id":1,"op":"maskRequest","payload":{"messages":[{"role":"user","content":"k=` + fakeKey + `"}]}}` + "\n"
	var w bytes.Buffer
	if err := serve(strings.NewReader(req), &w); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.String(), fakeKey) || !phRe.MatchString(w.String()) {
		t.Fatalf("serve maskRequest: %s", w.String())
	}
	p := phRe.FindString(w.String())
	w.Reset()
	if err := serve(strings.NewReader(`{"id":2,"op":"restore","value":{"cmd":"echo `+p+`"}}`+"\n"), &w); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.String(), fakeKey) {
		t.Fatalf("serve restore: %s", w.String())
	}
}
