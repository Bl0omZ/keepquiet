package main

import (
	"encoding/json"
	"fmt"
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
	switch event {
	case "UserPromptSubmit":
		masked := e.maskText(in.Prompt)
		if e.masked() == 0 && len(e.hits) == 0 {
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
		if !e.cfg.restoreToolInput() || e.cfg.restoreDenied(in.ToolName) || len(in.ToolInput) == 0 {
			return nil, nil
		}
		var v any
		if err := decodeJSON(in.ToolInput, &v); err != nil {
			return nil, err
		}
		nv, changed := e.restoreValue(v)
		if !changed {
			return nil, nil
		}
		out := hso{"hookEventName": "PreToolUse", "updatedInput": nv}
		if d := e.cfg.Claude.PreToolDecision; d != "none" {
			out["permissionDecision"] = d
			out["permissionDecisionReason"] = "keepquiet: 已在本地还原工具参数中的占位符"
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
			return nil, nil
		}
		if err := e.save(); err != nil {
			return nil, err
		}
		return map[string]any{"hookSpecificOutput": hso{"hookEventName": "PostToolUse", "updatedToolOutput": nv}}, nil

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
