# Harness 集成评审：kimi / opencode / agy 升为一等公民的差距

> 2026-10-16 · 只读评审 · 基线 main `6af28ba`
> 本机版本：codex-cli 0.162.1 · kimi 2.2.0 · opencode 1.19.0 · **agy 1.4.0**（今天 17:52 自动升级；`~/.pactify/verified-versions.json` 里 agy 只验证到 1.3.6）
> 方法：读 `internal/sub`、`internal/cockpit`、`internal/acp`、相关 spec、评估报告和插件；跑 `<cli> --help` / `--version`；用 `strings` 读三个已安装二进制里的内嵌源码和文档。没有调用任何模型。
> 回归：`PACTIFY_HOME=$(mktemp -d) go test ./internal/sub/... ./internal/cockpit/... ./internal/acp/...` 全部通过（sub 151s）。
> 标 **[待验证]** 的是从二进制字符串推断、还没有实跑确认的结论。

---

## 0. 结论先行

1. **"实验性"这个标签现在混了两件事**：一是适配层保真度（失败能不能识别、token 准不准、只读能不能用），二是模型质量（eval 一次完成率）。前者可以由 pactify 修好，而且是可以测量的；后者要按 (harness, 模型) 路线来评，不该算到 harness 头上。opencode 只测过 deepseek-flash，kimi 只测过 kimi-for-coding，agy 只测过 gemini-3.8-flash-high。
2. **最严重的问题在适配层，不在模型，而且现在是静默错误**：
   - **kimi 的失败会被报成成功。** kimi 2.2.0 的 ACP 实现把非鉴权的失败轮（限流、连接错误、过载）映射成 `stopReason: end_turn`；pactify 据此判为 succeeded。
   - **kimi、opencode 的临时错误重试实际上从未生效。** 两家的失败规则表里都没有 `transient` 类，所以 `TransientRetry: true` 对它们是死代码；kimi 额度耗尽也无法识别，回退链也就不会从 kimi 触发。
   - **kimi 的 token 会少算。** 只读了 `agents/main/wire.jsonl`，kimi 子代理的用量在 `agents/agent-N/` 下，没有计入。
   - **ACP 的非 end_turn 停止原因（refusal / max_tokens）一律按成功处理。**
3. **只读档三家都能补回来**，而且已安装的版本里就有机制：
   - opencode：用配置规则 `deny`（不终止循环）或 `experimental.continue_loop_on_deny: true`。评估里"拒绝即结束"正是因为 pactify 用 ACP 审批拒绝（`RejectedError`）去拦。
   - agy：用 hooks（`PreToolUse` 的 deny/allow、`Stop` 的 continue）加 `--sandbox`。
   - kimi：没有协议内的解法，建议走"一次性 worktree 隔离只读"。
4. **写档的隔离强度不对等**：codex 写档跑在 OS 沙箱（workspace-write）里，kimi、opencode、agy 写档是对整机无限制的自动批准。这一点要么补上（agy 有 `--sandbox`），要么在结果信封里明确标出。
5. **建议的一等公民门槛**：适配一致性套件全过（见 §5 的 P0、P1），加上至少一条 (harness, 模型) 路线做到大任务 ≥7/10、只读 3/3。

---

## 1. 传输方式与版本

| harness | 实际传输 | 入口代码 | 版本校验 |
|---|---|---|---|
| codex | `codex app-server`，stdio 上的 JSON-RPC 2.0（换行分隔）；每个 job 一个进程，`thread/start\|resume` + `turn/start` | `internal/cockpit/codex.go:19-24, 65-129` | `codex --version`；`.github/workflows/codex-schema.yml` 每周把 codex@latest 的 schema 与 `codex.go` 的读取做契约检查（硬门禁）和漂移检查（软信号）；本机 verified 0.162.1 |
| kimi | `kimi acp`，stdio 上的 ACP（`session/new`、`set_config_option`、`set_mode`、`prompt`、`cancel`、`load`）；**token 和模型从本地 `wire.jsonl` 事后读取** | `internal/cockpit/acp.go:125-137`；`internal/cockpit/kimirecord.go:51-104`；`internal/sub/kimi_usage.go:14-56` | `kimi --version`；verified 2.2.0；ACP 没有 schema 漂移 CI（spec §11 列为 M3，未做） |
| opencode | `opencode acp`，stdio 上的 ACP；权限经 `OPENCODE_CONFIG_CONTENT` 注入；**模型、token、成本事后用 `opencode export <sid>` 核实** | `internal/cockpit/opencode.go:19-49`；`internal/sub/opencode_usage.go:31-104` | verified 1.19.0；无 CI |
| agy | 命令行 headless：`agy --input-format stream-json --output-format stream-json --print-timeout 0s [--model] [--effort] [--conversation <id>] [--dangerously-skip-permissions]`，**每轮起一个新进程**，提示词经 stdin 传入 | `internal/cockpit/agy.go:123-199` | verified 1.3.6，**本机已是 1.4.0，处于未验证状态**；`agy changelog` 的本地输出最新只到 1.3.6，1.4.0 的变更说明不可得；无 CI |

