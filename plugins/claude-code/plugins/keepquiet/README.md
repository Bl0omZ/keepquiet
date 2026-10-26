# keepquiet (Claude Code plugin)

Forced local redaction for Claude Code via hooks. PostToolUse masks secrets and
personal data in every tool result (updatedToolOutput) before Claude sees it;
PreToolUse restores placeholders in tool input locally (updatedInput);
UserPromptSubmit blocks prompts that contain sensitive data (hooks cannot
rewrite prompts); MessageDisplay shows real values on screen only.
Detection is vendored from yetone/magpie internal/redact (MIT). See the
repository README (简体中文) for install, config and limitations.
