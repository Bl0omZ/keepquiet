package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// hookInput is the union of the fields keepquiet reads from Claude Code
// and Codex hook stdin.
type hookInput struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	Prompt        string          `json:"prompt"`
	Delta         string          `json:"delta"`
}

type hso = map[string]any

// claudeHook handles one Claude Code hook call. It returns the JSON to
// print (nil for none). err means it could not decide; the caller turns
// that into the event's fail-closed answer.
func claudeHook(event string, in hookInput) (map[string]any, error) {
	e, err := newEngine()
	if err != nil {
		return nil, err
	}
	// Every restorable placeholder contains an underscore; a replaced
	// host does not, so a delta with neither has nothing to do.
	if event == "MessageDisplay" && !strings.Contains(in.Delta, "_") && !e.hasReplacedDomain(in.Delta) {
		return nil, nil
	}
	switch event {
	case "UserPromptSubmit":
		masked := e.maskText(in.Prompt)
		if len(e.hits) == 0 {
			return nil, nil
		}
		// There is no updatedPrompt: the prompt goes as written or is
		// blocked. A prompt whose only finding is a replaced host goes as
		// written, host and all (README "局限"); anything else found still
		// follows prompt=block/warn.
		if !e.sensitiveHit() {
			return nil, nil
		}
		if e.masked() == 0 || (e.cfg.Prompt == "warn" && !e.blocked()) {
			return map[string]any{"systemMessage": "keepquiet: 提示词含敏感数据（" + e.summary() + "），按配置未拦截，原文已发送。"}, nil
		}
		if err := e.save(); err != nil {
			return nil, err
		}
		return map[string]any{
			"decision": "block",
			"reason": "keepquiet 已拦截：提示词含敏感数据（" + e.summary() + "）。Claude Code 的 hook 无法改写提示词，原文未发送给模型。\n" +
				"如需继续，可复制下面的脱敏版本重新发送（占位符会在工具执行前于本地还原）：\n\n" + masked,
			"hookSpecificOutput": hso{"hookEventName": "UserPromptSubmit", "suppressOriginalPrompt": true},
		}, nil

	case "PreToolUse":
		if len(in.ToolInput) == 0 {
			return nil, nil
		}
		var v any
		if err := decodeJSON(in.ToolInput, &v); err != nil {
			return nil, err
		}
		var nv any
		var changed bool
		if e.cfg.restoreToolInput() && !e.cfg.restoreDenied(in.ToolName) {
			nv, changed = e.restoreValue(v)
		} else {
			// Availability first for a denied tool: its host is put back so
			// a fetch or remote search can run, but every vault placeholder
			// stays masked. The real host goes to the external service.
			nv, changed = e.restoreDomainValue(v)
		}
		if !changed {
			return nil, nil
		}
		out := hso{"hookEventName": "PreToolUse", "updatedInput": nv}
		if d := e.cfg.Claude.PreToolDecision; d != "none" {
			out["permissionDecision"] = d
			out["permissionDecisionReason"] = "keepquiet: 已在本地还原工具参数中的占位符或域名"
		}
		return map[string]any{"hookSpecificOutput": out}, nil

	case "PostToolUse":
		if len(in.ToolResponse) == 0 {
			return nil, nil
		}
		var v any
		if err := decodeJSON(in.ToolResponse, &v); err != nil {
			return nil, err
		}
		nv, changed := e.maskValue(v)
		if !changed {
			if warning := e.warning(); warning != "" {
				return map[string]any{"systemMessage": warning}, nil
			}
			return nil, nil
		}
		if err := e.save(); err != nil {
			return nil, err
		}
		out := map[string]any{"hookSpecificOutput": hso{"hookEventName": "PostToolUse", "updatedToolOutput": nv}}
		if warning := e.warning(); warning != "" {
			out["systemMessage"] = warning
		}
		if e.blocked() {
			out["continue"] = false
			out["stopReason"] = "keepquiet: 工具输出命中 block 配置（" + e.summary() + "），已停止"
		}
		return out, nil

	case "MessageDisplay":
		if !e.cfg.displayRestore() || in.Delta == "" {
			return nil, nil
		}
		if r, _ := e.restoreValue(in.Delta); r.(string) != in.Delta {
			return map[string]any{"hookSpecificOutput": hso{"hookEventName": "MessageDisplay", "displayContent": r}}, nil
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported Claude Code event %q", event)
}

// claudeFailClosed is the answer when claudeHook failed.
func claudeFailClosed(event string, in hookInput, err error) (map[string]any, int) {
	msg := "keepquiet 内部错误，已按 fail-closed 处理: " + err.Error()
	switch event {
	case "UserPromptSubmit":
		return map[string]any{"decision": "block", "reason": msg,
			"hookSpecificOutput": hso{"hookEventName": "UserPromptSubmit", "suppressOriginalPrompt": true}}, 0
	case "PreToolUse":
		return map[string]any{"hookSpecificOutput": hso{"hookEventName": "PreToolUse",
			"permissionDecision": "deny", "permissionDecisionReason": msg}}, 0
	case "PostToolUse":
		var v any
		if decodeJSON(in.ToolResponse, &v) == nil {
			return map[string]any{"hookSpecificOutput": hso{"hookEventName": "PostToolUse", "updatedToolOutput": withheld(v)}}, 0
		}
		return nil, 2
	}
	return nil, 0 // MessageDisplay: display only, show the original
}