公共部分：
- 版本漂移提示：`internal/sub/worker.go:146-157`。
- 未验证版本警告：`internal/sub/verified.go:29-42`。
- 自检：`pactify sub doctor` 会写 `verified-versions.json`（`internal/sub/doctor.go:323-327`）。

---

## 2. 能力矩阵

图例：✅ 完整 · 🟡 部分或有缺陷 · ❌ 没有 · ⛔ 被禁用

| 能力 | codex | kimi | opencode | agy |
|---|---|---|---|---|
| **流式事件 / 进度** | ✅ item、delta、tool、tokenUsage、rateLimits 通知（`codex.go:832-883`） | 🟡 ACP `agent_message_chunk`、`tool_call`、`plan`（`acp.go:258-286`）；**非终态事件用非阻塞发送，缓冲 64，满了就丢**（`acp.go:237-254`） | 🟡 同 kimi，外加 usage 阻塞发送（`acp.go:243`） | ✅ `step_update` 文本增量和工具步骤（`agy.go:316-344`），缓冲 256，阻塞发送 |
| **最终文本** | ✅ `item/completed.text`，标 Final（`codex.go:861`） | 🟡 最后一次工具调用之后的 chunk 拼接（`collect.go:97-109, 155-160`）；chunk 被丢弃时会被截断 | 🟡 同 kimi | ✅ 最后一个"说过话"的 `agent_response` 步（`agy.go:284-287, 361-364`，AJ-661） |
| **会话 id / 续接** | ✅ `thread/resume`（`codex.go:141-155`） | ✅ `session/load`；续接时 model 和 thinking 用固定 id（`acp.go:489-530`） | ✅ `session/load` 带 configOptions（`acp.go:100-106`） | ✅ `--conversation`；每轮新进程，续接时沿用学到的模型（`agy.go:144-156`） |
| **只读执行** | ✅ `sandbox: read-only` + `approvalPolicy: never`（`codex.go:416-433`）；doctor 实测 | ⛔ `NoReadOnly`（`harness.go:125`）；plan 模式拦得住写，但 3/3 打满工具上限、没有结论 | ⛔ `OpencodeNoReadOnly`（`cockpit/opencode.go:15, 20-22`）；评估 0/3 | ⛔ `AgyNoReadOnly`（`cockpit/agy.go:21, 69-71`）；评估 0/3 |
| **为什么关掉只读** | — | plan 模式下 kimi 反复要求退出 plan、反复提问，始终不交结论（`phase0-report.md:135`） | pactify 用 ACP 审批**拒绝** shell（`worker.go:289-292`）→ opencode 抛 `PermissionRejectedError` → 默认 `shouldBreak` 让本轮结束（二进制：`continue_loop_on_deny!==!0`） | headless 模式对 shell 自动拒绝（stderr 出现 `auto-denied`）→ 本轮结束（`agy.go:207-211, 378-379`）；`--mode plan` 不是沙箱（spec §5.4） |
| **写档与审批** | ✅ `workspace-write` + 网络开放 + 可写根（`codex.go:435-444`）；**有 OS 沙箱** | 🟡 默认模式直接写，worker 自动批准所有审批（`worker.go:289`）；**没有沙箱** | 🟡 build 模式 + `permission{edit,bash,webfetch: allow}`（`opencode.go:37, 64-77`）；**没有沙箱** | 🟡 `--dangerously-skip-permissions`（`agy.go:157-159`）；**没有沙箱**（`--sandbox` 没用上） |
| **选模型** | ✅ `thread/start.model`，按启动响应核对（`codex.go:524-541`） | ✅ ACP `set_config_option(model)`，回报值核对（`acp.go:406-435`） | ✅ 同 ACP，build 模式后再核一次（`opencode.go:78-83`） | ✅ `--model`，与 `init.model` 比对，不一致判失败（`agy.go:312-314`） |
| **模型发现** | ✅ `model/list`（`codex_models.go`），带 effort 元数据 | 🟡 **读 `config.toml` 的 `[models]` 键**（`kimi_models.go:24-74`），不是 ACP 的实时列表 | ✅ 启动一次 ACP 读 configOptions（`opencode.go:89-113`），需要 15s 并起进程 | 🟡 `agy models` 要联网，负载下会超时；预检失败时放行（`agy.go:72-90`，AJ-671） |
| **推理强度** | ✅ 每轮 `effort` | ✅ ACP `thinking`（category `thought_level`），动态校验（`acp.go:436-456`） | ✅ ACP `effort` 归入 `thought_level`（`opencode.go:53-57`） | 🟡 `--effort` 直接透传；`EffortValues` 为空、不校验；`info.Effort` 取请求值，不是回报值（`agy.go:151-153, 310`） |
| **token 精度** | ✅ 各线程 `tokenUsage.total` 求和（`collect.go:120-140`） | 🟡 wire.jsonl 里 `usage.record` 的 `turn` 范围求和；**只算 main agent**（`kimirecord.go:40, 58`），本机 user 会话 447 个 main 里约 70 个有 `agent-N` 子代理目录；等待 wire 刷写超时就标 partial（`kimi_usage.go:46-51`） | 🟡 export 后对 assistant 消息求和（`opencode_usage.go:82-98`）；export 失败就标 partial；ACP 的 usage_update 是上下文窗口，已正确忽略（`acp.go:532-562`） | ✅ 流内按步汇总，result 再覆盖；失败时取 max（`agy.go:395-405`） |
| **成本** | 订阅制，不报美元（`cost.go:125`） | 订阅制 | ✅ USD：export 的 `cost`，加 ACP `usage_update.cost` | 订阅制 |
| **额度 / 限流识别** | ✅ 结构化：`rateLimits` usedPercent≥100 带 reset、`codexErrorInfo`、HTTP 429（`codex.go:1103-1175`）；额度快照进摘要 | ❌ 没有任何 quota 规则（`acp.go:566-570`）；**而且 kimi 的限流失败经 ACP 报成 end_turn**，根本到不了分类器 | 🟡 只有文本 `insufficient balance`（`opencode.go:127-132`） | 🟡 文本 `individual quota reached`，能解析 `Resets in`（`agy.go:450-473`） |
| **失败分类 / 临时错误重试** | ✅ 5xx、`serverOverloaded`、`responseStreamDisconnected` → transient（`codex.go:1152-1170`）；`retry.go` 退避 30s/120s，最多 2 次（默认） | ❌ 规则只有 auth 和 model；`TransientRetry: true` 但永远分不出 transient | ❌ 规则只有 quota、auth、model、canceled；`-32603` 明确不判（`acp.go:579`）；opencode 把 `APIError{statusCode,isRetryable}` 压成 `-32603` + `data.errorName`，**而 pactify 丢掉了 `data`**（`acp/acp.go:196-203`） | ✅ 文本规则：503、`: eof`、`stream was interrupted` → transient（`agy.go:450-458`） |
| **取消与进程树清理** | ✅ `turn/interrupt` + 进程组 kill（`codex.go:95-96, 184-192`） | ✅ `session/cancel`；先快照后代，宽限后逐个结束（`acp/process.go:56-110`） | ✅ 同 kimi | ✅ 先 SIGINT，快照后代，宽限后 kill（`agy_process.go:33-76`） |
| **job 级兜底** | 全部经 `store.StartProcess` 跟踪，结束时 `KillHarnesses`（`worker.go:194-205`），带 `PACTIFY_JOB` 环境标记（AJ-691） | 同左 | 同左 | 同左 |
| **受管配置目录 / 与用户配置隔离** | ✅ 进程级 `-c` 关掉 plugins、apps、skills、memories，并按名逐个禁用 MCP（`codex.go:72-87, 470-520`）；不改 HOME | ✅ `KIMI_CODE_HOME` 受管目录，链接 `config.toml`、`credentials`、`device_id`，删除 `mcp.json`（`kimi_home.go:8-17`） | ✅ 两个 XDG 镜像，排除 `opencode`，只链接 `auth.json`，再加 `OPENCODE_PURE` 等（`opencode_home.go:36-45`；`opencode.go:45-47`）；**用户 `opencode.json` 里的自定义 provider 不会带进来**（隐含后果，[待验证] 对 `minimax-coding-plan` 等路线的影响） | ✅ 镜像 HOME，把 `.gemini` 换成受管目录，只链接 OAuth token（`agy_home.go:9-14`） |
| **MCP / 指令 / AGENTS.md** | 保留 AGENTS.md 发现，MCP 全禁 | ACP `mcpServers: []`（`acp/acp.go:431`），没有 `mcp.json`；仓库里的 AGENTS.md 仍按 kimi 默认加载 | 不读 Claude Code 配置、不加载外部 skills、不加载插件；仓库 AGENTS.md 按 opencode 默认加载 | 全局 rules、MCP、插件都不加载（因为 `.gemini` 被替换）；仓库里的 `.agents/`、AGENTS.md、GEMINI.md 仍加载 |
| **会话不留痕** | ✅ Close 时 `thread/archive`（`codex.go:211-233`） | ✅ 受管目录天然隐藏；用户配置模式走 `kimi web` 的 API 归档，失败退回文件（`harness.go:268-299`） | ✅ 受管 XDG；用户配置模式报错，不隐藏 | ✅ 镜像 HOME；用户配置模式报错，不隐藏 |
| **doctor** | 安装、写、模型、只读、**网络**、会话隐藏 | 只读 SKIP；没有网络检查；模型独立核实（wire）；受管目录检查 MCP 未加载（`doctor.go:373-414`） | 只读 SKIP；没有网络检查；模型独立核实（export） | 只读 SKIP；没有网络检查；**模型核实只是自报**（`harness.go:91-93`，`init.model` 来自同一条流） |
| **worktree** | ✅ 通用实现（`worktree.go`），codex 额外把 tmp 加入可写根（`worker.go:214-216`） | ✅ 通用 | ✅ 通用 | ✅ 通用 |
| **测试覆盖** | 按名称约 53 个测试；codex schema 契约 CI | 约 35 个，全部基于 fake 或 fixture | 约 23 个，全部 fake | 约 20 个，全部 fake |
| **真实 CLI 测试** | 只有 doctor（本机手动）；`E0_SMOKE` 是旧的 ACP 桥冒烟 | 同 codex | 同 codex | 同 codex |
| **插件 skill / agent** | `skills/codex` + `agents/codex.md`；支持 `--read-only` | 有；只写档，横幅提示实验性 | 有；只写档；**skill 里少了"前台快速子代理"一段**，和 codex、kimi 的不一致 | 有；只写档 |
| **评估结果** | 9/10；只读 3/3 | 6/10（换新模板重测仍 6/10）；只读 0/3 | 6/10（deepseek-flash；0 次失败，共 $0.28）；只读 0/3 | 1/10（3 次后端出错，1 次空闲超时）；只读 0/3 |

