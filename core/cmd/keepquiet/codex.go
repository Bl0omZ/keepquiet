package main

import (
	"encoding/json"
	"fmt"
)

// codexHook handles one Codex CLI hook call (same contract as claudeHook).
func codexHook(event string, in hookInput) (map[string]any, error) {
	e, err := newEngine()
	if err != nil {
		return nil, err
	}
	switch event {
	case "UserPromptSubmit":
		masked := e.maskText(in.Prompt)
		if len(e.hits) == 0 {
			return nil, nil
		}
		if e.masked() == 0 || (e.cfg.Prompt == "warn" && !e.blocked()) {
			return map[string]any{"systemMessage": "keepquiet: 提示词含敏感数据（" + e.summary() + "），按配置未拦截。"}, nil
		}
		if err := e.save(); err != nil {
			return nil, err
		}
		return map[string]any{
			"decision": "block",
			"reason": "keepquiet 已拦截：提示词含敏感数据（" + e.summary() + "）。Codex 的 hook 无法改写提示词。\n" +
				"如需继续，可发送下面的脱敏版本（占位符会在工具执行前于本地还原）：\n\n" + masked,
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
		// Codex only honours updatedInput together with "allow"; in Codex
		// that does not skip sandbox/approval checks (core registry.rs).
		return map[string]any{"hookSpecificOutput": hso{"hookEventName": "PreToolUse",
			"permissionDecision": "allow", "permissionDecisionReason": "keepquiet: 已在本地还原占位符",
			"updatedInput": nv}}, nil

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
		text, ok := nv.(string)
		if !ok {
			b, _ := json.Marshal(nv)
			text = string(b)
		}
		// Codex can't rewrite a built-in tool's output (updatedMCPToolOutput
		// is unsupported); a block's reason replaces the model-visible
		// result, so the masked output goes there.
		return map[string]any{
			"decision": "block",
			"reason":   "[keepquiet] 工具输出含敏感数据（" + e.summary() + "），以下为本地脱敏后的完整输出：\n" + text,
		}, nil
	}
	return nil, fmt.Errorf("unsupported Codex event %q", event)
}

func codexFailClosed(event string, err error) (map[string]any, int) {
	msg := "keepquiet 内部错误，已按 fail-closed 处理: " + err.Error()
	switch event {
	case "UserPromptSubmit", "PostToolUse":
		return map[string]any{"decision": "block", "reason": msg}, 0
	case "PreToolUse":
		return map[string]any{"hookSpecificOutput": hso{"hookEventName": "PreToolUse",
			"permissionDecision": "deny", "permissionDecisionReason": msg}}, 0
	}
	return nil, 0
}
