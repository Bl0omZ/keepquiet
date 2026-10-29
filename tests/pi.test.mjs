import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";
import { spawnSync } from "node:child_process";
import register from "../plugins/pi/keepquiet/index.ts";

// Run after scripts/build.sh with Node 24; use the real stdio redactor.
test("Pi restores display text without restoring denied tool arguments", async () => {
	const home = mkdtempSync(join(tmpdir(), "keepquiet-pi-"));
	const env = { ...process.env };
	process.env.KEEPQUIET_HOME = home;
	process.env.KEEPQUIET_CONFIG = join(home, "config.json");
	process.env.KEEPQUIET_BIN = resolve(import.meta.dirname, "../dist/keepquiet");
	delete process.env.KEEPQUIET_RESTORE_DENY;
	writeFileSync(process.env.KEEPQUIET_CONFIG, JSON.stringify({ secrets: true, personal: true }));
	const handlers = {};
	register({ on: (name, handler) => { handlers[name] = handler; } });
	const warnings = [];
	const ctx = { abort: () => assert.fail("request aborted"), ui: { notify: (message, level) => warnings.push({ message, level }) } };
	const key = "sk-proj-FAKEonlyPiTestabcdefghijklmnopqrstuvwxyz0123456789";
	try {
		const masked = await handlers.before_provider_request({ payload: { messages: [{ role: "user", content: key }] } }, ctx);
		const placeholder = masked.messages[0].content;
		assert.match(placeholder, /^\{\{API_KEY_[a-z2-7]{8}\}\}$/);
		const message = { role: "assistant", content: [
			{ type: "text", text: placeholder },
			{ type: "toolCall", id: "t1", name: "web_fetch", arguments: { url: "https://example.invalid/?key=" + placeholder } },
		] };
		const result = await handlers.message_end({ message });
		const displayed = result?.message ?? message;
		assert.equal(displayed.content[0].text, key);
		const input = displayed.content[1].arguments;
		await handlers.tool_call({ toolName: "web_fetch", input });
		assert.equal(input.url, "https://example.invalid/?key=" + placeholder);
		const local = { command: "echo " + placeholder };
		await handlers.tool_call({ toolName: "bash", input: local });
		assert.equal(local.command, "echo " + key);
		writeFileSync(process.env.KEEPQUIET_CONFIG, JSON.stringify({ secrets: true, personal: true, restoreToolInput: false, words: ["NewPrivateProject"] }));
		const disabled = { command: "echo " + placeholder };
		await handlers.tool_call({ toolName: "bash", input: disabled });
		assert.equal(disabled.command, "echo " + placeholder);
		const updated = await handlers.before_provider_request({ payload: { messages: [{ role: "user", content: "NewPrivateProject" }] } }, ctx);
		assert.match(updated.messages[0].content, /^\{\{TERM_[a-z2-7]{8}\}\}$/);
		writeFileSync(process.env.KEEPQUIET_CONFIG, JSON.stringify({ secrets: true, restoreDenyTools: ["custom_remote"] }));
		const denied = { value: placeholder };
		await handlers.tool_call({ toolName: "custom_remote", input: denied });
		assert.equal(denied.value, placeholder);
		writeFileSync(process.env.KEEPQUIET_CONFIG, JSON.stringify({ secrets: true, restoreDenyTools: [] }));
		const allowed = { value: placeholder };
		await handlers.tool_call({ toolName: "web_fetch", input: allowed });
		assert.equal(allowed.value, key);
		writeFileSync(process.env.KEEPQUIET_CONFIG, JSON.stringify({ personal: true, actions: { EMAIL: "warn" } }));
		const warned = await handlers.before_provider_request({ payload: { messages: [{ role: "user", content: "fake@acme-corp.cn" }] } }, ctx);
		assert.equal(warned.messages[0].content, "fake@acme-corp.cn");
		assert.ok(warnings.some(w => w.level === "warning" && w.message.includes("EMAIL(warn)")));
		const clear = spawnSync(process.env.KEEPQUIET_BIN, ["vault", "clear"], { encoding: "utf8" });
		assert.equal(clear.status, 1, "clear must not report success while Pi retains the vault");
		assert.match(clear.stderr, /退出 Pi/);
	} finally {
		await handlers.session_shutdown();
		const clear = spawnSync(process.env.KEEPQUIET_BIN, ["vault", "clear"], { encoding: "utf8" });
		assert.equal(clear.status, 0, clear.stderr);
		process.env = env;
		rmSync(home, { recursive: true, force: true });
	}
});