### 跨 harness 的共性问题

- **事件丢弃**：`acpSession.dispatchEvent` 和 `codexSession.dispatchEvent` 对非终态事件都是 `select { case ch<-e: default: }`（`acp.go:250-253`、`codex.go:235-252`）。
  - ACP 的最终文本完全依赖 chunk 拼接，事件被丢时最终文本可能缺块。
  - 被丢的工具事件还会让 `ToolCalls` 少算，并让 `chunks.Reset()` 漏掉，结果把旁白拼进最终文本。
  - codex 的 Final 消息理论上也可能被丢。
- **工具调用上限**（40）只在只读时生效（`worker.go:394`）；**空闲超时**统一 10 分钟（`worker.go:50-52`）。

---

## 3. 为什么三家现在是"实验性"

| harness | 代码里的标记 | 文档与评估依据 |
|---|---|---|
| kimi | `harness.go:121` `Experimental: "kimi is an experimental harness (Phase 0: large tasks completed in one attempt 6/10; read-only work never converged)"`；`harness.go:125` NoReadOnly | `phase0-report.md:10, 15`：Q1 6/10 未达 ≥7，K1 只读 0/3（3/3 打满 41 次工具调用）；重测 4 题仍 6/10，T07 再次改测试去迁就新行为（`:42`）；Owner 决定 M1 以 codex 为默认，kimi 不作默认（`harness-subagents.md:224`） |
| opencode | `harness.go:98`（"6/10; read-only work never converged"）；`cockpit/opencode.go:15` NoReadOnly | `opencode-eval.md:15-27`：6/10、4 个小问题、0 次失败；只读 0/3（shell 被拒后本轮结束）；Owner 2026-10-16 决定保持实验性并关闭只读 |
| agy | `harness.go:83`（"1/10; read-only work never converged"）；`cockpit/agy.go:21` NoReadOnly | `agy-eval.md:10-18`：1/10；10 题里 3 题被 Gemini 后端的 EOF、503、stream 中断打断；只读 0/3；Owner 决定保持实验性、关闭只读、不再测 |

