# Muse Code 调研与 pactify 接入建议

> 调研日期：2026-10-16。只做了研究：没有安装，也没有执行任何下载的代码。`install.sh`、launcher 脚本和 SDK 仓库文件只当文本下载下来 grep，没有执行。
> 标注说明：**[官方]** 指 dev.meta.ai 文档、meta-models GitHub 组织或 npm / Homebrew 元数据；**[第三方]** 指社区来源；**[未找到]** 指我没找到权威来源，下文不做猜测。

---

## 0. 结论速览

- **身份确认**：superpowers 映射里的 "Muse" 就是 **Meta 的 Muse Code**，二进制名为 `muse`。
- **推荐接入路径：原生 MSP（Muse Session Protocol）**，也就是 `muse serve` 提供的 stdio JSON-RPC 2.0。
  - 它的形状和 codex app-server 几乎一一对应：`initialize`/`initialized`、`session/start`、`turn/start`、`item/*`、`turn/completed`（带 usage）、`turn/cancel`，以及服务端发起的 `approval/request`。
  - 因此可以直接复用 `internal/cockpit/jsonrpc.go` 的 `rpcConn` 和 `processGroup`，以 `codex.go` 为模板写一个 `muse.go`。
- **不推荐走 ACP**：
  - Muse **没有公开文档的 `muse acp` 命令**，ACP 官方 registry 也没有 muse 条目。
  - 现有 ACP 适配器都是社区项目（BrokkAi/muse-acp 等），本身就是 ACP→MSP 的桥。走 ACP 等于多一跳第三方代码，还会丢掉 MSP 的类型化 quota / usage 信息。
- **`muse exec --json` 只适合当兜底**：它输出 JSONL，但事件 schema 没有公开文档，也不能列模型。
- **最大的风险**：
  1. 沙箱默认很紧：只能写 workspace 和 temp，`.git` 只读，网络 proxy-only 且每个新 host 都要审批。Go / npm 缓存和 worktree 的 git 元数据都可能被挡。
  2. 凭据存储位置不明，可能在 keychain，影响托管 home 的隔离做法。
  3. 发版极快：约每天一个 patch，launcher 每小时自动更新。
  4. 订阅默认开启 Contributor mode（Meta 可以用你的内容训练），这需要 Owner 决策。
  5. 子代理的用量不计入 session cumulative，有少记 token 的风险。

---

## 1. 身份与候选（"Muse Code" 是否有歧义）

| 候选 | 是否匹配 superpowers 映射 | 说明 |
|---|---|---|
| **Meta Muse Code**（`muse` CLI） | **是** | 匹配点有五处，见下表。 |
| Spimy/muse-cli、liuyibo/muse、MUSE "AI Agent Memory System"、pypi muse4ever 等 | 否 | 名字相同或相近，但都与 coding agent harness 无关。 |
| musecodes.io | 不确定 | 自称 "Meta's first AI coding agent" 的介绍站，是否官方不明，下文不引用。 |

Meta Muse Code 与 superpowers 映射的匹配点：

| 映射中的特征 | 对应证据 |
|---|---|
| 插件命令 `muse plugins install ./` 和 `muse plugins approve superpowers` | superpowers 6.4.1 的 README 和 RELEASE-NOTES #2317 |
| `.muse-plugin/plugin.json` 中声明 `SessionStart` hook，hook 脚本读取 `MUSE_PLUGIN_ROOT` | 本地插件目录（脚本注释写的是 "Muse Spark"） |
| 工具名 `read_file`、`shell` 等 | 官方 MSP 转录中确实出现了 `read_file` 和 `shell` |
| 有 `muse skills` 命令 | 官方 extending 文档 |
| 项目指令按 `AGENTS.md`、`CLAUDE.md`、`.agents/AGENTS.md`、`.claude/CLAUDE.md` 的顺序查找 | 官方 configuration 文档 |

本地参考：`~/.claude/plugins/cache/claude-plugins-official/superpowers/6.4.1/` 下的 `skills/using-superpowers/references/muse-tools.md`、`.muse-plugin/plugin.json`、`hooks/session-start`、`README.md`（Muse 一节）、`RELEASE-NOTES.md`（v6.4.1）。

---

## 2. 厂商、许可、安装、版本、节奏

