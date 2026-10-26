/**
 * keepquiet for Pi (earendil-works/pi coding agent) — forced local redaction.
 *
 * Detection: yetone/magpie internal/redact (MIT, (c) 2026 yetone), run by the
 * keepquiet Go binary in `serve` mode (one long-lived child process).
 *
 *  - before_provider_request: the whole serialized provider payload (system
 *    prompt, history, tool results, tool definitions) is masked the way
 *    magpie's gateway masks a request body (redact.MaskJSON). Fail-closed: if
 *    masking fails, the run is aborted and an empty payload is returned.
 *  - message_end (assistant): placeholders are restored in the finalized
 *    message (text + tool call arguments), so the transcript, the screen and
 *    the tools see real values; the next request masks them again
 *    (same placeholder, the HMAC key is fixed).
 *  - tool_call: placeholders left in a tool's input are restored (backup for
 *    message_end); tools in restoreDenyTools are left alone.
 */
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";

type Resp = { id: number; error?: string; [k: string]: unknown };

const DENY_RESTORE = new Set(
	(process.env.KEEPQUIET_RESTORE_DENY ?? "web_search,web_fetch,WebSearch,WebFetch").split(",").filter(Boolean),
);

function binPath(): string {
	if (process.env.KEEPQUIET_BIN) return process.env.KEEPQUIET_BIN;
	const home = process.env.KEEPQUIET_HOME ?? join(homedir(), ".keepquiet");
	const p = join(home, "bin", "keepquiet");
	return existsSync(p) ? p : "keepquiet";
}

class Redactor {
	private child?: ChildProcessWithoutNullStreams;
	private next = 1;
	private pending = new Map<number, { resolve: (r: Resp) => void; reject: (e: Error) => void }>();

	private start(): ChildProcessWithoutNullStreams {
		if (this.child && this.child.exitCode === null && !this.child.killed) return this.child;
		const child = spawn(binPath(), ["serve"], { stdio: ["pipe", "pipe", "pipe"] });
		child.stderr.on("data", (d) => process.stderr.write(`[keepquiet] ${d}`));
		const fail = (e: Error) => {
			for (const p of this.pending.values()) p.reject(e);
			this.pending.clear();
			if (this.child === child) this.child = undefined;
		};
		child.on("error", fail);
		child.on("exit", (code) => fail(new Error(`keepquiet serve exited (${code})`)));
		createInterface({ input: child.stdout }).on("line", (line) => {
			let r: Resp;
			try {
				r = JSON.parse(line);
			} catch {
				return;
			}
			const p = this.pending.get(r.id);
			if (!p) return;
			this.pending.delete(r.id);
			if (r.error) p.reject(new Error(r.error));
			else p.resolve(r);
		});
		this.child = child;
		return child;
	}

	call(op: string, body: Record<string, unknown>, timeoutMs = 60_000): Promise<Resp> {
		const child = this.start();
		const id = this.next++;
		return new Promise<Resp>((resolve, reject) => {
			const t = setTimeout(() => {
				this.pending.delete(id);
				reject(new Error(`keepquiet ${op} timed out`));
			}, timeoutMs);
			this.pending.set(id, {
				resolve: (r) => (clearTimeout(t), resolve(r)),
				reject: (e) => (clearTimeout(t), reject(e)),
			});
			child.stdin.write(JSON.stringify({ id, op, ...body }) + "\n");
		});
	}

	stop() {
		this.child?.kill();
		this.child = undefined;
	}
}

export default function (pi: ExtensionAPI) {
	const r = new Redactor();

	pi.on("before_provider_request", async (event, ctx) => {
		try {
			const res = await r.call("maskRequest", { payload: event.payload });
			if (res.blocked) {
				ctx.abort();
				ctx.ui?.notify?.(`keepquiet: 请求含被设为 block 的敏感数据（${res.summary}），已中止`, "error");
				return {};
			}
			if ((res.masked as number) > 0) ctx.ui?.setStatus?.("keepquiet", `已脱敏 ${res.summary}`);
			return res.payload;
		} catch (e) {
			// A thrown error would let Pi send the original payload: fail closed.
			ctx.abort();
			ctx.ui?.notify?.(`keepquiet 脱敏失败，已中止请求: ${(e as Error).message}`, "error");
			return {};
		}
	});

	pi.on("message_end", async (event) => {
		const m = event.message as { role?: string };
		if (m?.role !== "assistant") return;
		try {
			const res = await r.call("restore", { value: event.message });
			if (res.changed) return { message: res.value as typeof event.message };
		} catch {
			// Restoring is display/tool convenience; the placeholder stays.
		}
		return;
	});

	pi.on("tool_call", async (event) => {
		if (DENY_RESTORE.has(event.toolName)) return;
		try {
			const res = await r.call("restore", { value: event.input });
			if (res.changed) {
				const input = event.input as Record<string, unknown>;
				for (const k of Object.keys(input)) delete input[k];
				Object.assign(input, res.value as Record<string, unknown>);
			}
		} catch (e) {
			return { block: true, reason: `keepquiet 还原参数失败: ${(e as Error).message}` };
		}
		return;
	});

	pi.on("session_shutdown", async () => r.stop());
}