这些标记在下游的体现：
- `HarnessInfo.Experimental` 和 `ReadOnly`（`catalog.go:71`）；
- `cmd_sub.go:194-195` 里只读入口退出码 2；
- 续接时拒绝只读 job（`actions.go:83-84`、`worktree.go:133-134`）；
- doctor 跳过只读探测（`doctor.go:264`）；
- 每个 job 都附一条警告（`worker.go:76-78`）；
- 插件 skill 的 description 和正文都有横幅。

**被低估的事实**：评估时下面三个缺陷都存在，所以三家的"一次完成率"和"可靠性"读数本身也受了 pactify 的影响：
- kimi 失败被报成功；
- opencode、kimi 的临时错误不重试；
- kimi 子代理 token 少算。

---

## 4. 已安装版本里有没有更好的集成路径

### kimi 2.2.0 —— 继续用 ACP，但要从 wire 补齐失败信息

- `kimi --help` 提供的入口：
  - `acp`：stdio ACP；
  - `-p --output-format stream-json`：自动批准一切，`--plan` 不能与 `-p` 同用；按 spec §10，这条路给不出模型和 token；
  - `web`：本机 REST + WebSocket 服务，pactify 只在归档时用它；
  - 另有 `--agent` / `--agent-file`（带 `tools` / `disallowedTools` 的 agent 配置）和 `--yolo` / `--auto`。
- 二进制内嵌源码（strings）里的关键事实：
  - **`turnEndReasonToStopReason`：`failed` → `end_turn`**，只有 `provider.filtered` 例外，映射成 `refusal`；鉴权错误走 `authRequired`。所以除鉴权以外的所有失败，经 ACP 看都是正常结束。
  - 错误码表：`provider.rate_limit`、`provider.connection_error`、`provider.overloaded`、`context.overflow` 标为 `retryable: true`；`provider.auth_error`、`provider.filtered`、`provider.api_error` 不可重试。`turn.ended` 事件带 `reason` 和 `error.code`。
  - ACP 的 `usage_update` 只给上下文用量和窗口大小，注明"`cost` stays omitted"，**不是 token**。现有代码没有误用，结论正确。
  - ACP 的 configOptions 只有 model、thinking、mode；mode 有 `default` / `plan` / `auto` / `yolo`。**agent 配置无法经 ACP 选择**。