**厂商**：Meta。2026-08-05 以 beta 形式发布，同时发布 Muse Spark 1.2。来源：[官方博客](https://research.meta.ai/blog/introducing-muse-code-and-muse-spark-1-2) 和 [The Register 2026-08-06](https://www.theregister.com/ai-and-ml/2026/08/06/meta-wants-to-get-inside-your-terminal-with-its-new-coding-agent/5283717)。目前已出 beta、进入 1.x。

**许可**：
- CLI 本体闭源，是原生二进制。从 Meta 的 CDN `lookaside.facebook.com` 分发，没有源码仓库。CLI 的使用条款 **[未找到]**。
- **SDK 与协议声明是 MIT 开源**：[meta-models/muse-code-sdk](https://github.com/meta-models/muse-code-sdk)。这是从内部仓库镜像出来的，Issues 开放。

**安装** [官方]：
- `curl -fsSL https://dev.meta.ai/install.sh | sh`，或者 Windows 下 `irm https://dev.meta.ai/install.ps1 | iex`。
- install.sh 会装一个 **launcher**（`~/.local/bin`，可用 `MUSE_INSTALL_DIR` 改）。真实二进制在 launcher 目录里，文件名形如 `muse-bin-<version>`。
- launcher 默认**每 3600 秒自动检查更新**。相关环境变量：`MUSE_NO_AUTO_UPDATE`、`MUSE_UPDATE_INTERVAL_SECONDS`、`MUSE_CHANNEL`（默认 `muse-stable`）、`MUSE_SYNC_UPDATE`。
- launcher 读取的凭据路径：默认 `$XDG_CONFIG_HOME/muse/auth.json`，`XDG_CONFIG_HOME` 未设时为 `~/.config/muse/auth.json`；可用 `MUSE_AUTH_PATH` 覆盖。
- launcher 在非 TTY 下不会发起登录（受 `MUSE_LOGIN` 控制）。
- Homebrew：`brew install --cask muse-code`。cask 版本 `1.4.9-R6297.1`，来源 [formulae.brew.sh](https://formulae.brew.sh/api/cask/muse-code.json)。
- 没有 npm 版 CLI。npm 上只有 SDK：`@muse-code/sdk` 和 `@muse-code/msp`。

**当前版本**：`1.4.9-R6297.1`（muse-stable 频道，`https://api.meta.ai/muse-code/channels/muse-stable`，2026-10-16 查询）。SDK 与 CLI 版本锁步，npm 上的 `@muse-code/sdk` 1.4.9 发布于 2026-10-15。

**发版节奏**：npm SDK 的发布时间依次为：
- 1.3.0：09-18
- 1.4.2：09-30
- 1.4.4：10-08
- 1.4.6：10-11
- 1.4.7：10-13
- 1.4.8：10-14
- 1.4.9：10-15

最近两周基本**每 1–2 天一个 patch**。官方 changelog 页面 [dev.meta.ai/docs/muse-code/changelog](https://dev.meta.ai/docs/muse-code/changelog) 不带日期，日期只能从 npm 推断。

**平台要求**：
- macOS 12+ 且只支持 Apple silicon（1.4.4 起）。
- Linux 沙箱需要 bubblewrap，且不能是 musl 构建。
- Windows 用自带沙箱，首次初始化可能弹 UAC。

---

## 3. 认证与计费

**登录方式** [官方 [auth](https://dev.meta.ai/docs/muse-code/auth)]：
- 首次运行时选浏览器登录（Meta Account / Meta Model API 账户）或 API key。
- 环境变量 `META_API_KEY` 优先级最高：先 `META_API_KEY`，再已存的 key，最后浏览器会话。
- 相关命令：`muse auth set`、`muse logout`、`muse login`。
- CI 场景：`META_API_KEY="$KEY" muse exec "..."`。
- MMA（Business & Enterprise）账户不能走浏览器登录，只能用 key。
- 凭据在磁盘上的存储位置，文档没有写 **[未找到]**。SDK 的 QA 隔离环境会设置 `TBH_CREDENTIAL_BACKEND=file`，这暗示默认后端**可能不是文件**（macOS 上可能是 keychain）。这一点必须在 Phase 0 实测。

**计费有两种**：
1. **按量（API key）**。价格见 [pricing-rate-limits](https://dev.meta.ai/docs/pricing-rate-limits)，单位为每百万 token：

   | 模型 | 输入 | 缓存命中输入 | 输出 |
   |---|---|---|---|
   | `muse-spark-1.3/1.2/1.1` | $1.25 | $0.15 | $4.25 |
   | `*-contributor` | $0.10 | $0.002 | $0.20 |
   | `muse-wonder-1.0` | $4 | $0.20 | $20 |

   速率限制按 team 计算（不是按 key）：standard 为 3000 RPM / 4M TPM，contributor 为 150 RPM / 3M TPM。超限返回 HTTP 429，可能带 `Retry-After`；成功响应带 `x-ratelimit-*` 头。

2. **订阅** [官方 [subscriptions](https://dev.meta.ai/docs/muse-code/subscriptions)]：
   - 只在 Muse Code CLI 里可用，覆盖 CLI 引导时自动绑定的那一个 key。
   - 四档：Everyday（"10-50 prompts every 5 hours"）、High（5x）、Power（20x，含 Muse Wonder Full）、Maximum（80x）。
   - 页面没写价格。
   - **默认开启 Contributor mode**：Meta 可用你的内容训练模型，换取更多额度。可以关闭。
   - 额度用完只能升级或等刷新。

**额度如何上报**（MSP 层是类型化的）：
- `usage/read` 和 `usage/changed` 返回 `SubscriptionUsage`，内容为：
  - `tier`
  - `window{usedPercent, resetsAtMs, windowDurationMins}`：5 小时级窗口
  - `weekly{usedPercent, resetsAtMs}`
- 额度耗尽时，turn 失败的 `turn/completed.error.quota = {limitKind:"tierLimit"|…, resetsAtMs}` 字段一定存在。规范明确要求按这个字段是否存在来分支，**不要解析 message**。

**另一条路**：Muse 模型也提供 OpenAI Responses 兼容端点 `https://api.meta.ai/v1`，以及 Anthropic Messages 端点 `https://api.meta.ai`。官方 [coding-agents](https://dev.meta.ai/docs/coding-agents) 页给了 codex（`CODEX_HOME` 加 `-c model_providers.meta...`）、opencode 和 Claude Code 的配置。这条路**只能按量计费，不能用订阅**。

---

## 4. 无头 / 非交互模式（`muse exec`）

来源：[extending#headless](https://dev.meta.ai/docs/muse-code/extending) 和 [configuration](https://dev.meta.ai/docs/muse-code/configuration)，均为官方文档。

**常用调用**：
- 基本：`muse exec "<prompt>"`，或 `muse exec --prompt-file task.txt`。
- `--json`：在 stdout 输出 JSONL 事件。**事件 schema 没有公开文档** **[未找到]**。changelog 只提到 workflow 报错时 "JSON output stays clean"。
- `--output-schema <FILE>`：让最终答案成为符合 schema 的 JSON。只有 Meta provider 支持。
- `--session-id <uuid>`：创建或续接指定会话。工作区不一致时需要加 `--allow-workspace-switch`。changelog 还提到 `muse exec --resume`。`muse resume` 只是交互式 UI。
- `muse export --session <uuid> --out run.json`：导出会话。可以当作独立验证源。

**其他 flag**：
- `--max-model-steps <n>`：限制运行步数。
- `--disable-reminders`
- `--disable-write` / `--disable-shell`：不向模型提供被禁用的工具。
- `--disable-request-user-input`：1.4.9 起，exec 和 serve 都可用。
- `--no-session-log`
- `-c key=value`：可重复。
- `--max-tool-output-bytes`

**exec 和交互模式共用的 flag**：
- `--model`
- `--reasoning-effort`（别名 `--effort`）
- `--sandbox-network`
- `--disable-sandbox` / `--disable-approval` / `--yolo`
- `--trust-workspace`
- `--approval-mode`
- `--approval-judge <on|off>`
- `--permission-profile`
- `--subagent-worktree-isolation`（只为兼容保留，不强制隔离）
- `--workspace <path>`

**exec 退出码**：

| 码 | 含义 |
|---|---|
| 0 | turn 完成（不代表工作正确） |
| 1 | 失败或取消，包括 `--max-model-steps` 触顶 |
| 2 | 用法错误 |
| 130 / 143 | SIGINT / SIGTERM |

---

## 5. 机器协议

### 5.1 MSP：`muse serve`，原生且稳定（推荐）

来源 [官方 SDK 仓库]：
- `schema/msp/msp.d.ts`
- `schema/msp/stable/msp.schema.json`（stable fingerprint 为 `sha256:5d3b43e5…`，`schemaVersion: 1`）
- `schema/msp/transcripts/*`：几十个手写的 golden 转录。
- 文档站：[meta-models.github.io/muse-code-sdk](https://meta-models.github.io/muse-code-sdk)

**传输**：stdio 上的 NDJSON，JSON-RPC 2.0，一行一帧。帧上限默认 10 MiB。

**握手**：

```
-> initialize {clientInfo:{name,version}, capabilities?:{experimentalApi, optOutNotificationMethods, requestedCapabilities, userInputDialogs}}
<- {serverInfo, userAgent, museHome, platformFamily, platformOs,
    schema:{version:1, fingerprint}, grantedCapabilities, experimentalApi,
    sessionDurability, sandboxAvailability?, sandboxUnavailableReason?}
-> initialized (notification)
```

握手结果里有两点要注意：
- `museHome` 告诉你实际的数据目录。转录中是 `~/.muse`，会话文件在 `<museHome>/sessions/YYYY/MM/DD/<sid>/session.jsonl`。
- `sandboxAvailability` 在 1.4.7 加入，告诉你这台机器能否强制执行沙箱。

**请求（client 到 server）**：

| 类别 | 方法 |
|---|---|
| 会话 | `session/start`、`session/resume`、`session/fork`、`session/list`、`session/read`、`session/compact`、`session/setModel`、`session/setReasoningEffort`、`session/setApprovalMode`、`session/rename` |
| 轮次 | `turn/start`、`turn/steer`、`turn/interrupt`、`turn/cancel`、`turn/unqueue` |
| 查询 | `model/list`、`usage/read` |
| 审批与用户输入 | `approval/decide`、`approval/listPending`、`userInput/answer`、`userInput/cancel` |
| 子代理 | `subagent/*` |
| 后台任务 | `task/*` |
| 视图 | `view/subscribe`、`view/page`、`view/gap` |
| 其他 | `skill/*`、`plugin/*`、`hook/list`、`goal/*`、`workflow/*` 等 |

**通知（server 到 client）**：
- `session/started`、`turn/started`
- `item/started`、`item/delta`、`item/updated`、`item/completed`
- `turn/completed`、`turn/retryScheduled`、`turn/modelRetryScheduled`、`turn/foregroundCompleted`
- `session/tokenUsage`、`session/contextUsage`、`session/todoListChanged`、`session/statusChanged`
- `usage/changed`
- `approval/requested`、`approval/resolved` 等

**服务端发起的请求**：`approval/request` 和 `userInput/request`。

**`session/start` 参数**：
- `commandId`：必填，UUIDv7，作幂等句柄。
- `sessionId?`：可由客户端指定。
- `workspaceRoot` / `workspaceRoots[]`：后者第一项是 CWD，其余根也套用 workspace 规则，但不授予 Project Trust。
- `modelId?`、`providerId?`
- `approvalMode?`：取值 `allowAll | promptUnmatched | onRequest | denyUnmatched`。文档说这是**唯一声明非交互运行策略的入口**。
- `permissionProfile?`：只能从 host 目录里已有的 profile 中选，不能新建。
- `config?.mcpServers`

**`turn/start` 参数**：`sessionId`、`commandId`、`input[{type:"text",text}]`、`reasoningEffort?`、`ifBusy?`（默认 queue）、`workspaceRoots?`。

**`turn/completed` 字段**：
- `terminal`：`completed | failed | cancelled`，开放枚举。
- `error?`：`{kind, message, retryable, quota?}`。
- `usage?`：`{inputTokens, outputTokens, cachedTokens, reasoningTokens, cacheRead/WriteTokens?}`。
- `durationMs`、`timeToFirstTokenMs`。

**`TurnErrorKind`**（开放枚举）：`stepLimit | configError | projectionError | logError | workflowLaunchError | environmentError | modelError | launchError | authRequired | mcpLinkError`。按规范，失败**只出现在 `turn/completed` 里**，不会以 JSON-RPC error 的形式返回。

**`session/tokenUsage`**：每次模型调用发一条，带 `promptTokens` 和 `totalTokens`（服务端按缓存口径去重计算），以及 `cumulative{promptTokens, outputTokens, totalTokens, cost?}`，其中 `cost` 由服务端计算。**子代理和 workflow 子任务的用量不计入 cumulative**，而是挂在各自的 item 上。

**item 种类**：`userMessage | agentMessage | reasoning | toolCall | userShell | subagent | workflow | reminderChild | compaction | hookRun | sideChat | webSearch | monitorFeed`。规范要求客户端以通用方式渲染未知种类。
- `agentMessage` 带 `messagePhase`（commentary 或 `finalAnswer`），1.4.8 加入。
- `toolCall` 带 `tool`、`callId`、`args`，用 `output` 字段做增量输出，完成后带 `visibleOutput`。

**`model/list`**：是查询，**不需要 session，也不计费**。返回字段：`modelId`、`providerId`、`isDefault`、`isActive`、`contextLimit`、`outputLimit`、`cost`、`variants[]`（可选的 effort 档）、`defaultReasoningEffort`。

**`approval/request` 参数**：`approvalId`、`availableChoices[{choiceId, decision, scope, label}]`、`currentRequirementId`、`toolName`、`rawArgs`、`subject`、`protectedWrite` 等。用 `approval/decide{approvalId, choiceId, requirementId, commandId}` 作答。choice 由服务端生成，只能从给出的选项中选。

**serve 进程退出码**（SDK 的 `classifyExit`）：

| 码 | 含义 |
|---|---|
| 0 | stdin EOF 后正常排空 |
| 1 | 通用失败（损坏的配置目前也走 1） |
| 3 | configError：设置或凭据无法加载（1.0.x 起） |
| 5 | SDK 门被关闭：设置了 `MUSE_EXPERIMENTAL_SDK_ENABLED=off`。GA 后默认开启 |

**未登录时**，serve **不会退出**，而是让每个 turn 以认证失败结束。

**SDK**：
- TypeScript 包 `@muse-code/sdk`（MIT，零依赖，Node 20+）。
- Python 包 `muse_code`。
- 状态为 Developer Preview，尚无稳定性承诺。

**SDK 的进程管理做法，值得 pactify 对照**：
- POSIX 下 serve 进程自成进程组（`detached`）。
- 关闭顺序：先关 stdin、等它排空，再 SIGTERM，宽限 2 秒后对整个进程组 SIGKILL。

### 5.2 ACP

- **没有公开文档的 `muse acp` 子命令**。
- [ACP registry](https://github.com/agentclientprotocol/registry) 里没有 muse 条目。
- 官方 SDK 的 CHANGELOG（1.4.0 段）有一行 "Zed (ACP) adapter: session/set_mode now works"，另有一行 "MSP/ACP clients get the permission prompt"。这说明 Meta 内部可能有一个 ACP 适配层，但没有找到任何公开的入口或文档 **[未找到]**。
- 社区适配器（都是 ACP 到 MSP 的桥）：
  - [BrokkAi/muse-acp](https://github.com/BrokkAi/muse-acp)：Rust，Apache-2.0，npm 包 `@brokkai/muse-acp`，约 20 star。
  - bex-co/muse-code-acp
  - sanjay3290/muse-acp
  - 1broseidon/muse-acp

  BrokkAi 的 README 提到一个限制：在 `muse serve` 下，`:auto-review` 会退化为事事都问用户。

### 5.3 HTTP

没有 HTTP 或 WebSocket 服务模式的公开文档。`streamable_http` 只是 MCP 客户端支持的一种传输。

---

## 6. 会话、权限、模型、配置

**会话**：
- 会话 id 是 UUIDv7，可以由客户端指定。
- `session/resume{sessionId, cursor?, excludeItems?, permissionProfile?}` 支持从 view cursor 续接，cursor 格式为 `v4:<sid>:<n>`（1.4.9 起 v3 被拒）。
- 会话是 durable 的：本地事件日志支持崩溃后恢复。
- 1.4.3 起删除了 `session/delete`。保留期由 `session_retention_days` 控制（仅 macOS 和 Linux），也可以 `-c session_retention_days=N`。

**权限** [官方 [permissions](https://dev.meta.ai/docs/muse-code/permissions)]：

Profile 有四种：
- Ask me
- **Auto-review**：新会话的默认值。由 approval judge 自动审批，judge 不可用时回落为问用户。
- Unrestricted：等同 `--yolo`。
- **Read-only**：可读，不能写文件，不能运行有副作用的命令。

配置方式：
- settings 中的 `permissions.default_profile`，或 `--permission-profile`。
- profile 的确切 id 字符串，文档没有给出 **[未找到]**。schema 注释提到 `permissionProfile/list`，但它不在稳定方法表里。
- 一个 profile 在当前会话无法强制执行时，会被**拒绝**，不会被静默降级（1.4.6 起）。可设 `MUSE_PERMISSION_PROFILE_FAIL_OPEN=1` 恢复旧行为。
- 曾有一个安全修复（1.0.x）：`muse serve` 以前忽略 `permissions.default_profile`，导致只读 profile 失效。

`--approval-mode` 有三种：
- `on-request`（默认）：分阶段的 shell 审查。
- `untrusted`
- `never`

沙箱：
- macOS 用 Seatbelt，Linux 用打包的 bubblewrap，Windows 用自带沙箱。
- **只能写 workspace 和 temp；workspace 内的 `.git`、`.muse`、`.agents` 也是只读。**
- 确认不了沙箱已生效时，拒绝执行一切 shell。
- 网络 `--sandbox-network` 有三档：`proxy-only`（默认，每个新 host / 端口 / 协议都要审批）、`restricted`、`unrestricted`。

其他安全要点：
- `--disable-sandbox` 会让文件工具能写任何位置，并且网络全开。
- **hooks 和 MCP 工具都在沙箱外运行。**
- workspace trust 的影响：只有受信任的目录才加载项目的 `AGENTS.md`、skills、rules 和 `.muse/hooks.json`。已提交的 `.agents/memory` 例外，不受信任也会加载。

**模型与推理强度**：
- 模型：`muse-spark-1.3`、`muse-spark-1.2`、`muse-wonder-1.0`，以及 `*-contributor` 变体。overview 页面写默认是 1.2，coding-agents 页写的是 1.3，**两页之间存在文档漂移**。
- 用 `--model` 或 `session/setModel` 选模型。
- 推理强度分 `none | minimal | low | medium | high(默认) | xhigh | max | ultra` 八档。Meta provider 不接受 `none`；provider 不支持 `ultra` 时按 `xhigh` 运行。
- 设置方式：`--reasoning-effort` 或 `turn/start.reasoningEffort`。
- 不计费地列出模型：MSP 的 `model/list`；或 `muse model-profile show <model> --effort <level>`。
- `muse serve` 支持 `--provider` 和 `--model`：host 是多 provider 的，转录里出现过 openai provider。

**配置与目录**：
- 用户设置：`$XDG_CONFIG_HOME/muse/settings.json`，默认 `~/.config/muse/settings.json`。**必须带 `"schema_version": 1`**，否则所有命令都报错。
- 数据目录 `museHome`：转录中是 `~/.muse`。没有公开的 `MUSE_HOME` 环境变量 **[未找到]**。
- SDK 的隔离做法：自己的 QA 和 cookbook 用 `HOME=<tmp>`、`XDG_CONFIG_HOME=<tmp>/config`、`XDG_DATA_HOME=<tmp>/data`、`TBH_CREDENTIAL_BACKEND=file`、`TBH_DISABLE_TELEMETRY=1`、`TBH_DISABLE_FEATURE_CONFIG=1` 实现完全隔离。其中 `TBH_*` 变量没有文档，属于内部变量。

**有文档的环境变量**：
- `META_API_KEY`
- `MUSE_TRANSPORT_TRACE=1`
- `MUSE_PERMISSION_PROFILE_FAIL_OPEN=1`
- `MUSE_SESSION_ID` 和 `MUSE_THREAD_ID`：注入给 shell、hook 和 stdio MCP 进程。
- `MUSE_PLUGIN_ROOT`：给插件 hook 用。
- `MUSE_SKIP_PROMPT_HISTORY`
- `MUSE_EXPERIMENTAL_SDK_ENABLED`
- launcher 的那些 `MUSE_*` 变量（见第 2 节）。

**指令文件**：每层目录按 `AGENTS.md`、`CLAUDE.md`、`.agents/AGENTS.md`、`.claude/CLAUDE.md` 的顺序查找，**取第一个存在的**。查找范围从 workspace 根向上，直到 `.git` 边界。

**Skills 的查找位置**：
- 用户级：`$XDG_CONFIG_HOME/muse/skills`、`~/.agents/skills`、**`~/.claude/skills`**、**`$CODEX_HOME/skills`**（或 `~/.codex/skills`）。
- 项目级：`.agents/skills`、`.codex/skills`、`.claude/skills`。

这意味着委派运行默认会读到用户的 Claude 和 Codex skills，需要靠托管 HOME 隔离掉。

**Hooks**：
- 事件：`SessionStart`、`PreToolUse`、`PermissionRequest`、`PostToolUse`、`Stop`、`SessionEnd` 等 17 种。
- 配置位置：项目级 `.muse/hooks.json`（需要 trust）、用户级 settings、`managed_hooks_path`。

**MCP**：在 settings 的 `mcp_servers` 中配置 stdio 或 streamable_http。默认 `mode:"required"`，所以连不上就失败；设成 `optional` 时只告警跳过。

**子代理**：
- 默认每棵树最多 8 个（含根），可通过 `agents.execution_capacity` 设为 1–64；`ultra` 根默认 64。
- 有后台 subagent 和 reminder agent。`turn/foregroundCompleted` 表示前台工作已结束，但 reminder 还撑着这个 turn。

**取消与子进程**：
- `turn/cancel` 走普通命令通道，`turn/interrupt` 走优先通道（相当于"用户按了停止"）。两者都以 `turn/completed{terminal:"cancelled"}` 收尾，正在执行的 toolCall 以 `status:"cancelled"` 结束。
- changelog 中与子进程相关的几条：
  - 停止后台 shell 时，会连带结束移入了独立进程组的子进程，比如 `timeout` 启动的进程。
  - 终端关闭时会结束 hook 及其子进程。
  - 关闭 stdin 后正常排空。

---

## 7. 对照 pactify 的 harness 模型

接口参考：`internal/sub/harness.go`（Harness 注册表）、`internal/cockpit/backend.go`（Backend / Session / Event）、`internal/cockpit/codex.go`（app-server 实现）、`internal/cockpit/jsonrpc.go`（rpcConn 和 processGroup）、`internal/cockpit/opencode.go` 加 `internal/cockpit/acp.go`（ACP 实现）、`internal/cockpit/failure.go`、`internal/sub/managed_home.go`、`agy_home.go`、`cache.go`、`quota.go`、`worker.go`。

| pactify 需要的 | MSP 怎么提供 | 能否支持 |
|---|---|---|
| `Backend.Start` / `Resume` / `ResumeWith` | `session/start`、`session/resume{excludeItems:true}` | ✅ |
| `ThreadID` | `sessionId`（UUIDv7，可由客户端指定） | ✅ |
| 消息事件 | `agentMessage` 的 `item/delta`（field=text）、`item/completed`（`messagePhase=finalAnswer` 映射为 Final） | ✅ |
| 工具事件 | `toolCall` 的 `item/started`、`item/delta`（output）、`item/completed` | ✅ |
| plan | `session/todoListChanged` | ✅（需要确认 payload） |
| usage / cost | `turn/completed.usage`，以及 `session/tokenUsage.cumulative.cost`（服务端计算） | ✅ 但**子代理用量要另外累加** |
| 模型兑现校验 | `session/started.session.modelId`、`tokenUsage.modelId` | ✅ |
| `Models` / `ModelDetails`（不计费） | `model/list`（无 session），`variants` 和 `defaultReasoningEffort` | ✅ |
| Effort | `turn/start.reasoningEffort`，按模型的 `variants` 校验 | ✅ |
| 审批 | 服务端请求 `approval/request` 回 `approval/decide`，按 choice.decision 挑选 choiceId | ✅ |
| 只读 | `permissionProfile`（Read-only 的 id 待确认），加上一律 Deny | ⚠️ 机制存在。kimi、agy、opencode 只读都是 0/3 收敛；Muse 的 Read-only profile 不用审批就能读，**有可能收敛，需要实测** |
| 写入 | `approvalMode:"allowAll"`（沙箱仍开），或 `promptUnmatched` 加 pactify 自动批准 | ✅ 但受沙箱写范围限制（见风险） |
| 中断 | `turn/cancel` 或 `turn/interrupt` | ✅ |
| 关闭与子进程 | 关 stdin 后排空，再 SIGTERM，最后对进程组 SIGKILL（复用 processGroup） | ✅ |
| 失败分类 | `error.kind` 类型化，`error.quota{limitKind, resetsAtMs}`；serve 退出码 3 / 5 | ✅ 比 ACP harness 好，不用匹配文本 |
| 额度记忆（`quota.go`） | `error.quota.resetsAtMs` 直接作为 ResetAt；`usage/changed` 有窗口百分比 | ✅ |
| 托管 home | 用 `HOME`、`XDG_CONFIG_HOME`、`XDG_DATA_HOME` 隔离（SDK 自己就是这么做的） | ⚠️ 凭据的位置和后端未知 |
| 隐藏委派会话（Conceal） | 没有 archive 或 delete API，只能靠托管 home 隔离 | ⚠️ 和 opencode、agy 一样：`--with-user-config` 时无法隐藏 |
| 独立验证（Verify） | `muse export --session <id> --out f.json`；`session.jsonl` 的内部格式不承诺稳定，不要直接解析 | ⚠️ export 的 JSON 结构**没有文档**，需要实测 |
| Version | `muse --version`（经过 launcher，要设 `MUSE_NO_AUTO_UPDATE=1`） | ✅ |
| 用户提问 | `capabilities.userInputDialogs:false` 加 `--disable-request-user-input` | ✅ |

---

## 8. 路径比较与推荐

| 路径 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| **A. 原生 MSP（`muse serve`）** | 官方稳定协议，有 schema 和 fingerprint、golden 转录（MIT，可以直接做测试夹具）；类型化的 usage、quota、错误；模型列表不计费；形状和 codex app-server 几乎一致，可复用 jsonrpc 和 processGroup | 协议面很大；SDK 仍是 preview；发版很快 | **推荐** |
| B. ACP（社区适配器） | 复用 pactify 已有的 `internal/acp` | 没有官方 `muse acp`；多一跳第三方二进制（还要再装一个 Rust 或 npm 包）；丢失 quota 和 cost 类型；auto-review 在 serve 下会退化 | 不推荐 |
| C. `muse exec --json` | 实现最简单；退出码有文档 | JSONL schema 没有文档；不能列模型；审批无法交互，只能靠 `--disable-approval` 或 `--yolo` | 只做兜底或冒烟 |
| D. 经 codex harness 跑 Muse 模型（`api.meta.ai/v1` 加 `CODEX_HOME`） | 零新代码，马上就能测"模型本身"的能力 | 只能按量计费，不能用订阅；测的是 codex 外壳而不是 Muse 外壳 | 可选：作为评估前的低成本对照 |

---

## 9. 实施要点（路径 A）

1. **`internal/cockpit/muse.go`**，参照 `codex.go`：
   - 启动命令：`muse serve [--model M] [--sandbox-network …] [-c agents.execution_capacity=N] [--disable-request-user-input]`。
   - 进程环境：在 `filteredEnviron()` 的基础上加 `MUSE_NO_AUTO_UPDATE=1`、托管 home 的变量，以及缓存变量。
   - 握手时做三项校验：
     - `schema.version==1`，并记录 fingerprint 用于版本漂移告警。
     - 记录 `museHome`，填入 `SessionInfo.Home`。
     - 写任务遇到 `sandboxAvailability=="unavailable"` 时直接失败，并给出可执行的提示。
   - `session/start`：`commandId` 用新写的 UUIDv7 辅助函数生成（Go 标准库没有），`workspaceRoot=RepoDir`，`workspaceRoots=[RepoDir, WritableRoots…]`。**`WritableRoots` 对应 `workspaceRoots`，这一点需要实测确认能否写入**。同时带上 `modelId`，以及 `permissionProfile` 或 `approvalMode`。如果回报的 `modelId` 与请求不一致，返回 `ErrModelUnavailable`。
   - 事件映射见第 7 节表格。`usage/changed` 和 `turn/retryScheduled` 发成 `EventSystem`。对未知 item 种类和未知通知要容忍。
   - 审批：`approval/request` 转成 `ApprovalRequest`，`Respond` 从 `availableChoices` 中挑 decision 匹配且 scope 最窄的那个 choice。
   - `Interrupt` 映射到 `turn/cancel`，带上 activeTurnId。
   - `Close`：关 stdin，有限时间内等排空，再对进程组 SIGKILL。
2. **`ClassifyFailure("muse")`**：以结构化字段为准。
   - `quota` 字段存在：判为 quota，ResetAt 取 `resetsAtMs`。
   - `authRequired`：判为 auth。
   - `modelError` 且 `retryable`：判为 transient 或 other。
   - `stepLimit`：判为 limit。
   - `configError`、`launchError`、`environmentError`、`mcpLinkError`：判为 config。
   - serve 退出码 3 判为 config，5 判为 unsupported。
3. **`internal/sub` 注册 `"muse"`**：
   - `Experimental`：首轮评估前写成实验性。
   - `MinimalConfig: true`。
   - `Home`：以 `managedHarnessHome("muse")` 为根，设置 `HOME`、`XDG_CONFIG_HOME`、`XDG_DATA_HOME`；写入最小的 `settings.json`（`schema_version:1` 加并发上限）；凭据的处理方式等 Phase 0 结论，可以是 link 一个 auth 文件，或者透传 `META_API_KEY`。
   - `Models` / `ModelDetails` 用 `model/list`。
   - `NoReadOnly` 先设上，等评估证明只读能收敛再放开。
   - `Conceal`：用户配置模式下返回明确错误，参照 opencode。
   - `Verify`：基于 `muse export`。
   - `TransientRetry: true`。
4. **`sub doctor muse`**：检查版本、sandboxAvailability、认证（发一个最小 turn）、模型是否兑现、只读探测（结果为 SKIP 或真测）、托管 home 隔离。
5. **插件**：在 `plugins/pactify` 加 `/pactify:muse` skill 和 agent。**必须升 `plugin.json` 的 version**，CI 会检查。
6. **测试**：把 SDK 仓库 `schema/msp/transcripts/` 下的转录（MIT）复制到 `testdata`，做 fake-server 回放测试，覆盖这些场景：
   - session-start
   - text-run-single-turn
   - cancel-mid-turn
   - provider-failure-turn
   - turn-retry-scheduled
   - approval-round-trip / approval-deny-round-trip
   - resume-after-cursor
   - model-round-trip

   测试必须隔离 `HOME` 和 `PACTIFY_HOME`。

---

## 10. 风险

1. **沙箱写范围**：只能写 workspace 和 temp。
   - `GOCACHE`、`GOMODCACHE`、npm 缓存默认都在外面。pactify 的 `cache.go` 会把这些缓存目录追加进 `WritableRoots`，但 Muse 是否接受把它们作为额外的 `workspaceRoots` 写入，需要实测；也可以把缓存环境变量指到 temp。
   - **worktree 的 git 元数据在主仓库的 `.git/worktrees/` 下，`.git` 在沙箱里只读**，所以 agent 执行 `git add/commit/stash` 会失败。pactify 自己做 diff 收集，影响应该不大，但要确认不会导致任务失败。
2. **网络**：默认 proxy-only，每个新 host 都要审批。写任务由 pactify 自动批准，问题不大；只读任务一律拒绝，例如 `go mod download` 会被拒。
3. **凭据与隔离**：凭据存储位置未知（keychain 还是文件）。如果在 keychain，单改 HOME 可能读不到登录态，也可能仍然读得到，都需要实测。`META_API_KEY` 这条路是确定可行的，但只能按量计费。
4. **升级太快**：约每天一个 patch，launcher 默认每小时自动更新，job 运行中可能被换掉二进制。做法：
   - 运行时设 `MUSE_NO_AUTO_UPDATE=1`；
   - 握手时记录 `schema.fingerprint` 和 `serverInfo.version`，变化时告警，参照已有的 AJ-670 版本提示机制。
5. **Contributor mode 和数据训练**：订阅默认开启，Meta 可能用你的代码训练模型。contributor 价位便宜 10 倍以上，但同样会被用于训练。**需要 Owner 决定账户类型和是否关闭。**
6. **用量少记**：子代理和 workflow 子任务的 token 不计入 cumulative。Muse 默认会主动起后台 subagent 和 reminder agent，最多 8 个。需要把 subagent item 上的用量一起累加，或者用 export 重新加总。这和 opencode 的 AJ-680 是同一类问题。
7. **turn 的延长**：reminder 和 verify agent 可能让 turn 迟迟不结束（`turn/foregroundCompleted`）。需要在 serve 侧设好空闲超时，并确认 serve 能否关闭 reminder；`--disable-reminders` 目前只在 exec 文档里出现过。
8. **workspace trust**：不信任时，项目的 `AGENTS.md` 和 skills 都不加载；信任后，仓库里的 `.muse/hooks.json` 会**在沙箱外**执行。这是不可信仓库的注入面。需要决定委派时是否 trust，以及 trust 后是否禁用项目 hooks。
9. **平台限制**：Intel Mac 不支持；Linux 需要 bubblewrap。
10. **文档漂移**：默认模型 1.2 还是 1.3，两页说法不一；第三方指南里 0.2.1 时代的信息（例如 `MUSE_EXPERIMENTAL_PLUGINS=1`）已经过时。

---

## 11. 评估方案（沿用 `docs/opencode-eval.md` 的格式）

**前置 Owner 决策**：
- 账户：订阅（哪一档）还是 API key。
- Contributor mode：开还是关。
- 预算上限。
- 模型：默认 `muse-spark-1.3`；`muse-wonder-1.0` 只在 Power 档或按量计费时可用，可以作为第二轮。

**M0 冒烟（不计入结论）**：
- `sub doctor muse`：版本、沙箱、认证、model/list、模型兑现、托管 home 下没有会话残留。
- 手动跑一个读写各一次的小任务，确认三件事：
  - GOCACHE 在沙箱里可写；
  - worktree 中的 git 行为；
  - `muse export` 的结构。

**主评估**：
- 基线 `88df743`，用同样的 10 个大任务加 3 个只读任务（Phase 0 目录下的 `briefs/`）。
- host：Claude Code headless（haiku），经 `/pactify:muse` 委派。
- **串行执行**：吸取 agy Argon 周额度的教训；订阅档是按 5 小时窗口计的。
- 每题前后各调一次 `usage/read`，记录窗口百分比的变化。

**结果表的列**（与 codex、kimi、agy、opencode 并列）：
- 一次完成（门槛 ≥7/10）
- 小问题
- 失败或需返工
- 只读收敛（x/3）
- 模型兑现（用 export 逐条核对 model）
- 会话留痕（托管 home 隔离）
- 总成本和每题成本：服务端 cost 加子代理用量，并用 export 重新加总核对
- 耗时中位数
- 后端错误、重试次数（`turn/retryScheduled`）、空闲超时、额度事件

**判定口径**与 Phase 0 相同：先串行跑验收命令，再由两个独立评审子代理对照任务说明读 diff，并做回退测试。

**文档产出**：`docs/muse-eval.md`；评估中发现的 pactify 缺陷登记到 Linear 或 Notion Inbox。

**可选对照**：先用路径 D（codex 外壳加 `muse-spark-1.3` 按量）跑同样的 10 题，区分"模型能力"和"Muse 外壳能力"。

---

## 12. 来源

**官方**：
- 文档：
  - https://dev.meta.ai/docs/muse-code
  - https://dev.meta.ai/docs/muse-code/auth
  - https://dev.meta.ai/docs/muse-code/subscriptions
  - https://dev.meta.ai/docs/muse-code/permissions
  - https://dev.meta.ai/docs/muse-code/configuration
  - https://dev.meta.ai/docs/muse-code/extending
  - https://dev.meta.ai/docs/muse-code/changelog
  - https://dev.meta.ai/docs/coding-agents
  - https://dev.meta.ai/docs/pricing-rate-limits
- 发布公告：https://research.meta.ai/blog/introducing-muse-code-and-muse-spark-1-2（2026-08-05）
- SDK 仓库：https://github.com/meta-models/muse-code-sdk（MIT；`schema/msp/msp.d.ts`、`schema/msp/transcripts/*`、`CHANGELOG.md`、`clients/sdk-ts/src/connection/spawn.ts`、`clients/sdk-ts/qa/recorder.ts`、`clients/sdk-cookbook/src/recipes/classify-serve-exits.ts`）
- 包与频道：npm `@muse-code/sdk`（1.4.9，2026-10-15）；Homebrew cask `muse-code` 1.4.9-R6297.1；频道清单 https://api.meta.ai/muse-code/channels/muse-stable
- 安装脚本：https://dev.meta.ai/install.sh 和 https://api.meta.ai/muse-launcher.sh（只当文本阅读）

**第三方**：
- https://github.com/BrokkAi/muse-acp
- https://www.theregister.com/ai-and-ml/2026/08/06/meta-wants-to-get-inside-your-terminal-with-its-new-coding-agent/5283717
- https://agenticcontrolplane.com/integrations/muse-code（内容已过时：0.2.1 时代）
- https://github.com/agentclientprotocol/registry（列表中没有 muse）

**本地**：superpowers 6.4.1 插件目录（见第 1 节）。
