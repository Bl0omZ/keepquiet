package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/bl0omz/keepquiet/internal/redact"
)

// Config is ~/.keepquiet/config.json. The first block maps 1:1 onto
// magpie's Settings → Privacy (internal/masking/masking.go options()).
type Config struct {
	Secrets      bool              `json:"secrets"`
	Personal     bool              `json:"personal"`
	Kinds        map[string]bool   `json:"kinds,omitempty"`
	Words        []string          `json:"words,omitempty"`
	WordsAnyCase bool              `json:"wordsAnyCase,omitempty"`
	Rules        []redact.Rule     `json:"rules,omitempty"`
	Actions      map[string]string `json:"actions,omitempty"`
	Allow        []string          `json:"allow,omitempty"`

	// ZhNames turns on magpie's rule-based Chinese name finder (zhname.go:
	// a surname plus a cue such as 客户/我叫/…先生). In magpie it only runs
	// beside the local NER model; here a model that finds nothing stands in
	// for it, so only the rules run. Off by default.
	ZhNames bool `json:"zhNames,omitempty"`
	// Replace sends every value of a kind as fixed text in place of its
	// {{KIND_xxxxxxxx}} placeholder. One-way: such a value is never restored
	// (tools get the fixed text too) and the model cannot tell two values of
	// the kind apart. Claude Code / Codex hooks only (not Pi's MaskJSON).
	Replace map[string]string `json:"replace,omitempty"`

	// Prompt is what UserPromptSubmit does with a prompt that has sensitive
	// data (Claude Code / Codex cannot rewrite a prompt): "block" (default)
	// or "warn" (let it through, only tell the user). A kind whose action is
	// "block" always blocks.
	Prompt string `json:"prompt,omitempty"`
	// RestoreToolInput puts real values back into a tool call's input
	// before the tool runs locally (default true).
	RestoreToolInput *bool `json:"restoreToolInput,omitempty"`
	// RestoreDenyTools are tools whose input is never restored (values
	// would leave the machine through them).
	RestoreDenyTools []string `json:"restoreDenyTools,omitempty"`
	// Claude-only knobs.
	Claude struct {
		// PreToolDecision is the permissionDecision sent with updatedInput:
		// "none" (default: keep Claude Code's own permission flow), "ask"
		// or "allow" (allow also skips the permission prompt).
		PreToolDecision string `json:"preToolDecision,omitempty"`
		// DisplayRestore shows real values on screen (MessageDisplay).
		DisplayRestore *bool `json:"displayRestore,omitempty"`
	} `json:"claude,omitempty"`
}

func defaultConfig() Config {
	c := Config{Secrets: true, Personal: true, Prompt: "block",
		RestoreDenyTools: []string{"WebFetch", "WebSearch", "web_search", "web_fetch"}}
	c.Claude.PreToolDecision = "none"
	return c
}

func (c Config) restoreToolInput() bool { return c.RestoreToolInput == nil || *c.RestoreToolInput }
func (c Config) displayRestore() bool {
	return c.Claude.DisplayRestore == nil || *c.Claude.DisplayRestore
}

func (c Config) restoreDenied(tool string) bool {
	for _, t := range c.RestoreDenyTools {
		if t == tool {
			return true
		}
	}
	return false
}

func (c Config) Options() redact.Options {
	// Shape (made-up keys) and Model (local PII model) are not supported:
	// see README "与 magpie 的差异".
	o := redact.Options{Secrets: c.Secrets, Personal: c.Personal, Kinds: c.Kinds, Words: c.Words,
		WordsAnyCase: c.WordsAnyCase, Rules: c.Rules, Actions: c.Actions, Allow: c.Allow}
	if c.ZhNames {
		o.Model = noModel
	}
	return o
}

// noModel finds nothing: set as Options.Model it only switches on the
// rule-based Chinese names that magpie runs beside its model.
func noModel(texts []string) [][]redact.Found { return make([][]redact.Found, len(texts)) }

