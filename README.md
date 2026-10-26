# keepquiet

Claude Code、Codex CLI、Pi 的本地强制脱敏插件。本仓库是 `Bl0omZ/keepquiet`，CLI 二进制仍为 `agent-redact`。

敏感数据的检测与占位符算法取自 [yetone/magpie](https://github.com/yetone/magpie) 的 `internal/redact`（MIT，© 2026 yetone，vendored 于提交 `c158b04`，并同步了其后的邮箱误判修复 `dce7fe6`、`25341b4`）。本项目把这套逻辑从 magpie 网关里抽出来，编成一个独立的 Go 二进制 `agent-redact`，再由各 agent 的 **hook / 扩展事件**自动调用：

- 是否脱敏不由模型决定，模型也无法跳过；
- 不需要运行 magpie 网关。

> 本 README 描述的行为都在 Linux 上用 Claude Code 2.1.308、Codex CLI 0.164.0、Pi 1.4.0 对本地 mock API 做过端到端测试，测试只使用伪造的密钥（见「测试」一节）。

## 1. 工作原理

```
                 ┌──────────── 本机 ────────────┐
用户/工具输出 ──▶ │ hook: 检测 → {{KIND_xxxxxxxx}} │ ──▶ 模型厂商（只看到占位符）
                 │        映射存入 vault.json      │
工具执行     ◀── │ hook: 占位符 → 真实值（本地还原）│ ◀── 模型返回的 tool call
                 └────────────────────────────────┘
```

- **占位符**：`{{API_KEY_k3m9x2ab}}` 的形式，由 HMAC-SHA256(本机密钥, 值) 生成。同一个值总是得到同一个占位符，模型可以据此引用。
- **vault**：`~/.agent-redact/vault.json`，权限 0600，用 flock 加锁、原子写入；HMAC 密钥是 `~/.agent-redact/redact.key`。hook 每次调用都是独立进程，它们通过 vault 共享映射。
- **还原**：工具真正执行前，在本地把占位符换回真实值，例如模型写出 `curl -H "Authorization: {{API_KEY_…}}"` 时，实际执行的命令带的是真值。工具输出如果又含有敏感值，会再次被脱敏。

### 目录结构

| 路径 | 内容 |
|---|---|
| `core/internal/redact/` | magpie `internal/redact` 拷贝（基于 `c158b04` + 邮箱误判修复两笔，含其测试）+ 新增 `vault.go`（Export/Import，用于跨进程持久化） |
| `core/internal/piistub/` | 替代 magpie `internal/pii`（本地 NER 模型）的空实现 |
| `core/cmd/agent-redact/` | CLI：`hook claude|codex <Event>`、`serve`（Pi 用）、`mask/restore/scan/vault/config` |
| `plugins/claude-code/` | Claude Code 本地 marketplace + 插件（`hooks/hooks.json`） |
| `plugins/codex/hooks.json.tmpl` | Codex hooks 模板 |
| `plugins/pi/agent-redact/` | Pi 扩展（`index.ts`，常驻子进程 `agent-redact serve`） |
| `scripts/` | 构建与安装脚本 |
| `tests/` | mock API 与端到端脚本 |

## 2. 各 agent 的接入方式与覆盖范围

### Claude Code（插件，hooks）

| 事件 | 动作 |
|---|---|
| `UserPromptSubmit` | 提示词中含敏感数据时**拦截**（`decision:block` + `suppressOriginalPrompt`），在提示中给出脱敏后的版本，可复制后重新发送。Claude Code 的 hook **不能改写**提示词，只能拦截。 |
| `PreToolUse`（全部工具） | 用 `updatedInput` 把占位符还原为真实值 |
| `PostToolUse`（全部工具） | 用 `updatedToolOutput` 替换工具输出为脱敏版本，结构保持不变 |
| `MessageDisplay` | 仅影响终端显示：把占位符还原给用户看，发送给模型的内容不变 |

前三个事件都配置了 `onFailure: "block"`：hook 崩溃、超时或输出非法时按拦截处理（fail-closed）。

### Codex CLI（`~/.codex/hooks.json`）

| 事件 | 动作 |
|---|---|
| `UserPromptSubmit` | 同上，拦截。Codex 的 hook 同样不能改写提示词。 |
| `PreToolUse` | `permissionDecision:"allow"` + `updatedInput` 还原占位符。这里的 allow 不会绕过 Codex 自己的审批策略。 |
| `PostToolUse` | Codex 不支持改写输出，所以采用 `decision:"block"`：`reason` 会替换模型看到的工具结果，内容是「提示语 + 完整的脱敏输出」。模型会把它视为 hook 反馈，但数据是完整的（实测模型能拿到占位符并继续工作）。 |

Codex 的 hook 出错时默认放行（fail-open）。因此 `agent-redact` 内部会捕获所有错误和 panic，自己输出 block/deny。但如果二进制不存在，或进程被系统杀掉，仍然会放行。

### Pi（扩展）

| 事件 | 动作 |
|---|---|
| `before_provider_request` | 对**整个请求 payload**（系统提示、历史、工具结果、文件内容）做 magpie `MaskJSON`。出错或命中 `block` 动作时调用 `ctx.abort()` 中止请求（fail-closed）。 |
| `message_end` | 把助手消息里的占位符还原，本地会话与显示用真实值；下次请求时会重新脱敏。 |
| `tool_call` | 把工具参数中的占位符就地还原；还原失败则阻止工具执行。 |

Pi 能改写整个请求，提示词**直接脱敏**而不需要拦截，覆盖面最完整。

## 3. 安装

依赖 Go ≥ 1.24（仅构建时需要）。仅支持 Linux/macOS（使用 flock）。

```bash
cd keepquiet
scripts/build.sh                  # 生成 dist/agent-redact，并复制到 Claude 插件的 bin/
```

**Claude Code**

```bash
scripts/install-claude.sh
# 等价于：
# claude plugin marketplace add ./plugins/claude-code
# claude plugin install agent-redact@agent-redact-local
```

**Codex CLI**

```bash
scripts/install-codex.sh          # 写入 ~/.codex/hooks.json；已有该文件时生成 hooks.agent-redact.json 供手动合并
codex                             # 进入后执行 /hooks，审核并信任这 3 个 hook
```

> ⚠️ Codex 会**跳过未信任的 hook**。实测：不信任时密钥会原样发给模型。非交互场景（`codex exec`）可以加 `--dangerously-bypass-hook-trust`；修改 hooks.json 后需要重新信任。

**Pi**

```bash
scripts/install-pi.sh             # 复制到 ~/.pi/agent/extensions/agent-redact/，Pi 启动时自动加载
# 或者：pi install ./plugins/pi/agent-redact   （需确保 agent-redact 在 PATH 或 ~/.agent-redact/bin）
```

二进制的查找顺序：`AGENT_REDACT_BIN` → `$AGENT_REDACT_HOME/bin/agent-redact` → `PATH` 中的 `agent-redact`。

## 4. 配置

配置文件为 `~/.agent-redact/config.json`，路径可以用 `AGENT_REDACT_HOME` / `AGENT_REDACT_CONFIG` 覆盖。未知字段会报错，报错时 hook 一律按 fail-closed 处理。

```json
{
  "secrets": true,
  "personal": true,
  "kinds": { "ip": true, "userhost": true },
  "words": ["Nightjar", "内部项目代号"],
  "wordsAnyCase": true,
  "rules": [
    { "kind": "GW_KEY", "prefix": "gw-" },
    { "kind": "KIRO_TOKEN", "regex": "kiro_token=([A-Za-z0-9]{16,})" },
    { "kind": "TICKET", "regex": "\\bTK-\\d{6}\\b" }
  ],
  "actions": { "EMAIL": "warn", "PRIVATE_KEY": "block" },
  "allow": ["sk-public-demo"],
  "zhNames": true,
  "replace": { "EMAIL": "[邮箱已隐藏]" },
  "prompt": "block",
  "restoreToolInput": true,
  "restoreDenyTools": ["WebFetch", "WebSearch", "web_search", "web_fetch"],
  "claude": { "preToolDecision": "none", "displayRestore": true }
}
```

| 字段 | 说明 |
|---|---|
| `secrets` / `personal` / `kinds` / `words` / `wordsAnyCase` / `rules` / `actions` / `allow` | 与 magpie Settings → Privacy 一一对应（`internal/masking/masking.go`）。 |
| `kinds` | 键是**类别 ID**，不是占位符类型名：`ssn` `passport` `iban` `birthday` `mac` `serial` `home` `userhost` `ip` `bucket`。写其他键（如 `PHONE`）会报错。EMAIL / PHONE / ID_CARD / BANK_CARD 随 `personal` 一起开启，不能单独开关；不想脱敏某一类时用 `actions: {"EMAIL":"warn"}`。`ip` 规则跳过内网、回环和常见公共 DNS（如 8.8.8.8、114.114.114.114）。 |
| `rules` | 每项写 `prefix` 或 `regex`（RE2），最多 32 条。`prefix` 长 3–64，后面至少要有 8 个 `[A-Za-z0-9_-]` 字符才会命中；`regex` 不超过 300 字符，有捕获组时只替换第 1 组（如上例中 `kiro_token=` 保留），匹配值至少 4 字符，能匹配空串的正则（如 `.*`）会被拒绝。`kind` 中的非 ASCII 字符会被丢弃（`工单号 ticket` → `TICKET`）。 |
| `actions` | 按类型名设置：`mask`（默认）；`warn` 原样放行并提示；`block` 脱敏并拦截（Pi 中止请求，提示词拦截）。 |
| `words` / `allow` | 关键词黑名单（命中即替换为 `TERM`）/ 白名单（这些原文永不脱敏）。 |
| `zhNames` | 默认 false。为 true 时启用 magpie 的规则式中文姓名识别（`zhname.go`，如「客户陈建国」→ `{{PERSON_…}}`）。不开启时**不识别中文姓名**。 |
| `replace` | 按类型名把占位符换成固定文字，例如 `{"EMAIL":"[邮箱已隐藏]"}`。**单向**：被替换的值不能还原，工具里看到的也是该文字。只对 Claude Code / Codex 生效；Pi 改写整个 payload（含签名 thinking 块），仍使用占位符。文字长 1–64，不能含 `"` `\` 控制字符或 `{{`。 |
| `prompt` | 提示词含敏感数据时的处理：`block`（默认）或 `warn`（放行原文，只提示）。只对 Claude Code / Codex 有效。 |
| `restoreToolInput` | 工具执行前是否还原真实值，默认 true。设为 false 时，工具拿到的是占位符。 |
| `restoreDenyTools` | 不做还原的工具，防止真实值通过联网工具发出。 |
| `claude.preToolDecision` | 与 `updatedInput` 一同返回的权限决定：`none`（默认，沿用 Claude 的权限流程；实测 updatedInput 照样生效）、`ask`、`allow`。注意：`-p` 非交互模式下 `ask` 会导致工具被拒。 |
| `claude.displayRestore` | `MessageDisplay` 中是否把占位符还原给用户看。 |

不支持 magpie 的 `Shape`（形似假密钥）和 `Model`（本地 NER 模型）这两个选项。`PERSON` 只能靠 `zhNames` 打开的规则式中文姓名识别，英文人名不识别。占位符格式 `{{KIND_xxxxxxxx}}` 不可配置（还原依赖它）。

常用命令：

```bash
agent-redact scan < file          # 只检测；发现敏感数据时 exit 1
agent-redact mask < file
agent-redact restore < file
agent-redact vault path|clear
agent-redact config default
```

## 5. 局限（务必阅读）

1. **Claude Code / Codex 不能改写提示词**，只能拦截。被拦截的提示词：
   - 不会发给模型（已实测）；
   - 但 Claude Code 仍会把它写入本地 transcript（`~/.claude/projects/*.jsonl`，已实测），也可能进入输入历史。

   Codex 在被拦截后会在终端回显原文；实测 `CODEX_HOME` 下没有落盘。
2. **Claude/Codex 的 hook 只覆盖提示词与工具调用**。以下内容不经过 hook，会原样发送：
   - 系统提示；
   - `CLAUDE.md` / `AGENTS.md`；
   - `@文件` 引用（Claude 会直接内联文件内容）；
   - 图片；
   - 托管工具（如 web search）。

   不要把密钥写进这些位置。Pi 改写的是整个 payload，没有这个缺口。
3. Claude Code 的 `PostToolUseFailure`（工具失败时的输出）**不能被改写**，失败工具的错误信息可能含原文。
4. Codex 的 hook 默认 fail-open，并且需要信任（见安装一节）。工具结果以「hook 拦截反馈」的形式交给模型；超过约 2500 token 时，Codex 会把它落盘到临时目录，只给模型头尾预览，落盘内容已是脱敏后的。
5. 还原后的真实值会出现在：
   - 本地工具执行中；
   - 本地 transcript 中：Claude 会记录 hook 的 stdout，已实测；
   - Pi 的本地会话中。

   这些值不会进入发往模型的请求（三端端到端测试均验证为 0 次泄露）。
6. `vault.json` 是**明文**映射（0600），请自行保护 `~/.agent-redact/`；可以用 `agent-redact vault clear` 清空。
7. `example.com` 等保留域名的邮箱不会被脱敏，这是 magpie 的既有行为。所有检测都基于规则，存在漏报和误报。

## 6. 耗时

在本机（box）测量。数据为日志、代码和中文混合文本，每 40 行有一个密钥。每格 20 次，单位 ms，中位数/p95。Claude 与 Codex 结果接近。

| vault 条目 | 提示词 | 10KB 工具输出 | 100KB | 1MB |
|---|---|---|---|---|
| 0 | 4.3 / 5.5 | 8.0 / 8.9 | 50 / 58 | 411 / 481 |
| 1k | 5.8 / 7.0 | 9.7 / 11 | 47 / 51 | 439 / 583 |
| 10k | 19 / 22 | 23 / 26 | 64 / 74 | 440 / 489 |

Pi 用常驻进程（`serve`），vault 只加载一次。每次请求的耗时为：小请求 0.2ms，10KB 4ms，100KB 40ms，1MB 约 410ms。Pi 每轮都会重发完整历史，所以耗时随上下文总量增长。

耗时主要花在 RE2 正则扫描上，约 2.7MB/s；干净文本约 6.3MB/s。Go 基准测试见 `core/cmd/agent-redact/bench_test.go`，需设置 `AGENT_REDACT_BENCH_DIR`。

## 7. 测试

```bash
cd core && go test ./...                 # 105 个测试：magpie 原有 86（含两笔邮箱误判修复测试）+ 本项目 19（hook 处理器、fail-closed、serve、配置）
tests/e2e_claude.sh none                 # 真实 claude -p + mock Anthropic API
tests/e2e_claude_prompt.sh
tests/e2e_codex.sh                       # 真实 codex exec + mock Responses API
tests/e2e_codex_prompt.sh
tests/e2e_pi.sh                          # 真实 pi --print + mock Anthropic API
tests/e2e_pi_prompt.sh
```

端到端脚本的流程：
1. 模型先读取含伪造密钥的 `secret.env`；
2. 再把它看到的占位符写入 `out.txt`；
3. 断言：
   - mock 收到的全部请求中都没有真实密钥；
   - 请求中出现了占位符；
   - `out.txt` 是真实值，即本地还原生效。

## 8. 署名与许可

- 检测与占位符逻辑：[yetone/magpie](https://github.com/yetone/magpie) `internal/redact`，MIT License，Copyright (c) 2026 yetone。原文见 `third_party/magpie/LICENSE`。
- 对 vendored 代码的改动：
  - 新增 `vault.go`（含 `Count`）/ `vault_test.go`；
  - 测试文件的 import 改为本模块路径，`pii` 改为 `piistub`；
  - `main_test.go` 改写为本地 TestMain（不依赖 magpie 的 testenv）；
  - 自 `c158b04` 之后同步了两笔邮箱误判修复（`dce7fe6`：`git@host` 不当邮箱；`25341b4`：需真实邮件域名，拒绝 `xxx@Agents.md` / `x@pytest.fixture`），含对应测试与 `golang.org/x/net/publicsuffix` 依赖。

  其余文件保持原样。
- 本项目其余部分：MIT（见 `LICENSE`）。