- **结论**：传输方式不用换。
  - **要修**：从 `wire.jsonl` 读 `turn.ended` 的 `reason` 和 `error.code` 来判定失败并分类，同时把 `agents/*/wire.jsonl` 全部计入用量。
  - **只读**：ACP 内没有好的解法。plan 模式本身就会循环；`auto` 模式对"安全操作"的定义不透明。推荐 §5 G9 的"隔离只读"。另一个可选方案是在受管 `KIMI_CODE_HOME` 里放一个带 `disallowedTools` 的默认 agent 配置，**[待验证]** config.toml 能否指定默认 agent。
  - `kimi web` 的 API 能否给出结构化的轮次错误和 token：[待验证]。不建议作为主传输。

### opencode 1.19.0 —— ACP 够用，但要把错误结构和只读配置补上；`serve` 作为备选

- `opencode --help` 提供：`acp`、`serve`（无界面 HTTP 服务）、`run --format json` / `--attach`、`--agent`、`--auto`、`export --sanitize`、`agent`。
- 二进制里的关键事实：
  - **权限拒绝的语义**：
    - ACP 审批被拒 → `PermissionRejectedError`，默认 `a.blocked = a.shouldBreak`，循环中止；
    - 配置规则 `deny` → `PermissionDeniedError`，给模型的是一条工具错误（"The user has specified a rule which prevents you…"），**循环不中止**；
    - 还有一个配置开关 `experimental.continue_loop_on_deny`（"Continue the agent loop when a tool call is denied"）。
  - 这就是评估里"拒绝即结束"的根因：问题在 pactify 拦截的方式，不在 opencode 不能做只读。
  - **ACP 的错误映射**（`ACP.promptResponse`）：
    - `MessageAbortedError` → `cancelled`；
    - `MessageOutputLengthError` → `max_tokens`；
    - `ContentFilterError` → `refusal`；
    - `ProviderAuthError` → authRequired；
    - 其余（包括 `APIError{statusCode, isRetryable}`）→ `internalError(-32603)`，`data` 里带 `{service, errorName}`，message 是 provider 的错误文本。**`isRetryable` 和 `statusCode` 在 ACP 层丢了**。
- **路径选择**：
  1. **最小改动，推荐**：继续用 ACP。
     - 只读：在 `OpencodeEnv` 里注入 `permission.edit: "deny"`、`permission.bash` 写成模式表（`"*": "deny"`，只放行 `git status/diff/log/show`、`ls`、`rg`、`grep`、`find` 等只读命令），再加 `experimental.continue_loop_on_deny: true`，用 `plan` 模式或 agent；
     - 失败分类：保留 `rpcError.Data`，并在失败后解析 `opencode export` 里 assistant 消息的 `info.error`（`name`、`data.statusCode`、`data.isRetryable`）。export 本来就要跑，**[待验证]** export 里确实带 error 字段。
  2. **`opencode serve`（HTTP + SSE）**：按消息拿结构化的 `error`、tokens、cost、providerID/modelID，可以按消息选 agent，省掉事后 export 子进程和 export 截断的风险（#104）。代价是要管服务进程的生命周期、端口和 basic auth。只有在 ACP 方案补不全时才值得换。
  3. `run --format json`：一次性进程，控制力不如 ACP，不推荐。

### agy 1.4.0 —— 没有 ACP 或服务模式；用 hooks 和 sandbox 补强 stream-json

- `agy --help` 里没有 ACP 或 server 子命令，二进制里也没有 ACP 方法名（`remote-control` 是远程界面守护进程，不是编程接口）。
- 可用的杠杆：
  - **stream-json 支持一个进程跑多轮**（help：stdin "reads one NDJSON message per line… runs a turn for each"）。现在 pactify 每轮起一个新进程，改成一个进程后，重试轮可以留在同一进程里，少一次冷启动和一次模型预检。
  - **Hooks**（二进制内嵌的 `# Lifecycle Hooks` 文档）：
    - `PreToolUse` 可返回 `allow` / `deny` / `ask` 加 `reason`（"shown to the user/agent"），也可以改写参数；
    - `Stop` 可返回 `{"decision":"continue","reason":…}` 阻止本轮结束，输入里有 `terminationReason`（`NO_TOOL_CALL` / `ERROR` / …）和 `fullyIdle`；
    - `PostInvocation` 可以 `force_continue`。
    - 用途：
      - 用 `PreToolUse` 拦写工具、给命令做白名单，实现只读；
      - 在 `fullyIdle=false` 时用 `Stop` 阻止结束，修掉 T10 / T04 那种"启动测试后就结束本轮"；
      - 在 `terminationReason=ERROR` 时考虑由 hook 续跑。
    - 落点：全局 customization root。镜像 HOME 下 `.gemini` 由 pactify 控制，**不会碰用户仓库**；**[待验证]** 确切路径（推测是 `~/.gemini/config/hooks.json`），以及 headless 下 hook 的 `deny` 是否同样会结束本轮。
  - **`--sandbox`**：带终端限制运行（macOS 默认禁读 `~/Library/Application Support`，见 changelog 1.3.3）。可以给写档加 OS 级约束，也可以作为只读的第二道防线。**[待验证]** 写档下测试还能不能绑定端口、联网。
  - `--json-schema`：强制最终结果为结构化对象，可用来固定结果信封（改动、测试、结论），减少"终稿是旁白"一类问题。
  - `-p "/usage"`（changelog 1.2.13）：额度和重置时间，带 JSON 输出，可以做成和 codex rateLimits 同类的额度快照。**[待验证]** 具体参数。
  - 二进制里有一条 "--mode %s is not supported headless, continuing in the default mode"：headless 下 `--mode` 部分无效。和 spec 一致，`plan` 不能当只读用。

