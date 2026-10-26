// Command agent-redact masks secrets and personal data before they reach a
// model vendor, for Claude Code / Codex CLI hooks and the Pi extension. The
// detection is magpie's internal/redact (MIT, (c) 2026 yetone), vendored.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/bl0omz/agent-redact/internal/redact"
)

const version = "0.1.0"

func usage() {
	fmt.Fprint(os.Stderr, `agent-redact `+version+` — local redaction for coding agents (detection: yetone/magpie internal/redact, MIT)

usage:
  agent-redact mask            stdin text → masked text
  agent-redact mask-json       stdin JSON request body → masked (magpie MaskJSON)
  agent-redact restore         stdin text → placeholders restored
  agent-redact scan            stdin text → report kinds found; exit 1 if any
  agent-redact hook claude <Event>   Claude Code hook (UserPromptSubmit|PreToolUse|PostToolUse|MessageDisplay)
  agent-redact hook codex <Event>    Codex CLI hook (UserPromptSubmit|PreToolUse|PostToolUse)
  agent-redact serve           line-delimited JSON on stdio (Pi extension)
  agent-redact vault path|clear
  agent-redact config default  print the default config
  agent-redact version
env: AGENT_REDACT_HOME (default ~/.agent-redact), AGENT_REDACT_CONFIG
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "hook":
		if len(os.Args) < 3 {
			usage()
			os.Exit(2)
		}
		event := ""
		if len(os.Args) > 3 {
			event = os.Args[3]
		}
		os.Exit(runHook(os.Args[2], event, os.Stdin, os.Stdout))
	case "serve":
		if err := serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "agent-redact:", err)
			os.Exit(1)
		}
	case "mask", "mask-json", "restore", "scan":
		os.Exit(runText(os.Args[1]))
	case "vault":
		st, err := openStore()
		if err == nil && len(os.Args) > 2 && os.Args[2] == "clear" {
			err = st.clear()
		} else if err == nil {
			fmt.Println(st.vaultPath())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent-redact:", err)
			os.Exit(1)
		}
	case "config":
		b, _ := json.MarshalIndent(defaultConfig(), "", "  ")
		fmt.Println(string(b))
	case "version", "--version", "-v":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func runText(op string) int {
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-redact:", err)
		return 1
	}
	e, err := newEngine()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-redact:", err)
		return 1
	}
	switch op {
	case "mask":
		os.Stdout.WriteString(e.maskText(string(in)))
	case "mask-json":
		os.Stdout.Write(e.maskJSON(in))
	case "restore":
		os.Stdout.WriteString(redact.Restore(string(in), false))
		return 0
	case "scan":
		redact.Preview(string(in), e.opts())
		if len(e.hits) == 0 {
			fmt.Println("clean")
			return 0
		}
		fmt.Println(e.summary())
		return 1
	}
	if err := e.save(); err != nil {
		fmt.Fprintln(os.Stderr, "agent-redact:", err)
		return 1
	}
	return 0
}

// runHook never lets an error or panic become a plain non-zero exit: Codex
// fails open on those, so each event gets its fail-closed answer instead.
func runHook(agent, event string, r io.Reader, w io.Writer) (code int) {
	var in hookInput
	fail := func(err error) {
		var out map[string]any
		switch agent {
		case "claude":
			out, code = claudeFailClosed(event, in, err)
		default:
			out, code = codexFailClosed(event, err)
		}
		if out != nil {
			_ = json.NewEncoder(w).Encode(out)
		}
		if code == 2 {
			fmt.Fprintln(os.Stderr, "agent-redact:", err)
		}
	}
	defer func() {
		if p := recover(); p != nil {
			fail(fmt.Errorf("panic: %v", p))
		}
	}()
	b, err := io.ReadAll(r)
	if err == nil {
		err = json.Unmarshal(b, &in)
	}
	if event == "" {
		event = in.HookEventName
	}
	if err != nil {
		fail(fmt.Errorf("bad hook input: %w", err))
		return code
	}
	var out map[string]any
	switch agent {
	case "claude":
		out, err = claudeHook(event, in)
	case "codex":
		out, err = codexHook(event, in)
	default:
		err = fmt.Errorf("unknown agent %q", agent)
	}
	if err != nil {
		fail(err)
		return code
	}
	if out != nil {
		if err := json.NewEncoder(w).Encode(out); err != nil {
			fail(err)
		}
	}
	return 0
}
