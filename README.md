# keepquiet

当你使用 Claude Code、Codex CLI 或 Pi 时，`keepquiet` 会在本地把发往模型的密钥、个人隐私和内部域名替换成 `{{KIND_xxxxxxxx}}` 占位符。当模型调用本机工具时，`keepquiet` 会在命令执行前把占位符还原成真实值，并将工具返回的输出再次脱敏。

检测与占位符算法来自 [yetone/magpie](https://github.com/yetone/magpie) 的 `internal/redact` 包。`keepquiet` 通过各客户端的本地 hook 或扩展直接运行，不需要部署 magpie 网关。

## 工作原理

一次完整的工具调用会经过以下四个步骤：

1. **出站脱敏。** 工具输出或请求进入本地 hook 时，`keepquiet` 将识别到的敏感值替换为 `{{API_KEY_xxxxxxxx}}` 这类占位符，并把配置的内部域名替换为假域名。
2. **本地保存映射。** `keepquiet` 使用本机密钥 `~/.keepquiet/redact.key` 计算 `HMAC-SHA256`，因此同一个敏感值在同一台机器上总是生成相同的占位符。占位符与真实值的映射保存在本机 `~/.keepquiet/vault.json` 中，文件权限为 `0600`。
3. **本机工具还原。** 模型在返回工具调用参数时会原样带上占位符。对于允许还原的本机工具，`keepquiet` 会在工具执行前把占位符和假域名还原成真实值，使本机命令正常运行。
4. **终端显示还原。** Claude Code 的 `MessageDisplay` 与 Pi 的 `message_end` 会在终端渲染时把占位符还原为你可见的真实值，但不会把真实值发回给模型。

## 安装与接入

构建二进制需要 Go 1.25 或更高版本。由于进程间加锁依赖 `flock`，运行时只支持 Linux 和 macOS。

### 1. 构建并验证二进制

克隆仓库并执行构建脚本：

```bash
git clone https://github.com/Bl0omZ/keepquiet.git
cd keepquiet
scripts/build.sh
```

构建成功后，终端会打印 `built <repo>/dist/keepquiet`。你可以指定一个临时目录来抽查脱敏效果，避免在 `~/.keepquiet` 下留下测试数据：

```bash
KEEPQUIET_HOME=/tmp/keepquiet-demo ./dist/keepquiet version
printf 'sk-proj-not-a-real-key-0123456789abcdef\n' | KEEPQUIET_HOME=/tmp/keepquiet-demo ./dist/keepquiet mask
```

第一条命令会输出 `0.1.1`。第二条命令会把测试密钥替换成 `{{API_KEY_xxxxxxxx}}`。

### 2. 接入你的客户端

下面三个安装脚本都会先把二进制安装到 `~/.keepquiet/bin/keepquiet`，并在首次安装时写入默认配置文件 `~/.keepquiet/config.json`，文件权限为 `0600`。这一步成功时，终端会打印 `installed ~/.keepquiet/bin/keepquiet (config: ~/.keepquiet/config.json)`。

#### 接入 Claude Code

运行安装脚本以注册插件并启用 hook：

```bash
scripts/install-claude.sh
```

该脚本会完成本机二进制安装、校验本地插件目录、添加 GitHub marketplace `Bl0omZ/keepquiet`，并执行 `claude plugin install keepquiet@keepquiet`。脚本结束时，`claude plugin list` 会列出已启用的 `keepquiet@keepquiet`。

重启 Claude Code 后插件生效。如果要停用插件，执行 `claude plugin disable keepquiet@keepquiet`。

#### 接入 Codex CLI

运行安装脚本以写入 Codex 的 hook 配置：

```bash
scripts/install-codex.sh
```

如果 `~/.codex/hooks.json` 还不存在，脚本会直接写入 3 个 hook 并打印“已写入”。如果 `~/.codex/hooks.json` 已经存在，脚本会生成 `~/.codex/hooks.keepquiet.json`，你需要把其中的 3 个 hook 手动合并到 `~/.codex/hooks.json`。

写入配置后，按以下步骤信任这些 hook：

1. 启动 `codex`。
2. 输入 `/hooks` 并审核信任这 3 个 hook。未受信任的 hook 会被 Codex 直接跳过，导致敏感数据按原文发给模型。
3. 如果你使用非交互命令 `codex exec`，可以加上 `--dangerously-bypass-hook-trust` 参数。每次修改 `hooks.json` 后，都需要重新执行 `/hooks` 确认信任。

#### 接入 Pi

运行安装脚本以复制 Pi 扩展：

```bash
scripts/install-pi.sh
```

脚本会把扩展复制到 `~/.pi/agent/extensions/keepquiet/` 并打印该路径。下次启动 Pi 时扩展会自动加载。如果 `keepquiet` 已经在 `PATH` 或 `~/.keepquiet/bin` 中，你也可以直接运行 `pi install ./plugins/pi/keepquiet`。

Pi 扩展按以下顺序查找二进制文件：先读环境变量 `KEEPQUIET_BIN`，再找 `$KEEPQUIET_HOME/bin/keepquiet`，最后查找 `PATH` 中的 `keepquiet`。

## 各客户端的拦截与脱敏行为

三个客户端提供的扩展接口不同，因此 `keepquiet` 在不同阶段的处理方式存在差异。

| 客户端 | 用户提示词 | 工具调用输入 | 工具调用输出 | Hook 异常时的策略 |
|---|---|---|---|---|
| Claude Code | 发现敏感项时直接拦截，不能改写提示词。只含配置域名时按原文放行。 | 在 `PreToolUse` 阶段还原真实值。命中 `restoreDenyTools` 的工具只还原域名。 | 在 `PostToolUse` 阶段把敏感值替换为占位符。 | 拒绝执行，配置为 `onFailure: block`。 |
| Codex CLI | 发现敏感项时直接拦截，不能改写提示词。只含配置域名时按原文放行。 | 在 `PreToolUse` 阶段还原真实值。命中 `restoreDenyTools` 的工具只还原域名。 | 不能直接改写输出，改为通过已脱敏的 `block.reason` 把结果交给模型。 | 二进制缺失或进程被系统杀死时默认放行。 |
| Pi | 发送请求前直接改写整个请求体。 | 在 `tool_call` 阶段还原真实值。 | 发送请求前对整个请求体执行 `MaskJSON` 脱敏。 | 调用 `ctx.abort()` 中止当前请求。 |

当 Claude Code 或 Codex CLI 拦截含有敏感数据的提示词时，终端返回的拦截原因中会附带一份已经脱敏的提示词文本，你可以直接复制该文本重新发送。

## 配置说明

默认配置文件位于 `~/.keepquiet/config.json`。你可以通过 `KEEPQUIET_HOME` 修改整个数据目录，也可以通过 `KEEPQUIET_CONFIG` 只修改配置文件路径。

如果未设置 `KEEPQUIET_HOME`，且 `~/.keepquiet` 不存在但旧版目录 `~/.agent-redact` 存在，`keepquiet` 会继续读取旧目录中的 `vault.json` 而不会复制文件。运行任意安装脚本创建 `~/.keepquiet` 后，将不再读取旧目录。

配置文件包含未知字段或 JSON 损坏时，hook 会拒绝执行以阻止泄露。Pi 扩展会在下一次请求或还原操作时自动读取最新配置，不需要重启 Pi。

### 默认配置

安装脚本首次运行时会写入以下默认配置，你也可以运行 `keepquiet config default` 查看：

```json
{
	"secrets": true,
	"personal": true,
	"domainReplacements": [
		{ "from": "antgroup-inc.cn", "to": "Xgroup-inc.cn" },
		{ "from": "alipay.com", "to": "Xpay.com" },
		{ "from": "antfin.com", "to": "Xfin.com" },
		{ "from": "alibaba-inc.com", "to": "Xbb-inc.com" }
	],
	"prompt": "block",
	"restoreDenyTools": ["WebFetch", "WebSearch", "web_search", "web_fetch"],
	"claude": { "preToolDecision": "none" }
}
```

### 完整配置示例

你可以在 `~/.keepquiet/config.json` 中按需添加细分类别、自定义词表和正则规则：

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
	"domainReplacements": [
		{ "from": "antgroup-inc.cn", "to": "Xgroup-inc.cn" },
		{ "from": "alipay.com", "to": "Xpay.com" },
		{ "from": "antfin.com", "to": "Xfin.com" },
		{ "from": "alibaba-inc.com", "to": "Xbb-inc.com" }
	],
	"zhNames": true,
	"replace": { "EMAIL": "[邮箱已隐藏]" },
	"prompt": "block",
	"restoreToolInput": true,
	"restoreDenyTools": ["WebFetch", "WebSearch", "web_search", "web_fetch"],
	"claude": { "preToolDecision": "none", "displayRestore": true }
}
```

### 基础检测与动作字段

以下字段控制内置敏感数据检测、拦截策略与客户端行为：

| 字段 | 说明 |
|---|---|
| `secrets` | 密钥检测总开关，默认开启。覆盖私钥、各类 API Key、JWT 令牌以及配置或 URL 中的密码与密钥。 |
| `personal` | 个人隐私检测总开关，默认开启。开启后会检测 `EMAIL`、`PHONE`、`ID_CARD` 和 `BANK_CARD`。这四项跟随 `personal` 开关，不能写进 `kinds`。 |
| `kinds` | 附加类别开关，仅允许填写 `ssn`、`passport`、`iban`、`birthday`、`mac`、`serial`、`home`、`userhost`、`ip`、`bucket`。前 6 项在 `personal` 开启时默认开启，后 4 项默认关闭。填写 `PHONE` 等其他名称会报错。`ip` 会自动跳过内网、回环地址和常见公共 DNS。 |
| `zhNames` | 规则式中文姓名识别开关，默认关闭。不识别英文人名。 |
| `actions` | 按类别指定命中后的动作。`mask` 为默认值，会将敏感值替换为占位符。`warn` 会按原文放行并输出提示。`block` 会完成脱敏并停止本轮对话。工具输出触发 `block` 时，工具已经产生的副作用不会回滚。 |
| `prompt` | 当提示词包含域名以外的敏感数据时的处理策略。`block` 为默认值，直接拦截提示词。`warn` 会输出提示并放行。该字段仅对 Claude Code 和 Codex CLI 生效。 |
| `replace` | 将指定类别的敏感数据替换成固定文本，而不是可还原的占位符。该替换是单向的，无法在工具执行前还原，且仅对 Claude Code 和 Codex CLI 生效，Pi 始终使用占位符。 |
| `claude.preToolDecision` | Claude Code 在 `PreToolUse` 阶段随 `updatedInput` 一起返回的权限决定，可选 `none`、`ask` 或 `allow`，默认为 `none`。在非交互 `-p` 模式下设为 `ask` 会导致工具调用被拒绝。 |
| `claude.displayRestore` | 是否在 Claude Code 的 `MessageDisplay` 阶段把占位符还原到终端屏幕，默认开启。 |

`keepquiet` 不包含 magpie 的 `Shape` 同形伪造功能或本地 NER 模型，且占位符格式不可修改。

### 自定义词表与正则规则字段

以下字段用于扩展你自己的敏感词、白名单和正则规则：

| 字段 | 说明 |
|---|---|
| `words` | 关键词黑名单，命中后替换为 `{{TERM_xxxxxxxx}}` 占位符。 |
| `wordsAnyCase` | 匹配 `words` 时是否忽略大小写，默认关闭。 |
| `allow` | 白名单数组。完整匹配白名单的字符串永远不会被脱敏。 |
| `rules` | 自定义规则数组，最多 32 条。每条规则设置 `kind` 名称，并二选一填写 `prefix` 或 `regex`，正则使用 RE2 语法。`prefix` 长度为 3 到 64 个字符，且被匹配的值在前缀后至少要有 8 个 `[A-Za-z0-9_-]` 字符。`regex` 长度不超过 300 个字符，包含捕获组时只替换第 1 个捕获组。 |

### 内部域名替换与工具还原边界

`domainReplacements`、`restoreToolInput` 和 `restoreDenyTools` 共同控制内部域名和敏感占位符在工具调用时的还原行为：

- **出站域名替换。** `domainReplacements` 在出站时按数组顺序替换匹配的主机名及其子域名，默认包含上表中的 4 个内部域名，设为空数组 `[]` 可关闭。匹配遵循完整域名标签边界，因此 `alipay.com.cn` 不会被当成 `alipay.com` 替换。对于邮箱地址，只替换 `@` 后面的域名部分并保留用户名，例如 `alice@alipay.com` 会变成 `alice@Xpay.com`，而不会变成 `EMAIL` 占位符。
- **本机工具完整还原。** `restoreToolInput` 默认开启。未命中 `restoreDenyTools` 的本机工具在执行前，输入参数中的 `vault` 占位符和被替换的域名都会完整还原为真实值。
- **外部联网工具边界。** `restoreDenyTools` 默认包含 `WebFetch`、`WebSearch`、`web_search` 和 `web_fetch`。为了保证外部请求可用，这些工具在执行前只还原 `domainReplacements` 中配置的真实域名，而不还原 `vault` 中的密钥和个人信息占位符。这意味着真实域名会随请求发送给外部工具服务，但密钥和个人信息不会。
- **提示词阶段的域名限制。** Claude Code 和 Codex CLI 的 `UserPromptSubmit` 接口不支持改写提示词。如果提示词只命中了 `domainReplacements` 中的域名，提示词会按原文放行。如果提示词同时包含域名和其他敏感项，则按其他敏感项的 `prompt` 策略拦截或告警。

## 命令行与环境变量

`keepquiet` 提供以下命令行子命令：

| 命令 | 说明 |
|---|---|
| `keepquiet mask` | 从标准输入读取文本，向标准输出打印脱敏后的文本，并将映射写入 `vault.json`。 |
| `keepquiet mask-json` | 从标准输入读取 JSON 请求体并执行 `MaskJSON` 脱敏。 |
| `keepquiet restore` | 从标准输入读取文本，将其中的占位符和假域名还原为真实值。 |
| `keepquiet scan` | 只检测标准输入是否包含敏感数据，不写入 `vault.json`。发现敏感数据时退出码为 `1`。 |
| `keepquiet hook claude <Event>` | 供 Claude Code 调用的内部 hook 入口。 |
| `keepquiet hook codex <Event>` | 供 Codex CLI 调用的内部 hook 入口。 |
| `keepquiet serve` | 供 Pi 扩展调用的常驻标准输入输出 JSON 服务。 |
| `keepquiet vault path` | 打印当前 `vault.json` 的文件路径。 |
| `keepquiet vault clear` | 清空 `vault.json`。如果仍有正在运行的 Pi 或 `keepquiet serve` 进程，该命令会拒绝执行。 |
| `keepquiet config default` | 打印默认配置的 JSON 内容。 |
| `keepquiet version` | 打印当前版本号。 |

以下环境变量可覆盖默认路径或还原策略：

| 环境变量 | 说明 |
|---|---|
| `KEEPQUIET_HOME` | 数据目录路径，默认为 `~/.keepquiet`。 |
| `KEEPQUIET_CONFIG` | 配置文件路径，默认为 `$KEEPQUIET_HOME/config.json`。 |
| `KEEPQUIET_BIN` | Pi 扩展使用的 `keepquiet` 二进制文件路径。 |
| `KEEPQUIET_RESTORE_DENY` | Pi 扩展侧额外禁止还原的工具名称，使用逗号分隔。命中该变量的工具完全跳过还原，包括域名也不还原，配置文件中的 `restoreDenyTools` 仍然生效。 |

## 已知局限与安全边界

使用前先了解以下限制，避免在未覆盖的路径中泄露密钥：

1. **Claude Code 与 Codex 不能改写提示词。** 这两个客户端的 `UserPromptSubmit` 只能拦截提示词，无法原地修改文本。被拦截的提示词不会发给模型，但 Claude Code 仍可能把原文写入本机的本地会话记录。同时，只包含内部域名的提示词无法在该阶段隐藏，不要在提示词中直接输入内部域名。
2. **Claude Code 与 Codex 的未覆盖路径。** 这两个客户端的 hook 不会处理系统提示词、`CLAUDE.md`、`AGENTS.md`、`@文件` 引用、图片以及客户端托管的远端工具，不要把密钥写进这些位置。Pi 直接改写发往模型的完整请求体，不受此限制。
3. **Codex 进程异常时默认放行。** 当 `keepquiet` 二进制文件被删除，或 hook 进程被操作系统强制杀死时，Codex 会按 fail-open 放行请求，导致敏感数据按原文发出。对于进程内部的错误，`keepquiet` 会捕获并返回拦截结果。
4. **工具执行失败的报错无法改写。** Claude Code 的 `PostToolUseFailure` 不支持改写输出，因此工具执行失败时的错误文本可能包含未脱敏的原始内容。
5. **本机 `vault.json` 为明文存储。** `~/.keepquiet/vault.json` 保存了占位符到真实值的明文映射，你需要自行保护 `~/.keepquiet/` 目录权限。如需清理映射，先退出所有 Pi 和 `keepquiet serve` 进程，再执行 `keepquiet vault clear`。清空后，历史对话中的旧占位符将无法还原。
6. **工具参数还原不解析目标语法。** 工具输入中的占位符按字面量直接替换，不会解析或转义 shell、SQL 或代码文件语法。如果敏感值本身包含引号或换行符，建议让工具通过独立参数、环境变量或文件读取该值。
7. **带签名或加密的推理块不替换域名。** 模型返回的带签名或加密的 thinking 与 reasoning 块需要逐字节保留原始签名，因此不参与域名替换。虽然加密块通常不可直接阅读，但不要依赖这些块来隐藏内部域名。
8. **保留域名与规则准确率。** 属于 `example.com` 等保留域名的邮箱不会被脱敏。基于规则的检测同时存在漏报和误报可能。

## 开发验证

运行 Go 单元测试：

```bash
cd core && go test ./...
```

构建二进制后，使用 Node.js 24 运行 Pi 扩展测试。该测试会调用真实的 Pi 扩展回调与本地二进制，不会请求外部模型服务：

```bash
scripts/build.sh
node --test tests/pi.test.mjs
```

上游 `TestMaskEval` 测试中的 `both` 列包含了预录制的本地模型识别结果。`keepquiet` 默认只启用规则检测，因此不能用 `both` 列的数据代表本项目的召回率。

## 开源许可与致谢

- **检测规则与占位符实现。** 源自 [yetone/magpie](https://github.com/yetone/magpie) 的 `internal/redact`，采用 MIT License，Copyright (c) 2026 yetone，许可证全文见 `third_party/magpie/LICENSE`。代码基于提交 `c158b04` 引入，并同步了邮箱误判修复提交 `dce7fe6` 与 `25341b4`。
- **本项目其余代码。** 采用 MIT License，详见 `LICENSE`。
