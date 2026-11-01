package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDomains is the fictional host map a test puts in its config when
// it expects replacement. It is not the default.
const fixtureDomains = `"domainReplacements":[{"from":"northwind.cn","to":"harbor.cn"},{"from":"contoso.com","to":"lumen.com"},{"from":"fabrikam.net","to":"cedar.net"},{"from":"adventure-works.io","to":"ridge-works.io"}]`

// TestDefaultDomainReplacements pins that replacement is off until a local
// config sets hosts.
func TestDefaultDomainReplacements(t *testing.T) {
	c := defaultConfig()
	if len(c.DomainReplacements) != 0 {
		t.Fatalf("default domains: %s", dump(c.DomainReplacements))
	}

	// A config that omits the field loads with no replacements, so a host
	// is left as written.
	writeConfig(t, `{"secrets":true}`)
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Options().DomainReplacements) != 0 {
		t.Fatalf("omitted field loaded domains: %s", dump(got.DomainReplacements))
	}
	if e, err := newEngine(); err != nil {
		t.Fatal(err)
	} else if masked := e.maskText("https://contoso.com/x"); masked != "https://contoso.com/x" {
		t.Fatalf("default config replaced a host: %q", masked)
	}

	// A new field still has to be a host map, not a loose string swap.
	writeConfig(t, `{"domainReplacements":[{"from":"contoso.com","to":"not a host"}]}`)
	if _, err := loadConfig(); err == nil {
		t.Fatal("accepted an invalid replacement host")
	}

	// An empty list is the explicit opt-out.
	writeConfig(t, `{"secrets":true,"domainReplacements":[]}`)
	if e, err := newEngine(); err != nil {
		t.Fatal(err)
	} else if masked := e.maskText("https://contoso.com/x"); masked != "https://contoso.com/x" {
		t.Fatalf("empty domainReplacements still replaced: %q", masked)
	}
}

// TestDomainOnlyPromptPasses pins the boundary: UserPromptSubmit has no
// updatedPrompt, so a prompt whose only finding is a replaced host goes as
// written, for both agents and either prompt policy. The host is not
// hidden at this stage (README "局限").
func TestDomainOnlyPromptPasses(t *testing.T) {
	for _, cfg := range []string{`{"secrets":true,` + fixtureDomains + `}`, `{"secrets":true,"prompt":"warn",` + fixtureDomains + `}`} {
		writeConfig(t, cfg)
		for _, agent := range []string{"claude", "codex"} {
			for _, prompt := range []string{
				"please open https://contoso.com/x",
				"northwind.cn and api.contoso.com and fabrikam.net and adventure-works.io",
			} {
				if out, code := hook(t, agent, "UserPromptSubmit", map[string]any{"prompt": prompt}); code != 0 || out != nil {
					t.Fatalf("%s %s: a domain-only prompt must pass as written: %d %s", cfg, agent, code, dump(out))
				}
			}
		}
	}

	// A prompt with nothing to replace is not held back, and a longer
	// domain that only starts with contoso.com is not one.
	writeConfig(t, `{"secrets":true,`+fixtureDomains+`}`)
	for _, prompt := range []string{"hello, how are you?", "open https://contoso.com.cn/x"} {
		if out, code := hook(t, "claude", "UserPromptSubmit", map[string]any{"prompt": prompt}); code != 0 || out != nil {
			t.Fatalf("clean prompt %q blocked: %d %s", prompt, code, dump(out))
		}
	}
}

// TestDomainPromptSavesNothing: a replaced host is fixed text, not a
// placeholder to restore, so it is not a masked value and must not write a
// vault with nothing in it.
func TestDomainPromptSavesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KEEPQUIET_HOME", home)
	writeConfig(t, `{"secrets":true,`+fixtureDomains+`}`)
	e, err := newEngine()
	if err != nil {
		t.Fatal(err)
	}
	if got := e.maskText("open https://contoso.com/x for alice@contoso.com"); got != "open https://lumen.com/x for alice@lumen.com" {
		t.Fatalf("maskText = %q", got)
	}
	if len(e.hits) != 2 || e.masked() != 0 || e.sensitiveHit() {
		t.Fatalf("domain hits counted as masked (%d) or sensitive: %s", e.masked(), dump(e.hits))
	}
	if err := e.save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "vault.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a domain hit wrote a placeholder-less vault: %v", err)
	}
}