---

## 5. 升为一等公民的差距清单（按优先级）

列说明：**侧**——P = pactify 能修；H = 上游限制。验收条件都应可测量。

### P0 · 静默错误（不修就不能当一等公民，codex 以外的路由结果都不可信）

| # | 差距 | 证据 | 修法 | 验收 | 侧 |
|---|---|---|---|---|---|
| G1 | **kimi 失败轮被报成 succeeded** | kimi 2.2.0 源码 `turnEndReasonToStopReason("failed")` → `end_turn`；pactify 只看 stopReason（`acp.go:199-214`）；`kimirecord.go:76-79` 只数 `turn.ended`，不读 `reason` 和 `error` | 在 `ReadKimiRecordSince` 里解析 `turn.ended.reason` 和 `error.code`；`verifyKimiUsage` 回传 `TurnFailed` 和错误码，worker 据此把 job 置为 failed。映射：`rate_limit` → quota（能解析到重置时间就带上；短时限流可当 transient）；`connection_error` / `overloaded` → transient；`auth_error` → auth；`filtered` → refusal 或 other；`context.overflow` → other | 用 fixture wire 覆盖 6 个错误码，判定和分类都正确；用假 kimi 发 `end_turn` 加失败 wire，job=failed；一次真实复现（例如关网）结果是 failed(transient) 并自动重试 | P |
| G2 | **kimi、opencode 的临时错误永远不重试，额度耗尽不识别，所以不会回退** | `acp.go:566-570`、`opencode.go:127-132` 都没有 transient；`retry.go:21-36`；`worker.go:480`；回退链示例 `"codex": ["@mm","kimi"]`（`router-v1.md:61`） | kimi 见 G1。opencode：在 `rpcError.Error()` 里带上 `data.errorName`，或者按 `errors.As` 暴露 `Data`；失败后解析 export 里的 `info.error.{name,data.statusCode,data.isRetryable}`，`isRetryable` 或 5xx → transient，429 加 balance/quota 文本 → quota。补两家的 `failure_test.go` 表 | `ClassifyFailure` 的表驱动测试覆盖 opencode 的 `-32603`+`APIError` 503 → transient、429 → quota；worker 测试证明 kimi 和 opencode 各触发一次 `transient_retry`；在真实 opencode 上用错误的 base URL 或代理复现 503，`Retries` 非空 | P |
| G3 | **ACP 非 end_turn 停止原因被当成成功** | `acp.go:201` 一律 `StateTurnCompleted`；opencode 会回 `max_tokens`、`refusal`，kimi 会回 `refusal` | `Prompt` 里按 stopReason 映射：`end_turn` → completed；`cancelled` → failed(canceled)（目前只对 opencode 做了，`acp.go:209`）；`refusal`、`max_tokens`、`max_turn_requests` → failed，并给出类别 | 单元测试覆盖 4 种 stopReason × 2 家 | P |
| G4 | **kimi 子代理 token 不计入** | `kimirecord.go:40, 58` 写死 `agents/main`；本机 kimi 会话中约 70 个带 `agents/agent-N`；`kimi_usage.go:34-44` | 统计会话目录下所有 `agents/*/wire.jsonl`；续接的基线从单个 offset 改为按 agent 记录的 map；模型列表仍以 main 为准 | fixture：main 加 agent-0 → tokens 等于两者之和；真实带子代理的任务里 pactify 的 tokens 与 wire 全量重算一致（误差 0） | P |
| G5 | **ACP 和 codex 的非终态事件在背压下被丢，影响最终文本和工具计数** | `acp.go:250-253`、`codex.go:235-252` 的非阻塞发送；ACP 最终文本靠 chunk 拼接（`collect.go:97-109`） | 消息和工具事件改成阻塞发送（带 `<-done` 退出，与终态事件一致），或者在 session 内自己累积最终文本，只把汇总后的结果作为 Final 事件发出 | 慢消费者测试：1 万个 chunk 加 50 个工具事件，最终文本逐字节相等，ToolCalls=50；`-race` 通过 | P |

### P1 · 与 codex 对齐所需的能力