// replacePlaceholder matches a placeholder and captures its kind.
var replacePlaceholder = regexp.MustCompile(`\{\{([A-Z][A-Z0-9_]*)_[a-z2-7]{8}\}\}`)

// replaceFixed swaps the placeholders of kinds in c.Replace for their
// fixed text. The text has no quote, backslash or control character
// (loadConfig), so it is the same written raw or inside JSON strings.
func (c Config) replaceFixed(s string) string {
	if len(c.Replace) == 0 || !strings.Contains(s, "{{") {
		return s
	}
	return replacePlaceholder.ReplaceAllStringFunc(s, func(p string) string {
		if t, ok := c.Replace[replacePlaceholder.FindStringSubmatch(p)[1]]; ok {
			return t
		}
		return p
	})
}

func (c Config) enabled() bool { return c.Secrets || c.Personal || len(c.Words) > 0 }

// homeDir is where config.json, redact.key, and vault.json live.
// KEEPQUIET_HOME wins when it is set. Otherwise the directory is ~/.keepquiet.
// If that directory does not exist and ~/.agent-redact does, the old directory
// is used so an existing vault keeps working. Files are not copied.
func homeDir() string {
	if d := os.Getenv("KEEPQUIET_HOME"); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		h = "."
	}
	neu := filepath.Join(h, ".keepquiet")
	if _, err := os.Stat(neu); os.IsNotExist(err) {
		old := filepath.Join(h, ".agent-redact")
		if st, err := os.Stat(old); err == nil && st.IsDir() {
			return old
		}
	}
	return neu
}

func configPath() string {
	if p := os.Getenv("KEEPQUIET_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(homeDir(), "config.json")
}

// loadConfig reads the config; a missing file is the defaults, a broken
// one is an error (the hooks then fail closed).
func loadConfig() (Config, error) {
	c := defaultConfig()
	b, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("config %s: %w", configPath(), err)
	}
	if c.Rules, err = redact.CheckRules(c.Rules); err != nil {
		return c, fmt.Errorf("config %s: rules: %w", configPath(), err)
	}
	switch c.Prompt {
	case "", "block":
		c.Prompt = "block"
	case "warn":
	default:
		return c, fmt.Errorf("config: prompt must be block or warn, not %q", c.Prompt)
	}
	switch c.Claude.PreToolDecision {
	case "":
		c.Claude.PreToolDecision = "none"
	case "none", "ask", "allow":
	default:
		return c, fmt.Errorf("config: claude.preToolDecision must be none, ask or allow")
	}
	for k := range c.Kinds {
		if !knownCategory(k) {
			return c, fmt.Errorf("config: kinds[%s]: not a category; use one of %s (EMAIL/PHONE/ID_CARD/BANK_CARD follow \"personal\"; use actions to warn instead)", k, categoryIDs())
		}
	}
	for k, t := range c.Replace {
		if !redact.KindName(k) {
			return c, fmt.Errorf("config: replace[%s]: not a placeholder kind (A-Z, 0-9, _)", k)
		}
		if t == "" || len(t) > 64 || strings.Contains(t, "{{") || strings.ContainsAny(t, "\"\\") || strings.IndexFunc(t, unicode.IsControl) >= 0 {
			return c, fmt.Errorf("config: replace[%s]: 1-64 characters, no quote, backslash, control character or {{", k)
		}
	}
	for k, a := range c.Actions {
		if a != redact.Warn && a != redact.Block && a != "" && a != "mask" {
			return c, fmt.Errorf("config: actions[%s] must be mask, warn or block", k)
		}
	}
	return c, nil
}

func knownCategory(id string) bool {
	for _, c := range redact.Categories {
		if c.ID == id {
			return true
		}
	}
	return false
}

func categoryIDs() string {
	ids := make([]string, len(redact.Categories))
	for i, c := range redact.Categories {
		ids[i] = c.ID
	}
	return strings.Join(ids, ", ")
}