// TestDomainWithSensitivePromptFollowsPromptPolicy: a replaced host neither
// lifts nor adds to the policy of the other values in the prompt.
func TestDomainWithSensitivePromptFollowsPromptPolicy(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			writeConfig(t, `{"secrets":true,`+fixtureDomains+`}`)
			out, code := hook(t, agent, "UserPromptSubmit", map[string]any{
				"prompt": "key " + fakeKey + " for https://contoso.com/x",
			})
			s := dump(out)
			if code != 0 || out["decision"] != "block" || strings.Contains(s, fakeKey) || strings.Contains(s, "contoso.com") || !strings.Contains(s, "lumen.com") || !phRe.MatchString(s) {
				t.Fatalf("%s: a key with a replaced host must still block on the key: %d %s", agent, code, s)
			}

			// prompt=warn still lets the key through as written; the host
			// does not keep it blocked.
			writeConfig(t, `{"secrets":true,"prompt":"warn",`+fixtureDomains+`}`)
			out, code = hook(t, agent, "UserPromptSubmit", map[string]any{
				"prompt": "key " + fakeKey + " for https://contoso.com/x",
			})
			if code != 0 || out["decision"] != nil || out["systemMessage"] == nil || strings.Contains(dump(out), fakeKey) {
				t.Fatalf("%s: prompt=warn must warn on the key, not block on the host: %d %s", agent, code, dump(out))
			}

			// A warn action with the default prompt=block warns as it did
			// before; the host does not turn it into a block.
			writeConfig(t, `{"personal":true,"actions":{"EMAIL":"warn"},`+fixtureDomains+`}`)
			out, code = hook(t, agent, "UserPromptSubmit", map[string]any{
				"prompt": "mail bob@gmail.com about https://contoso.com/x",
			})
			if code != 0 || out["decision"] != nil || out["systemMessage"] == nil || strings.Contains(dump(out), "bob@gmail.com") {
				t.Fatalf("%s: a warn email with a replaced host must warn, not block: %d %s", agent, code, dump(out))
			}
		})
	}
}

func TestClaudeDomainToolAndDisplayRoundTrip(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true,`+fixtureDomains+`}`)
	out, code := hook(t, "claude", "PostToolUse", map[string]any{
		"tool_name": "Bash",
		"tool_response": map[string]any{
			"stdout": "see https://api.contoso.com/x and alice@contoso.com",
		},
	})
	s := dump(out)
	if code != 0 || strings.Contains(s, "contoso.com") || !strings.Contains(s, "api.lumen.com") || !strings.Contains(s, "alice@lumen.com") {
		t.Fatalf("tool output not replaced by host: %d %s", code, s)
	}

	out, _ = hook(t, "claude", "PreToolUse", map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "curl https://api.lumen.com/x"},
	})
	cmd := out["hookSpecificOutput"].(map[string]any)["updatedInput"].(map[string]any)["command"]
	if cmd != "curl https://api.contoso.com/x" {
		t.Fatalf("tool input host not restored locally: %s", dump(out))
	}

	out, _ = hook(t, "claude", "MessageDisplay", map[string]any{"delta": "visit https://api.lumen.com/x"})
	got := out["hookSpecificOutput"].(map[string]any)["displayContent"]
	if got != "visit https://api.contoso.com/x" {
		t.Fatalf("display host not restored: %s", dump(out))
	}
}

func TestDeniedToolRestoresHostButKeepsSecretPlaceholder(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true,`+fixtureDomains+`}`)
	p := maskedPlaceholder(t)
	for _, agent := range []string{"claude", "codex"} {
		for _, tool := range []string{"WebFetch", "WebSearch", "web_fetch", "web_search"} {
			out, _ := hook(t, agent, "PreToolUse", map[string]any{
				"tool_name":  tool,
				"tool_input": map[string]any{"url": "https://api.lumen.com/?key=" + p},
			})
			h := out["hookSpecificOutput"].(map[string]any)
			url := h["updatedInput"].(map[string]any)["url"]
			if url != "https://api.contoso.com/?key="+p {
				t.Fatalf("%s %s: want the host restored and the secret still a placeholder, got %s", agent, tool, dump(out))
			}
		}
		// A local tool that is not on the list is restored in full: both the
		// secret and the host.
		out, _ := hook(t, agent, "PreToolUse", map[string]any{
			"tool_name":  "Bash",
			"tool_input": map[string]any{"command": "curl https://api.lumen.com/?key=" + p},
		})
		cmd := out["hookSpecificOutput"].(map[string]any)["updatedInput"].(map[string]any)["command"]
		if cmd != "curl https://api.contoso.com/?key="+fakeKey {
			t.Fatalf("%s: a local tool must restore host and secret, got %s", agent, dump(out))
		}
	}
}

