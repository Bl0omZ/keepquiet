package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bl0omz/keepquiet/internal/redact"
)

// All values below are fake test fixtures.

func TestCustomRulesMaskAndRestore(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true,
		"words":["Nightjar"],"wordsAnyCase":true,
		"rules":[{"kind":"GW_KEY","prefix":"gw-"},{"kind":"KIRO_TOKEN","regex":"kiro_token=([A-Za-z0-9]{16,})"}]}`)
	e, err := newEngine()
	if err != nil {
		t.Fatal(err)
	}
	in := "key gw-FAKEabcdef12345678 tok kiro_token=FAKEabcdefgh123456 project NIGHTJAR"
	out := e.maskText(in)
	for _, want := range []string{`\{\{GW_KEY_[a-z2-7]{8}\}\}`, `kiro_token=\{\{KIRO_TOKEN_[a-z2-7]{8}\}\}`, `\{\{TERM_[a-z2-7]{8}\}\}`} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("%s not in %q", want, out)
		}
	}
	if strings.Contains(out, "FAKE") || strings.Contains(out, "NIGHTJAR") {
		t.Fatalf("value left in %q", out)
	}
	if got := redact.Restore(out, false); got != in {
		t.Fatalf("restore = %q, want %q", got, in)
	}
}

func TestReplaceFixedText(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true,"replace":{"EMAIL":"[邮箱已隐藏]"}}`)
	out, _ := hook(t, "claude", "PostToolUse", map[string]any{
		"tool_name": "Bash", "tool_input": map[string]any{"command": "cat x"},
		"tool_response": map[string]any{"stdout": "mail ops@acme-corp.cn key=" + fakeKey}})
	s := dump(out)
	if !strings.Contains(s, "[邮箱已隐藏]") || strings.Contains(s, "acme-corp") || strings.Contains(s, "{{EMAIL_") {
		t.Fatalf("email not replaced: %s", s)
	}
	if !phRe.MatchString(s) {
		t.Fatalf("other kinds should keep placeholders: %s", s)
	}
}

func TestZhNames(t *testing.T) {
	in := "请把报告发给客户陈建国"
	writeConfig(t, `{"secrets":true,"personal":true}`)
	e, _ := newEngine()
	if got := e.maskText(in); got != in {
		t.Fatalf("zhNames off should not mask names: %q", got)
	}
	writeConfig(t, `{"secrets":true,"personal":true,"zhNames":true}`)
	e, _ = newEngine()
	got := e.maskText(in)
	if strings.Contains(got, "陈建国") || !strings.Contains(got, "{{PERSON_") {
		t.Fatalf("zhNames on: %q", got)
	}
}

func TestConfigRejects(t *testing.T) {
	for _, cfg := range []string{
		`{"kinds":{"PHONE":false}}`,
		`{"replace":{"email":"x"}}`,
		`{"replace":{"EMAIL":"a\"b"}}`,
		`{"replace":{"EMAIL":"{{EMAIL}}"}}`,
		`{"replace":{"EMAIL":""}}`,
		`{"rules":[{"kind":"X","regex":".*"}]}`,
	} {
		writeConfig(t, cfg)
		if _, err := loadConfig(); err == nil {
			t.Errorf("accepted %s", cfg)
		}
	}
	writeConfig(t, `{"kinds":{"ip":true,"userhost":true}}`)
	if _, err := loadConfig(); err != nil {
		t.Fatal(err)
	}
}

func TestHomeDirLegacyFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("KEEPQUIET_HOME", "")

	neu := filepath.Join(root, ".keepquiet")
	old := filepath.Join(root, ".agent-redact")
	if got := homeDir(); got != neu {
		t.Fatalf("neither dir: got %s, want %s", got, neu)
	}

	if err := os.Mkdir(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := homeDir(); got != old {
		t.Fatalf("legacy only: got %s, want %s", got, old)
	}

	if err := os.Mkdir(neu, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := homeDir(); got != neu {
		t.Fatalf("both dirs: got %s, want %s", got, neu)
	}

	if err := os.RemoveAll(old); err != nil {
		t.Fatal(err)
	}
	if got := homeDir(); got != neu {
		t.Fatalf("new only: got %s, want %s", got, neu)
	}

	// A non-directory at the old path is not a home.
	if err := os.RemoveAll(neu); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := homeDir(); got != neu {
		t.Fatalf("legacy file: got %s, want %s", got, neu)
	}

	custom := filepath.Join(root, "custom-home")
	t.Setenv("KEEPQUIET_HOME", custom)
	if got := homeDir(); got != custom {
		t.Fatalf("KEEPQUIET_HOME: got %s, want %s", got, custom)
	}
}

func TestSaveSkipsWhenNothingNew(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true}`)
	e, err := newEngine()
	if err != nil {
		t.Fatal(err)
	}
	e.maskText("KEY=" + fakeKey + "SAVE1")
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	st1, err := os.Stat(e.st.vaultPath())
	if err != nil {
		t.Fatal(err)
	}
	e.hits = nil
	e.maskText("again KEY=" + fakeKey + "SAVE1")
	if e.masked() == 0 || e.st.dirty() {
		t.Fatalf("masked=%d dirty=%v", e.masked(), e.st.dirty())
	}
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(e.st.vaultPath())
	if !os.SameFile(st1, st2) {
		t.Fatal("vault rewritten with nothing new")
	}
	e.hits = nil
	e.maskText("new KEY=" + fakeKey + "SAVE2")
	if !e.st.dirty() {
		t.Fatal("new value not dirty")
	}
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(e.st.vaultPath())
	if !strings.Contains(string(b), fakeKey+"SAVE2") {
		t.Fatal("new value not saved")
	}
}
