package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/bl0omz/keepquiet/internal/redact"
)

// engine masks and restores with one config and the shared vault.
type engine struct {
	cfg  Config
	st   *store
	hits []redact.Hit
}

func newEngine() (*engine, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	st, err := openStore()
	if err != nil {
		return nil, err
	}
	return &engine{cfg: cfg, st: st}, nil
}

func (e *engine) opts() redact.Options {
	o := e.cfg.Options()
	o.Seen = func(h redact.Hit) { e.hits = append(e.hits, h) }
	return o
}

// maskText masks one string.
func (e *engine) maskText(s string) string {
	if !e.cfg.enabled() || s == "" {
		return s
	}
	out, _ := redact.Mask(s, e.opts())
	return e.cfg.replaceFixed(out)
}

// maskJSON masks a provider request body the way magpie's gateway does.
func (e *engine) maskJSON(b []byte) []byte {
	if !e.cfg.enabled() {
		return b
	}
	// Replace is not applied here: MaskJSON leaves signed blocks (thinking
	// with a signature) as they are, and rewriting text there would make the
	// vendor reject the request. Replace is for the Claude Code/Codex hooks.
	out, _ := redact.MaskJSON(b, e.opts())
	return out
}

// maskValue masks every string leaf of a decoded JSON value, keeping its
// shape (what Claude Code's updatedToolOutput needs).
func (e *engine) maskValue(v any) (any, bool) {
	return mapStrings(v, "", func(key, s string) string {
		if skipLeaf(key, s) {
			return s
		}
		if !e.cfg.enabled() {
			return s
		}
		out, _ := redact.MaskField(key, s, e.opts())
		return e.cfg.replaceFixed(out)
	})
}

// restoreValue puts real values back in every string leaf.
func (e *engine) restoreValue(v any) (any, bool) {
	return mapStrings(v, "", func(_, s string) string { return redact.Restore(s, false) })
}

// masked counts hits that changed the text (warn hits are sent as written).
func (e *engine) masked() int {
	n := 0
	for _, h := range e.hits {
		if h.Action != redact.Warn {
			n++
		}
	}
	return n
}

func (e *engine) blocked() bool {
	for _, h := range e.hits {
		if h.Action == redact.Block {
			return true
		}
	}
	return false
}

func (e *engine) warning() string {
	for _, h := range e.hits {
		if h.Action == redact.Warn {
			return "keepquiet: 敏感数据按 warn 配置原文放行（" + e.summary() + "）"
		}
	}
	return ""
}

// summary is "API_KEY×2, EMAIL×1" with no values in it.
func (e *engine) summary() string {
	c := map[string]int{}
	for _, h := range e.hits {
		k := h.Kind
		if h.Action == redact.Warn {
			k += "(warn)"
		} else if h.Action == redact.Block {
			k += "(block)"
		}
		c[k]++
	}
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s×%d", k, c[k])
	}
	return strings.Join(parts, ", ")
}

func (e *engine) save() error {
	if e.masked() == 0 {
		return nil
	}
	return e.st.save()
}

func mapStrings(v any, key string, f func(key, s string) string) (any, bool) {
	switch x := v.(type) {
	case string:
		y := f(key, x)
		return y, y != x
	case map[string]any:
		changed := false
		for k, val := range x {
			if nv, ch := mapStrings(val, k, f); ch {
				x[k] = nv
				changed = true
			}
		}
		return x, changed
	case []any:
		changed := false
		for i, val := range x {
			if nv, ch := mapStrings(val, key, f); ch {
				x[i] = nv
				changed = true
			}
		}
		return x, changed
	}
	return v, false
}

// skipLeaf leaves encoded payloads alone (an image read by a tool), where
// a "match" would only corrupt the data.
func skipLeaf(key, s string) bool {
	if strings.HasPrefix(s, "data:") {
		return true
	}
	if (key == "base64" || key == "data") && len(s) >= 256 && isBase64(s) {
		return true
	}
	return false
}

func isBase64(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' || c == '\n' || c == '\r') {
			return false
		}
	}
	return true
}

// withheld replaces every string leaf, for when masking failed and the
// output must not go through as it is.
func withheld(v any) any {
	out, _ := mapStrings(v, "", func(_, s string) string { return "[keepquiet: output withheld (internal error)]" })
	return out
}

// decodeJSON keeps numbers as written (json.Number), so a tool's large
// integers survive the round trip.
func decodeJSON(b []byte, v *any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}