// TestServeDeniedToolRestoresHostButKeepsSecretPlaceholder: the Pi stdio path
// applies the same availability-first rule, and no tool name (display
// restore) still restores everything.
func TestServeDeniedToolRestoresHostButKeepsSecretPlaceholder(t *testing.T) {
	writeConfig(t, `{"secrets":true,"personal":true,`+fixtureDomains+`}`)
	p := maskedPlaceholder(t)
	value := `{"url":"https://api.lumen.com/?key=` + p + `"}`
	for _, c := range []struct {
		tool, want string
	}{
		{"web_fetch", `"url":"https://api.contoso.com/?key=` + p + `"`},
		{"bash", `"url":"https://api.contoso.com/?key=` + fakeKey + `"`},
	} {
		req := `{"id":1,"op":"restore","toolName":"` + c.tool + `","value":` + value + `}` + "\n"
		var w bytes.Buffer
		if err := serve(strings.NewReader(req), &w); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(w.String(), c.want) {
			t.Fatalf("%s: got %s", c.tool, w.String())
		}
	}
	var w bytes.Buffer
	if err := serve(strings.NewReader(`{"id":2,"op":"restore","value":`+value+`}`+"\n"), &w); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.String(), `"url":"https://api.contoso.com/?key=`+fakeKey+`"`) {
		t.Fatalf("display restore must put both back: %s", w.String())
	}
}

func TestRestoreCommandPutsDomainsBack(t *testing.T) {
	writeConfig(t, `{"secrets":true,`+fixtureDomains+`}`)
	if got := runTextWithIO(t, "restore", "open https://api.lumen.com and mail alice@lumen.com"); got != "open https://api.contoso.com and mail alice@contoso.com" {
		t.Fatalf("restore command = %q", got)
	}
	if got := runTextWithIO(t, "mask", "open https://api.contoso.com and mail alice@contoso.com"); strings.Contains(got, "contoso.com") || !strings.Contains(got, "alice@lumen.com") {
		t.Fatalf("mask command = %q", got)
	}
}

// runTextWithIO runs a stdin/stdout command with test input.
func runTextWithIO(t *testing.T, op, in string) string {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	go func() {
		_, _ = io.WriteString(inW, in)
		_ = inW.Close()
	}()
	code := runText(op)
	_ = outW.Close()
	b, _ := io.ReadAll(outR)
	if code != 0 {
		t.Fatalf("%s exited %d: %s", op, code, b)
	}
	return string(b)
}

// TestClaudePluginHooksRunWithoutArgs checks the Cursor-compatible command
// form: the event is part of the command, with no args array for a loader
// to drop, and run.sh still gets it.
func TestClaudePluginHooksRunWithoutArgs(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(root, "plugins", "claude-code", "plugins", "keepquiet")
	b, err := os.ReadFile(filepath.Join(plugin, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string   `json:"type"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("KEEPQUIET_HOME", home)
	bin := filepath.Join(home, "bin", "keepquiet")
	if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
		t.Fatal(err)
	}
	// A stand-in that shows what run.sh passes it, rather than a real
	// keepquiet that would need a config and a vault.
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'arg:%s\\n' \"$@\"\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "MessageDisplay"} {
		groups, ok := doc.Hooks[event]
		if !ok || len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s: bad hooks.json shape: %s", event, string(b))
		}
		h := groups[0].Hooks[0]
		want := `"${CLAUDE_PLUGIN_ROOT}/run.sh" hook claude ` + event
		if h.Command != want || len(h.Args) != 0 {
			t.Fatalf("%s: command %q args %q, want command %q and no args", event, h.Command, h.Args, want)
		}
		cmd := exec.Command("sh", "-c", h.Command)
		cmd.Env = append(os.Environ(), "CLAUDE_PLUGIN_ROOT="+plugin, "KEEPQUIET_HOME="+home)
		cmd.Stdin = strings.NewReader(`{"hook_event_name":"` + event + `"}`)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s: command failed: %v\n%s", event, err, out.String())
		}
		wantOut := "arg:hook\narg:claude\narg:" + event + "\n"
		if !strings.HasPrefix(out.String(), wantOut) {
			t.Fatalf("%s: run.sh got wrong arguments: %q", event, out.String())
		}
		if strings.Contains(out.String(), "usage:") {
			t.Fatalf("%s: binary printed usage: %s", event, out.String())
		}
	}
}
