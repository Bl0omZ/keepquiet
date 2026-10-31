# keepquiet (Claude Code plugin)

Forced local redaction for Claude Code via hooks. PostToolUse masks secrets and
personal data in every tool result (updatedToolOutput) before Claude sees it;
PreToolUse restores placeholders and replaced hosts in local tool input
(updatedInput). Tools in restoreDenyTools are availability-first: only the
configured replaced hosts are restored so the external request can run, while
keys and personal-data placeholders stay masked; the real hosts are sent to
the external tool service.
UserPromptSubmit blocks prompts that contain sensitive data other than a
replaced host (hooks cannot rewrite prompts, so a prompt naming only a
replaced host passes as written); MessageDisplay shows real values on screen
only.
Detection is vendored from yetone/magpie internal/redact (MIT). See the
repository README (简体中文) for install, config and limitations.