| # | 差距 | 证据 | 修法 | 验收 | 侧 |
|---|---|---|---|---|---|
| G6 | **opencode 只读** | §4 opencode：被拒是 `RejectedError` 才中止；配置规则 `deny` 不中止；有 `continue_loop_on_deny` | 只读时 `OpencodeEnv` 注入：`permission.edit: deny`、`write: deny`、`bash` 用模式表（默认 deny，白名单只读命令）、`webfetch: allow`、`experimental.continue_loop_on_deny: true`；再 `set_mode plan`；worker 仍拒绝所有 ACP 审批；结束时 diff 必须为空，否则判失败；去掉 `NoReadOnly` | 用 doctor 的只读探测（`doctor.go:265-276`）PASS；重跑 R1–R3 共 3/3 收敛、交出结论、0 文件改动；再加 3 个对抗任务（让它"顺便修一下"）0 改动 | P（依赖 H：bash 模式表不是沙箱） |
| G7 | **agy 只读** | headless 自动拒绝即结束（`agy.go:207-211`）；hooks 和 `--sandbox` 已在 1.4.0 提供（§4 agy） | 在受管 HOME 的全局 customization root 写入 pactify 的 `hooks.json`：`PreToolUse` 对 `write_to_file`、`replace_file_content` 等返回 deny 加 reason，对 `run_command` 按白名单 allow 或 deny；只读时用 `--dangerously-skip-permissions`，避免 headless 的自动拒绝，再加 `--sandbox`；结束时 diff 必须为空 | 先实测 hook 的 deny 会不会结束本轮（**[待验证]**）；doctor 只读 PASS；R1–R3 共 3/3；用户的 `~/.gemini` 不变（snapshot 检查已有） | P + H（依赖 hook 语义） |
| G8 | **kimi 只读** | plan 模式循环（`phase0-report.md:135`）；ACP 不能选 agent 配置；`auto` 模式的边界不透明 | 首选 G9 的隔离只读；次选在受管 `KIMI_CODE_HOME` 放一个默认 agent 配置（`disallowedTools: [Write, Edit, …]`），**[待验证]** 能否经 config 设为默认 | R1–R3 共 3/3 收敛；0 改动 | P / H |
| G9 | **通用的"隔离只读"兜底**（需 Owner 拍板：`--read-only` 的语义从"拦截"变为"隔离 + 丢弃"） | 三家的只读失败都源于"拒绝 → 不收敛" | 对没有 OS 沙箱的 harness，只读 job 一律在一次性 detached worktree 里以写权限运行，模型看不到拒绝；结束时 diff 非空就标违规并丢弃，结果里明确写"只读由隔离实现，仓库外副作用未受限"；agy 再叠加 `--sandbox` | 3 家 R1–R3 各 3/3；主工作树 0 改动；违规检测测试（故意写文件 → status=failed，class=readonly_violation）；文档写明安全边界 | P（需 Owner 决策） |
| G10 | **写档没有 OS 沙箱**（codex 有） | `codex.go:435-444` 对比 `opencode.go:37`（bash allow）、`agy.go:157-158`、kimi 默认模式；doctor 的网络检查只给 codex（`doctor.go:279`） | agy 写档加 `--sandbox`，并实测端口和网络；kimi 和 opencode 没有上游沙箱（H），在结果信封和 `HarnessInfo` 里加 `sandboxed=false` 字段，skill 文案说明；doctor 给每家都加"Network"和"Sandbox"检查（WARN 而不是 FAIL） | doctor 为每家输出 Sandbox 行；agy 写档在 `--sandbox` 下跑通 doctor 的网络脚本；结果 JSON 有 `sandboxed` 字段 | P + H |
| G11 | **agy 模型核实只是自报**；agy 推理强度不校验 | `harness.go:91-93`、`agy.go:310`；help 列出 effort 为 `low\|medium\|high\|xhigh\|max` | 给 agy 设 `EffortValues`（help 里的静态表），不在表内就拒绝；能否从受管 HOME 的会话库独立读到实际模型 **[待验证]**，读不到就在结果里标 "self-reported" | 未知 effort 直接退出 2；doctor 的 Model 行如实写明核实来源 | P |
| G12 | **agy 后端可靠性**（3/10 被 Gemini 打断） | `agy-eval.md:42`；agy 规则已覆盖 EOF、503、stream 中断，但评估时 AJ-693 的重试尚未合入（评估 pactify `182afa9` 早于 `d998817`） | 上游问题（H）为主。pactify 侧：在 AJ-693 之后重跑 agy 评估；`Stop` hook 在 `fullyIdle=false` 时 continue（修"启动测试后结束本轮"）；一个进程跑多轮（stream-json）减少冷启动 | 带重试的重跑中，后端错误导致的最终失败 ≤1/10；"后台命令未等完就结束"0 次 | H 为主，P 辅助 |
| G13 | **kimi 模型发现读 config.toml，不读实时列表** | `kimi_models.go:24-74` | 改用 ACP `session/new` 的 configOptions（与 opencode 相同），config.toml 只作离线兜底；缓存沿用 `catalog.go` | 新加入的 provider 无需改 config.toml 就出现在列表里；离线时退回 config 并给出警告 | P |
| G14 | **额度快照**：只有 codex 有 | `collect.go:148-151`；`cost.go` 的 QuotaSnapshots | agy：job 结束后在受管 HOME 跑 `agy -p "/usage"` 的 JSON 输出（**[待验证]**），解析 reset 时间写入 `quota.json`；kimi 和 opencode 暂无上游来源（H） | `sub cost` 显示 agy 的额度快照；quota 失败带 `ResetAt` | P / H |

