# keepquiet

keepquiet 在请求离开本机之前，把密钥和个人信息换成 `{{KIND_xxxxxxxx}}` 占位符。Claude Code、Codex CLI、Pi 通过 hook 或扩展自动调用它；模型看不到原文，也不能跳过脱敏。检测规则来自 [yetone/magpie](https://github.com/yetone/magpie) 的 `internal/redact`，不经过 magpie 网关。你已经在用这些 agent、又不想把真实密钥发给模型厂商时，用它。

## 如何安装

构建需要 Go 1.24 或更高版本。运行时只支持 Linux 与 macOS（使用 flock）。

```bash
git clone https://github.com/Bl0omZ/keepquiet.git
cd keepquiet
scripts/build.sh
```

成功时终端打印 `built <repo>/dist/keepquiet`。抽查时带上临时家目录，避免在 `~/.keepquiet` 落下文件：

```bash
KEEPQUIET_HOME=/tmp/keepquiet-demo ./dist/keepquiet version
printf 'sk-proj-not-a-real-key-0123456789abcdef\n' | KEEPQUIET_HOME=/tmp/keepquiet-demo ./dist/keepquiet mask
```

第一行应打印 `0.1.0`。第二行应把密钥换成 `{{API_KEY_xxxxxxxx}}`。

下面三个脚本都会先把二进制装到 `~/.keepquiet/bin/keepquiet`，并在该目录写入默认 `config.json`（权限 0600）。你应看到 `installed ~/.keepquiet/bin/keepquiet (config: ~/.keepquiet/config.json)`。

### Claude Code

```bash
scripts/install-claude.sh
```

脚本会校验插件、注册本地 marketplace `keepquiet-local`，再执行 `claude plugin install keepquiet@keepquiet-local`。结束时 `claude plugin list` 应列出 `keepquiet`。

### Codex CLI

```bash
scripts/install-codex.sh
```

若 `~/.codex/hooks.json` 不存在，脚本直接写入三个 hook，并打印「已写入」。若文件已存在，它会生成 `~/.codex/hooks.keepquiet.json`，你必须手动合并。然后启动 `codex`，执行 `/hooks`，审核并信任这 3 个 hook。未信任的 hook 会被跳过，密钥会原样发给模型。`codex exec` 可加 `--dangerously-bypass-hook-trust`。修改 `hooks.json` 后要重新信任。

### Pi

```bash
scripts/install-pi.sh
```

扩展会被复制到 `~/.pi/agent/extensions/keepquiet/`，终端打印该路径。下次启动 Pi 时自动加载。也可以 `pi install ./plugins/pi/keepquiet`，前提是 `keepquiet` 在 `PATH` 或 `~/.keepquiet/bin` 里。

Pi 查找二进制的顺序：`KEEPQUIET_BIN`，然后 `$KEEPQUIET_HOME/bin/keepquiet`，最后是 `PATH` 中的 `keepquiet`。

## 如何配置

配置文件是 `~/.keepquiet/config.json`。用 `KEEPQUIET_HOME` 改整个数据目录，用 `KEEPQUIET_CONFIG` 只改配置文件路径。未设置 `KEEPQUIET_HOME` 且 `~/.keepquiet` 不存在、但 `~/.agent-redact` 存在时，keepquiet 继续读取旧目录里的 vault，不会复制文件。安装脚本会创建 `~/.keepquiet`，之后不再走这条兼容路径。

未知字段会报错。配置损坏时 hook 按 fail-closed 处理。

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

| 字段 | 作用 |
|---|---|
| `secrets` / `personal` | 密钥与个人信息总开关。EMAIL、PHONE、ID_CARD、BANK_CARD 随 `personal` 开启，不能单独写进 `kinds`。 |
| `kinds` | 类别 ID，仅允许 `ssn` `passport` `iban` `birthday` `mac` `serial` `home` `userhost` `ip` `bucket`。写 `PHONE` 会报错。不想脱敏某一类时用 `actions`。`ip` 会跳过内网、回环和常见公共 DNS。 |
| `words` / `wordsAnyCase` / `allow` | 关键词黑名单（命中变为 `TERM`）与白名单（永不脱敏）。 |
| `rules` | 最多 32 条，每条写 `prefix` 或 `regex`（RE2）。`prefix` 长 3-64，后面至少 8 个 `[A-Za-z0-9_-]`。`regex` 不超过 300 字符，有捕获组时只替换第 1 组。 |
| `actions` | `mask`（默认）、`warn`（放行并提示）、`block`（脱敏并拦截）。 |
| `zhNames` | 默认关闭。打开后启用规则式中文姓名识别。不识别英文人名。 |
| `replace` | 把某类占位符换成固定文字。单向，不能还原。只对 Claude Code 与 Codex 生效。Pi 仍用占位符。 |
| `prompt` | 提示词含敏感数据时：`block`（默认）或 `warn`。只对 Claude Code 与 Codex 有效。 |
| `restoreToolInput` / `restoreDenyTools` | 工具执行前是否还原真值，以及永不还原的工具名。 |
| `claude.preToolDecision` | 与 `updatedInput` 一起返回的权限决定：`none`（默认）、`ask`、`allow`。非交互 `-p` 下 `ask` 会拒工具。 |
| `claude.displayRestore` | `MessageDisplay` 是否把占位符还原给你看。 |

没有 magpie 的 Shape、pf-serve 或本地 NER 模型。占位符格式不可配置。

## 各 agent 的行为

| Agent | 提示词 | 工具输入 | 工具输出 | hook 失败 |
|---|---|---|---|---|
| Claude Code | 拦截，不能改写 | `PreToolUse` 本地还原 | `PostToolUse` 改写为占位符 | fail-closed（`onFailure: block`） |
| Codex CLI | 拦截，不能改写 | `PreToolUse` 本地还原 | 不能改写输出，用已脱敏的 `block.reason` 交给模型 | 二进制缺失或进程被杀时 fail-open |
| Pi | 直接改写整个请求 | `tool_call` 还原 | 请求发出前已 `MaskJSON` | fail-closed（`ctx.abort()`） |

Claude 的 `MessageDisplay` 与 Pi 的 `message_end` 只影响本机显示和会话，不会把真值发回模型。同一值总是得到同一占位符（HMAC-SHA256，密钥在 `redact.key`）。映射写在 `vault.json`，权限 0600。

## 命令与环境变量

```bash
keepquiet mask            # stdin 文本 → 脱敏文本
keepquiet mask-json       # stdin JSON 请求体 → 脱敏（magpie MaskJSON）
keepquiet restore         # stdin 文本 → 还原占位符
keepquiet scan            # 只检测；发现敏感数据时 exit 1
keepquiet hook claude <Event>
keepquiet hook codex <Event>
keepquiet serve           # Pi 用的 stdio JSON
keepquiet vault path|clear
keepquiet config default
keepquiet version
```

| 变量 | 作用 |
|---|---|
| `KEEPQUIET_HOME` | 数据目录，默认 `~/.keepquiet` |
| `KEEPQUIET_CONFIG` | 配置文件路径，默认 `$KEEPQUIET_HOME/config.json` |
| `KEEPQUIET_BIN` | Pi 扩展使用的二进制路径 |
| `KEEPQUIET_RESTORE_DENY` | Pi 侧永不还原的工具名，逗号分隔 |

## 局限

1. Claude Code 与 Codex 不能改写提示词，只能拦截。被拦的提示不会发给模型，但 Claude Code 仍可能把它写入本地 transcript。
2. Claude / Codex 的 hook 不覆盖系统提示、`CLAUDE.md` / `AGENTS.md`、`@文件`、图片、托管工具。不要把密钥写进这些位置。Pi 改写整个 payload，没有这个缺口。
3. Codex 在二进制缺失或进程被系统杀掉时 fail-open，密钥会原样发出。keepquiet 自己会接住内部错误并输出 block。
4. Claude Code 的 `PostToolUseFailure` 不能改写，失败工具的报错可能含原文。
5. `replace` 对 Pi 无效。Pi 会改写含签名 thinking 的完整请求，继续使用占位符。
6. `vault.json` 是明文映射。用 `keepquiet vault clear` 清空。自行保护 `~/.keepquiet/`。
7. `example.com` 等保留域名的邮箱不会被脱敏。规则会漏报也会误报。

## 署名

- 检测与占位符：[yetone/magpie](https://github.com/yetone/magpie) `internal/redact`，MIT License，Copyright (c) 2026 yetone。原文见 `third_party/magpie/LICENSE`。
- vendored 提交：`c158b04`。其后同步了邮箱误判修复 `dce7fe6`（`git@host` 不当邮箱）与 `25341b4`（需真实邮件域名）。
- 本项目其余部分：MIT，见 `LICENSE`。