### P2 · 一致性保障与发布卫生

| # | 差距 | 证据 | 修法 | 验收 | 侧 |
|---|---|---|---|---|---|
| G15 | **没有真实 CLI 的一致性测试，也没有 ACP 漂移 CI** | 三家只有 fake 测试；spec §11 "协议 schema 漂移 CI" 定在 M3，未做；agy 今天已自动升到 1.4.0 | ① 录制真实会话（每家一次写、一次只读、一次续接、一次取消、一次注入失败）成 golden 转录，回放测试适配层；② 加一个每周 workflow：安装 kimi、opencode、agy 的最新版，检查 `--help` 参数，用 ACP 做不需鉴权的 `initialize` 和 configOptions 握手，再 strings 检查关键常量（`continue_loop_on_deny`、`turnEndReasonToStopReason`、hooks 文档）；③ 版本变化时自动提示跑 doctor | 每家 ≥5 条 golden 回放测试；漂移 workflow 绿；人为把 golden 里的字段改名能让测试变红 | P |
| G16 | **doctor 的覆盖不对等** | `doctor.go:279` 的 Network 只测 codex；只读 SKIP；没有"失败分类"探测 | doctor 增加：Network（各家）、Read-only（G6–G9 落地后）、Failure classification（可选的离线注入，例如无效模型或无效 endpoint）、Sandbox 声明 | 4 家的 doctor 输出行数相同、没有 SKIP | P |
| G17 | **插件与标签** | skill 横幅和 description（`skills/{kimi,opencode,agy}/SKILL.md:2, 8-12`）；opencode skill 少了前台子代理一段；agents 写死"always --write"（`agents/agy.md:14-16`） | 各家达标时：去掉横幅；恢复 `--read-only` 文案；三份 skill 对齐 codex 结构；按 memory 规则**升 `plugin.json` 版本** | `plugin-version` CI 通过；`router_skills.bats` 与 skill diff 只剩 harness 名和模型说明的差异 | P |
| G18 | **评估方法**：把 harness 和模型绑在一起评 | opencode 只测 deepseek-flash；kimi 只测 kimi-for-coding | "一等公民"拆成两道门：① **适配一致性**（G1–G16 的验收全绿，加 doctor 全 PASS），按 harness 评；② **路线质量**（大任务 ≥7/10、只读 3/3），按 (harness, 模型) 评，结果记进 router 的默认值。opencode 应补一条强模型路线（遵守 Owner 的选模型规则：Zen 只用 `-free`，否则用 minimax.io）；kimi 在 G1、G4 修复后重测 | 有一份结果表：每家至少一条路线 ≥7/10，只读 3/3 | P（测试成本） |
| G19 | **opencode 最小配置会丢掉用户自定义的 provider** | `opencode_home.go:39-41` 排除了 config 下的 `opencode` 目录 | 只读取用户 `opencode.json` 里的 `provider` 段，经 `OPENCODE_CONFIG_CONTENT` 合并进去（不带 mcp、plugin、agent 段），并在 doctor 里核实路线模型可用 | router 里配置的每条 opencode 路线，在最小配置下 `sub doctor --model <route>` 都能 PASS | P |

### 建议的推进顺序

1. 先做 **G1–G5**，纯 pactify、改动小、能消除静默错误，并用 fixture 测试锁住。
2. 然后做 **G6**（opencode 只读，证据最充分）和 **G18** 的路线评估，让 opencode 最先具备转正条件。
3. 接着做 **G7、G12**（agy 的 hooks 实验），再做 **G8/G9**（kimi 只读，需要 Owner 对隔离只读的语义拍板）。
4. **G15–G17** 作为各家转正前的发布门。

---

## 6. 需要 Owner 决定的事项

1. **只读的语义**：是否接受"隔离 + 丢弃"（G9）作为没有 OS 沙箱的 harness 的只读实现。它不是安全边界，仓库外的副作用不受限。
2. **写档的隔离强度**：kimi、opencode 写档没有沙箱。是只在结果和文档中标注，还是把这一项作为转正的硬门槛？
3. **转正门槛**：是否采用 §5 G18 的两道门（适配一致性按 harness 评，质量按路线评）。
4. **agy**：Owner 已定"不再测"。hooks 和 `--sandbox` 是新的事实，是否值得为 G7、G12 做一次小规模复测（受周额度限制，Argon 约 10-23 重置）。

## 7. 本次没有做的

- 没有调用任何模型，没有跑 doctor，没有改动任何文件或全局配置。
- 所有 **[待验证]** 项都只来自 `--help` 或二进制字符串，需要在隔离的 HOME 里各做一次真实探测确认：
  - kimi wire 里 `turn.ended` 的字段名；
  - opencode export 是否带 `info.error`；
  - agy 全局 hooks 的路径，以及 hook deny 在 headless 下的行为；
  - agy `--sandbox` 下的网络和端口；
  - `agy -p /usage` 的 JSON 输出；
  - kimi 能否指定默认 agent。
