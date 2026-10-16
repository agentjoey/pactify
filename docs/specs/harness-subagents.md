# Spec — Harness Subagents（把其他 coding agent harness 当 subagent 调用）

> Status: **DRAFT** · 2026-10-15 起草，同日完成 Phase 0 前实测（§10）。方向已由 Human Owner 拍板（§0），本文到「方案、验证与分期」为止，**不含实现**。
> 现状描述为源码实读，标注 `文件:行号`（起草时 HEAD = `2950d17`）。仍标 **[待验证]** 的是尚未实测的假设；
> 已实测的结论集中在 §10，正文相应处已改写。Phase 0 范围与路线图于 2026-10-15 按 D5–D7 改写（§6、§7、§11）。

## 0. 定位与已拍板的决策（2026-10-15）

**一句话**：pactify 是 coding agent 的 **harness 路由器**——在 Claude Code 里一个 slash 命令，就把
codex / opencode / agy（Antigravity CLI）/ kimi … 作为 subagent 拉起来执行一个任务，体验接近原厂 subagent，
同时按任务在各家 harness 的能力与订阅额度之间做平衡。类比 `musistudio/claude-code-router`，
但路由的是 **harness**，不是模型。

| # | 决策 | 结论 |
|---|---|---|
| D1 | 产品归属 | **就是 pactify**。不另起品牌、不拆仓库 |
| D2 | 第一版 host | **只做 Claude Code** |
| D3 | 现有协议 / 看板 / 云端 | **降级**：冻结新投入，变成可选的「治理模式」；具体删减见 §8 |
| D4 | gemini-cli | **移出范围**（2026-10-15）：agy（Antigravity CLI）已取代 gemini-cli，Gemini 系模型经 agy 调用。编排器里现有的 gemini-cli 代码随降级冻结，不再验证 |
| D5 | 第一版 callee | **codex + kimi**（2026-10-15）：一个保真度最高（模型可核实、权限干净、有额度事件），一个有已知风险（只读要靠 plan 模式且会循环、协议不回报 token），两者放在一起才能验证适配层能否兜住「不完美的 harness」 |
| D6 | 产品定位 | **主打大任务委派**：固定开销（callee 启动 2–5 万 token + host 转发与读结果）只在大任务上划算；结果信封必须显示本次成本 |
| D7 | 兼容适配 | **作为产品能力规划**（§11）：各家 CLI 会持续自动更新，适配不能靠一次性修代码，要有版本感知、动态模型表与自检 |

**为什么第三方能做、而且只有第三方会做**：host 厂商不会让自家 harness 拉起竞品（商业上不允许）；
callee 厂商会做「别人的 host 调我」（`openai/codex-plugin-cc` 34.2k★ 已做 Claude→Codex），
但没人会做「任意 host × 任意 harness + 按成本/能力选路」这个中立层。
与 CCR 的结构差异：pactify 拉起的是**各家官方二进制、用各家官方登录**，不碰鉴权与代理，
每份订阅都在其条款内被使用——「把重活从 Claude 额度挪到 Codex 额度」本身就是合规的成本平衡。

## 1. 目标场景

```
（Claude Code 会话中）
> /pactify:codex 把 internal/stats 的 per-task LOC 归因改成按 commit SHA 计算，补回归测试
  ⎿ codex · gpt-5.x · workspace-write · 4m12s · 38k tok
    改了 3 个文件（+84 −21）· 测试已跑 ✅
    [codex 的终稿摘要……]
    续接：/pactify:codex --resume …   完整日志：~/.pactify/jobs/<id>/log
```

- 主线程的 Claude 只负责**转写任务 + 读回结果**，探索、编辑、跑测试都在 callee 里完成。
- 长任务转后台，完成时以 Claude Code 原生的后台任务通知回到主线程（§5.2）。
- 同一 repo 可以 `--resume` 续接上一次的 callee 会话。

## 2. 非目标（第一版）

- 不做 Claude Code 以外的 host（D2）。
- 不做模型代理 / 鉴权转发（那是 CCR 的地盘，且有条款风险）。
- 不做多 agent 编排、任务状态机、看板——这些属于降级后的「治理模式」（§8）。
- 不在 callee 里中转审批到 host 的审批 UI（M1 只做「只读 / 可写」两档预设，§5.4）。

## 3. 原则（实现不得违背）

- **P-1 官方二进制 + 官方登录**：只拉起用户本机已装、已登录的官方 CLI / server，不读写其凭据。
- **P-2 模型绑定要么兑现、要么明说**：请求的模型无法在所选传输上兑现时，**拒绝执行或在结果首行标红**，
  绝不静默降级。（这是 `[ACP-MODEL]` 那次 243 次静默失真的教训。）
- **P-3 权限语义统一**：用户只面对 `--read-only` / `--write` 两档，由 pactify 映射到各 harness 的原生参数。
- **P-4 结果有界**：回到 host 上下文的只有结果信封（§4.3），完整日志落盘给路径，不回灌整段过程。
- **P-5 host 侧开销最小**：转发 subagent 用最便宜的模型（haiku），只做一次 Bash 调用，不读代码、不轮询。
- **P-6 大任务优先**（D6）：skill 的描述与转发规则引导「多文件、需要探索或跑测试」的工作外派，小改动留给 host 自己做；
  每次委派在信封里报告成本（token，能拿到时附美元），让用户看到「挪出去是否划算」。
- **P-7 版本感知**（D7）：每个 job 记录 callee 的 CLI 版本；遇到未经自检的新版本时先跑自检再执行，自检不过就明说，不硬跑。

## 4. 架构

```
Claude Code 插件（plugins/pactify，薄皮）            pactify 运行时（Go，复用现有代码）
──────────────────────────────────────            ─────────────────────────────────────
commands/ codex.md opencode.md … sub.md            pactify sub run|status|result|cancel|wait|list
          jobs.md（status/result/cancel）            ├─ job store   ~/.pactify/jobs/<id>/
agents/   <harness>.md                              ├─ worker      detached 进程，持有 harness 会话
          (model: haiku, tools: Bash, 薄转发)        ├─ adapters    复用 internal/cockpit 的 Session
hooks/    SessionStart：检查 pactify 是否可用          └─ router      ~/.pactify/router.json（M2）
skills/   brief 写法（如何把任务转写成自足的 brief）
```

### 4.1 运行时 CLI 面

```
pactify sub run <harness> [--model M] [--read-only|--write] [--resume|--fresh]
                [--wait 540s] [--cwd DIR] [--json]  < brief
pactify sub wait <job>        # 阻塞到完成；给主线程 run_in_background 用（§5.2）
pactify sub status [<job>] | result <job> | cancel <job> | list
pactify sub doctor            # 哪些 harness 装了 / 登录了 / 能兑现模型绑定
```

`sub status --json` 的共享任务摘要字段、兼容规则和过滤选项见 [任务摘要契约](../sub-status.md)。

`run` 的行为：起一个 **detached worker**（独立进程组）持有 harness 会话并写 job 状态；
前台 CLI 只是等 job 文件。`--wait` 到点仍未完成 → 返回「仍在运行 + job id」，worker 继续跑。
这样同时解决 Claude Code Bash 单次 10 分钟上限和 host 重启后找回 job。

### 4.2 Adapters：以 cockpit Session 为底，不用 orchestrate 的 Runner

| 选项 | 现状 | 结论 |
|---|---|---|
| `orchestrate.Runner` | 接口只返回 `error`（`internal/orchestrate/runner.go:81-83`），终稿从不提取；子进程 stdin/stdout 直连父进程（`runner_idle.go:108,121`） | 不用 |
| `cockpit.Session` | 统一事件流含文本/终稿/工具/用量（`internal/cockpit/backend.go:61-70`），带审批流与 `ThreadID()` 续接（`:88-95`）；codex app-server / claude bridge / ACP 三类后端都已实现 | **采用** |

需要补的（全是定点改动，依据见 §10 实测）：

- **codex**：`StartOpts.Model` 字段已存在（`backend.go:12`），但 `threadStart` 只传 `cwd`
  （`internal/cockpit/codex.go:283`）。补 `model` / `sandbox` / `approvalPolicy=never`，每轮传 `effort`。
  启动响应回报的 `model` / `modelProvider` 即实际模型（实测与请求一致）；其中 `reasoningEffort` 是**会话默认值**，
  不反映每轮传入的 `effort`。随附 schema 需刷新到当前版本（0.144.4 → 0.162.1 相关字段只有新增、无删改）。
- **终稿文本**：codex 0.162.1 的 `item/completed`（agentMessage）**已带 `text`**，现有代码（`codex.go:560-561`）没读它，
  改为直接取；ACP 由 `agent_message_chunk` 累加；Claude 取 `result` 消息。
- **ACP 模型参数（历史验证）**：opencode 与 kimi 的 `session/new` 都返回 `configOptions`，其中 `id=model` 的选项带当前值与可选列表；
  `session/set_config_option`（以及 `session/set_model`）切换后，opencode 日志证实实际改跑所选模型。
  **因此新方案在协议内设模型，不再需要 #55 的 `OPENCODE_CONFIG_CONTENT` 注入。** kimi 还有 `id=thinking`（推理强度）
  与 `id=mode`（工作模式）两项。kimi 的 ACP 不回报 token 与实际模型，但本地会话记录
  `~/.kimi-code/sessions/<工作目录>/<sessionId>/agents/main/wire.jsonl` 逐轮记有 `model` 与 `usage`，实测与协议内设定一致，
  用它补齐核实与成本（路径经 `~/.kimi-code/session_index.jsonl` 按 sessionId 查）。
- **agy v2 stream-json backend (AJ-711, Antigravity CLI 1.4.0)**: one persistent process per job, with `--input-format stream-json --output-format stream-json --model <model> --print-timeout 0s`. Each JSON-encoded user line runs one turn in the same conversation. Model selection uses router defaults or `gemini-3.8-flash-low`; discovery and `/usage` also always pin flash.
  - Verify `init.model`; sum step usage (`input_tokens + cache_read_tokens`, output tokens), since result usage and turn counts are cumulative.
  - Transient errors use AJ-693 backoff and continue in the same process. EOF restarts with `--conversation <id>`; explicit job resume uses that same handle.
  - Empty answers, empty answers with denied actions, and print-timeout empty results fail with categories. ACTIVE tool steps and conservative unfinished-reply patterns trigger up to two extra turns asking for actual results (`--completion-followups`). At the limit, retain a non-empty reply with a warning; active tools are recalculated per turn.
  - Read-only uses plan mode, an exact command allowlist enforced by the compiled managed hook, managed PreToolUse denial, and AJ-710's fail-closed tracked/untracked/ignored baseline. Write access has `sandboxed=false`, with no `--sandbox`; managed hooks reject `BypassSandbox`. Write `--with-user-config` reports `managed hooks are not installed with --with-user-config`; read-only rejects that opt-out. Read-only refuses `.agents`, `.agent`, and `_agents` workspace directories through the repository root. The baseline covers only the repository; the hook denies writes outside it.
  - Non-billable `/usage` snapshots before and after jobs retain bucket ids, remaining fractions and reset times in quota-snapshots.json, result JSON, cost and Dashboard Quota. Quota failures retain ResetAt. Opening snapshots have a 5-second budget; terminal results publish before the bounded 10-second closing snapshot. Snapshot failures are warnings.
  - Cancellation snapshots descendants before SIGINT, terminates survivors after grace, and checks process identity. Doctor requires real denied-write evidence (WARN when the model never attempts it), an independent write-mode read-only hook probe, and Network evidence. Probe process markers include the disposable store identity to prevent cross-store cleanup collisions.
- **精简 callee 的启动配置**：实测每次委派的输入基线约 2–5 万 token，主要来自 callee 加载用户全部的 MCP 服务器与插件
  （codex 启动了 17 个 MCP 服务器；claude 带了 359 个工具）。新方案应默认以最小配置启动 callee（ACP 会话已传
  `mcpServers: []`；codex 经 `ThreadStartParams.config`、claude 经 `--strict-mcp-config` 一类参数，**[待验证]**）。

### 4.3 结果信封（回到 host 的唯一内容）

```
status · harness · model(请求 → 实际，不一致标红) · 权限档 · 耗时 · tokens
改动：N 个文件（+a −b）+ diffstat 前 20 行（internal/diffstat 已有）
终稿：callee 最后一条消息，截断到 ~4 KB
续接：session/thread id · 完整日志路径
```

`--json` 输出同一内容的结构化形式，供 skill / hook 使用。

## 5. 关键设计点

### 5.1 上下文交接（体验成败的关键）

callee 冷启动，没有 host 的对话上下文。第一版做法：转发 subagent 按 skill 把用户请求转写成
**自足 brief**（目标、相关文件路径、约束、验收标准），callee 自己读 repo。
不传会话记录全文（成本高、各 harness 无通用导入格式；codex 有官方导入器，可作为 M2 之后的 codex 专属增强）。
**Phase 0 必须度量**：同一批任务，brief 质量是否足以让 callee 不需要回问就完成。

### 5.2 前台 / 后台与完成通知

- 小任务：转发 subagent 前台 `pactify sub run … --wait 540s`，结果直接成为 subagent 的返回。
- 大任务：`run` 立即返回 job id，**主线程**用 Bash `run_in_background` 跑 `pactify sub wait <job>`，
  进程退出即触发 Claude Code 原生的后台完成通知。按官方文档：subagent 自己起的后台命令，完成通知只回到该 subagent，
  前台 subagent 结束时它的后台命令也会停——所以「等结果」这一步必须放在主线程，或把转发 subagent 设为 `background: true`。
- 补充（官方文档）：Bash 前台命令到达超时会被**转到后台继续跑**而不是被杀（`BASH_MAX_TIMEOUT_MS` 默认 10 分钟，可调）；
  主线程的 MCP 工具调用超过 2 分钟也会自动转后台（`MCP_TOOL_TIMEOUT` 默认约 28 小时）。后者让「MCP 工具形态的 delegate」
  也成为可行的备选，M4 时再比较两种形态。

### 5.3 隔离

默认在**当前工作树**运行（与 codex-plugin-cc 一致，最符合「像原生 subagent」的直觉），但：
- `--write` 且转后台时，**默认进 worktree**（`internal/gitx` 的 `AddWorktree` 已有），结果给 worktree 路径，
  由 host 决定合并还是丢弃——避开 host 与 callee 同时改同一棵树（`CommitAll` 同类问题）。
- **已实测**：subagent 设 `isolation: worktree` 时，Bash 在 `.claude/worktrees/agent-<id>`（从当前 HEAD 新开分支）里执行；
  没有改动时 worktree 自动清掉，有改动则保留。前台可写任务可直接用这个原生隔离。

### 5.4 权限映射（P-3）

| 档位 | codex | opencode（serve） | kimi（ACP） | agy (persistent stream-json) | claude（CLI / SDK） |
|---|---|---|---|---|---|
| `--read-only` | `sandbox: read-only` ✅ | `agent: plan` + 拒绝 permission；diff 非空记 readonly_violation（AJ-710） | `session/set_mode plan` ✅⚠️ | `--mode plan` + exact allowlist + managed hooks + fail-closed baseline (AJ-711) | `--allowedTools=Read,Grep,Glob` ✅ |
| `--write` | `sandbox: workspace-write` + 网络开放（`sandbox_workspace_write.network_access`，2026-10-15 Owner 拍板）✅ | build + permission 自动应答 once ✅ | 默认模式（直接写）✅ | `--dangerously-skip-permissions` ✅（1.3.5：`--mode=accept-edits` 只放行写文件，shell 命令会被自动拒绝；`--mode=auto` 对本账号不可用） | `--permission-mode acceptEdits` ✅ |

**agy (AJ-711)**: read-only access is restored in v2. The AJ-683 closure is superseded;
agy remains experimental pending the separate host evaluation.

**opencode（AJ-710，2026-10-16）**：恢复只读档，保持实验性。
每个 Start 启动独立的 `opencode serve --hostname 127.0.0.1 --port 0`，随机密码、Basic Auth，
仅用 v1 HTTP/SSE；续接保留受管 XDG 数据库。只读使用 plan，用 session 有序权限覆盖用户规则，危险 bash 一律拒绝；结束时对比包含 ignored/untracked 的文件基线，任意新增、修改、删除均记
`failed(readonly_violation)`，无法核验则 failed。写档使用 build，主/子 session permission 自动应答 once。
SSE 断流最多退避重连五次，耗尽归 transient；消息和用量分别从最终消息列表及父/子会话汇总恢复。Stop 只 abort 当前 turn，job 结束回收 server 进程组。
两档均无 OS 沙箱，结果和 harnesses 接口标 `sandboxed=false`；只有 codex 为 true。
模型列表取 `/config/providers`，用量和成本取 `GET /session/{id}` 会话汇总并扣除启动基线，不再跑 export。
取消先调用 v1 `/abort`，随后回收进程组及已跟踪的子进程。

✅ = 2026-10-15 实测：只读档写文件被拦、可写档写成功。⚠️ kimi 的 plan 模式拦得住，但被拒后会反复申请退出、反复提问
（一轮 32 次工具调用），需在 brief 里写明只读且对 `ExitPlanMode` 自动拒绝并附说明，同时设每轮调用上限。
kimi 的默认模式名为「手动审批」，实测却**不发权限请求直接写**，所以只读不能靠拒绝请求实现。
kimi 命令行 `-p` 一律自动批准，且 `--plan` 不能与 `-p` 同用——**kimi 的只读只能走 ACP**。
claude 的 `--allowedTools` 是变长参数，后面紧跟提示词会被吞掉，必须写成 `--allowedTools=…` 或经 stdin 传提示词。

### 5.5 host 侧开销

转发 subagent 用 haiku、只做一次 Bash；host 读回的只有信封。Phase 0 度量
「host 额外 token / callee 完成的工作量」，确认挪走重活确实划算。

## 6. Phase 0 — 用 codex + kimi 验证「大任务委派」值不值得用（约 1.5 周）

**范围**：codex（app-server）+ kimi（ACP），前台与后台两种形态，只读与可写两档。
插件入口先用一个最小 skill（`/codex`、`/kimi`）+ 转发 subagent + `pactify sub run|wait|status|result`；
结果信封、job 存储、版本记录按 §4 的最小形态实现，路由器、兼容自检的完整版不在 Phase 0。

**任务集**：从本仓 backlog 与近期 PR 里挑 **10 个大任务**（判定：涉及 ≥3 个文件，或需要探索代码、跑测试才能完成；
host 自己做预计 ≥10 分钟）。每个任务 codex、kimi 各跑一次，共 20 次委派；另挑 3 个只读任务（调研、评审类）给两家各跑一次。

| 编号 | 问题 | 通过标准 | 怎么测 |
|---|---|---|---|
| Q1 | 交接质量 | 每家 10 个大任务中 ≥7 个一次完成，无需人工补上下文 | 人工判定 + 跑任务自带的验收（测试 / 构建） |
| Q2 | host 开销 | host 侧额外 token 中位数 < 1 万 / 次 | 主线程 + 转发 subagent 的 token 记录 |
| Q3 | 后台通知 | 后台委派完成后，主线程收到原生完成通知并读回信封 | 至少 5 次后台委派全部到达 |
| Q4 | 成本可见 | 20 次委派的信封里都有 token；codex 附额度事件 | 检查信封 |
| Q5 | 模型兑现 | 请求的模型 = 实际模型，20 次全部一致 | codex 读启动响应；kimi 读本地会话记录（§10） |
| K1 | kimi 只读 | 3 个只读任务里 plan 模式不出现循环（工具调用 ≤ 上限），且无文件改动 | brief 写明只读 + 对退出 plan 的请求自动拒绝并附说明 + 每轮调用上限 |
| K2 | kimi 核实 | 每次委派都能从 `~/.kimi-code/sessions/…/wire.jsonl` 读出实际模型与 token | 按 ACP 返回的 sessionId 经 `session_index.jsonl` 定位 |
| K3 | 版本漂移 | Phase 0 期间 CLI 自动更新后（本机 kimi 2.2.0 已在待装），自检能发现差异并给出明确提示 | 更新前后各跑一次 §10 的探测 |

**停止线**：Q1 或 Q2 任一不达标 → 先改交接方式（skill 的转写规则、brief 模板），不加新 harness、不进 M1；
K1 不达标 → kimi 第一版不开放只读档（写进文档），不影响 codex 推进。

**产出**：一份 Phase 0 报告（每次委派的成本、耗时、是否一次完成、失败原因），作为 M1 的输入。

**Phase 0 结论（2026-10-15，详见 [`../phase0-report.md`](../phase0-report.md)）**：26 次委派（10 大任务 × 2 家 + 3 只读 × 2 家）。
codex 全部达标（Q1 9/10）。Q2–Q5 达标：Q2 的 host 内容开销中位数 3.3k；Q4 实测中发现 codex token 少报约 17 倍、额度事件没进摘要，已修。
**kimi 的 Q1 为 6/10，未达标** → 按停止线先改交接方式：brief 模板已加入全量测试命令、隐含约束、禁止为迁就新行为改旧测试；是否先重测 kimi 再进 M1，待 Owner 拍板。
**K1 未达标**（3/3 只读任务打满工具调用上限、无结论）→ 第一版不开放 kimi 只读档，`sub run kimi --read-only` 拒绝执行并指向 codex。
K2 达标。K3 实测期间两家 CLI 都没有更新，未触发；版本变化提示已实现。
**补测（同日）**：用新模板重测 kimi 失败的 4 题，一次完成数仍为 6/10（T07 再次改测试迁就新行为）；同模板下 codex 补上了 T07 的关键缺口。**2026-10-15 Owner 拍板**：M1 以 codex 为默认 harness，kimi 不作默认；codex 可写档开放网络（全量测试可起本地服务、可拉依赖），只读档仍不联网。

## 7. 路线图

| 里程碑 | 内容 | 退出条件 | 量级 |
|---|---|---|---|
| **M0 · Phase 0** | §6：codex + kimi 的最小可用链路与 10 个大任务实测 | §6 停止线全部通过 | ~1.5 周 |
| **M1 · 首个可发布版本**（v0.13.0） | codex + kimi 正式支持：完整 job 管理（后台、续接、取消、日志）、结果信封含成本、只读/可写、worktree 隔离流程、`sub doctor` 自检 v1（§11）、插件 skill 打磨；README 与文档站切换到新定位 | 公开发布；本仓日常开发自用两周无阻塞问题 | ~2–3 周 |
| **M2 · 扩展 harness + 路由 v1** | 接入 opencode（多提供方、美元成本）、agy（多模型系）、claude-code（GLM 等换提供方场景）；router v1：命令别名 → 默认 harness、按任务类型选路、额度/限流时的回退链（复用 `[FALLBACK-PAR]` 思路）、跨委派的成本汇总 | 5 个 harness 均通过 §11 自检；回退链有真实触发记录 | ~3 周 |
| **M3 · 稳定性与精简** | §11 兼容层完整版（版本矩阵、协议 schema 漂移 CI、callee 最小配置启动）；云端代码及其 CI 已删除（AJ-664）；web 看板与 serve 的去留单独决定（§8，每项单独 PR、单独确认） | 二进制与仓库体积显著下降；CLI 自动更新后 24 小时内能发现不兼容 | ~2 周 |
| **M4 · 可选扩展** | 治理模式：`sub run --task T` 记入 pact 账本，callee = worker、host = reviewer；第二个 host（Codex 作为调用方）；MCP 工具形态的 delegate（§5.2 备选） | 视 M1–M3 的使用数据再定 | — |

M2 剩余部分（router v1、跨委派成本汇总）的设计见 [`router-v1.md`](router-v1.md)。

**并行的维护线**：每个里程碑结束时跑一遍 §11 自检并更新「已验证版本」表；backlog 里随降级冻结的条目不排期。

## 8. 降级（D3）的具体含义

| 资产 | 处理 | 何时 |
|---|---|---|
| 云端 | **已退役**：Go 云端（#97）、web 托管模式（#100）、共享 workspace 及构建 / 发布 CI（AJ-664）已删除；仓库外资源另行处理 | 仓库内清理已完成（2026-10-16） |
| web 看板 + `serve`（~20.7k TS + 6.5k Go） | **冻结**：治理模式相关功能不再新增，只保证能构建；`cockpit` 包从 serve 中独立出来供 `sub` 使用。**例外（Owner 2026-10-16）**：dashboard 新增只读的 Jobs 页，用于查看委派任务，经 `pactify.agentjoey.ai`（Cloudflare Tunnel + Access）访问（AJ-689，见 [`jobs-view.md`](../jobs-view.md)）。含路由配置的写操作（AJ-700），仅 serve | 立即冻结；简化 / 删除在 M3，单独决定，届时需考虑 Jobs 页的去处 |
| pact 协议 + 账本 + `pact` 插件 | **保留为可选的治理模式**；不再作为产品主线叙事 | — |
| 单一规范账本 spec | **暂停在 WS-B**（已合入、flag 默认关，不影响现网行为） | 立即 |
| 绑在上述资产上的 backlog 条目（`[UI-GATE]`、`[CLOUD-DOWN]`、`[BACKLOG-LIST]`、`[RUNGUARD-DEAD]` 等） | 标记「随降级冻结」，不再排期 | 立即 |

删除类动作（云端代码、web、serve）都是大范围不可逆改动，**每一项单独提 PR、单独确认**，不在本 spec 的授权范围内。

## 9. 开放问题

1. **命令命名**（官方文档已答）：插件 `commands/` 下只能用 `/pactify:codex`；做成插件 **skill**（`skills/codex/SKILL.md`）
   则在无冲突时可用短形式 `/codex`，`/pactify:codex` 始终可用。第一版用 skill。注意插件 subagent 会忽略
   `permissionMode` / `mcpServers` / `hooks` 三个字段。
2. **与官方 codex 插件共存**：用户同时装了 `codex-plugin-cc` 时，`/pactify:codex` 是否改为委托给它，还是各走各的。
3. **README / 文档站叙事**切换到新定位——已排进 M1。

## 10. Phase 0 前实测结果（2026-10-15）

在临时目录的一次性 git 仓库中，用每家最便宜的模型档实测。gemini-cli 按 D4 不再验证。
以下为历史能力快照；agy read-only is restored (AJ-711), as is opencode (AJ-710)，权限映射以 §5.4 为准。

| 项目 | codex 0.162.1 | claude 2.1.300 | opencode 1.18.34 | kimi 2.0.0 | agy 1.2.15 |
|---|---|---|---|---|---|
| 调用通道 | app-server | CLI `-p`（SDK 同源） | `opencode serve`（v1 HTTP/SSE，AJ-710） | `kimi acp` | CLI `-p --output-format json` |
| 设模型 | `thread/start.model` ✅ | `--model` ✅ | `set_config_option` ✅ | `set_config_option` ✅ | `--model` ✅（未知名直接报错） |
| 读出实际模型 | 启动响应 `model`/`modelProvider` ✅ | init 消息 `model` + `modelUsage` ✅ | `configOptions.currentValue`，日志证实一致 ✅ | 协议内只有配置值；本地会话记录 ✅ | 结果 JSON 无模型字段 ❌ |
| 推理强度 | 每轮 `effort` | `--effort` | 随模型 | `id=thinking` 选项 | 模型名后缀 |
| 最终回复 | `item/completed.text` ✅ | `result` ✅ | `agent_message_chunk` ✅ | `agent_message_chunk` ✅ | `response` ✅ |
| token / 成本 | `thread/tokenUsage/updated`（含上下文窗口）+ `account/rateLimits/updated` | `usage` + `total_cost_usd` | `usage_update` 含**美元成本** | ACP 不回报；本地会话记录有逐轮 `usage` ✅ | `usage` ✅ |
| 只读 / 可写 | 见 §5.4 ✅ | ✅ | ✅ | ✅⚠️ | ✅ |
| 续接 | `thread/resume` | SDK resume | `loadSession` / `resume` | `loadSession` / `resume` | `--conversation <id>` |
| 每次委派输入基线 | ~2 万 token（17 个 MCP 服务器） | ~4.8 万（含缓存，359 个工具） | ~1.9 万 | — | ~2 万 |

**实测中发现的现有代码问题**（v0.12.0 已发布版本中存在，随降级冻结，记入 backlog）：

- **agy 模型表过期**：`internal/agent/launch.go` 写死的 `gemini-3.7-flash-*` / `gemini-3.1-pro-*` 已不被识别，
  当前为 `gemini-3.8-flash-{high,medium,low}`、`claude-opus-5-5-*`、`claude-sonnet-5-5-*`、`gpt-oss-120b-medium`。
  ⇒ 编排器用默认模型拉起 agy 会直接失败。agy 还新增了 `--mode {accept-edits,auto,plan}` 与 `--sandbox`。
- **kimi 2.0.0 配置迁移**：配置从 `~/.kimi/` 移到 `~/.kimi-code/config.toml`，默认模型变为 `kimi-code/k3-256k`；
  `pactify doctor` 仍检查 `~/.kimi/`。`-p` 仍自动批准、`-m` 仍可用；新增 `--output-format stream-json`（含最终回复与会话 id，无模型与 token）。

**补充（同日）**：kimi 本地会话记录证实——协议内切到 `kimi-for-coding` 的会话实际跑 `kimi-for-coding`，未切换的跑默认 `k3-256k`；
plan 模式那次循环在记录里是 64 次模型调用。测试期间 kimi 已自行下载 2.2.0 待安装，CLI 版本漂移是常态（D7）。

**仍待验证**：callee 最小配置启动的确切参数（codex `config`、claude `--strict-mcp-config`）；Claude SDK 桥读出 init 消息里的模型；
Q1（brief 交接质量）与 Q2（host 开销）需要真实任务，属于 Phase 0 本身。

## 11. 兼容适配层（D7 · 产品侧的日常维护）

各家 CLI 会自动更新（实测期间 kimi 已自行下载 2.2.0），模型表、配置位置、参数、协议字段都会变。
2026-10-15 一次批量更新后，5 个接入里有 3 处失配（agy 模型表、kimi 配置目录、codex 协议版本）。适配因此是产品能力，不是偶发修补：

| 组成 | 做什么 | 首次落地 |
|---|---|---|
| **版本记录** | 每个 job 记录 callee 的 CLI 版本与协议版本；结果信封里显示 | M0 |
| **动态模型表** | 不写死模型名：codex 用 `model/list`，ACP 用 `session/new` 的 `configOptions`，agy 用 `agy models`；用户指定的模型不在表内时拒绝执行并列出可选项 | M1 |
| **`pactify sub doctor` 自检** | 把 §10 的探测做成命令：安装与登录、设模型并核实、只读被拦、可写成功、拿到最终回复与 token。只用最便宜的模型档、一次性临时仓库 | M1 |
| **已验证版本表** | 每个 harness 记录「自检通过的版本」；运行时发现新版本 → 先自检（或提示用户跑），通过后写入表 | M1 |
| **协议 schema 漂移 CI** | 沿用现有 `codex-schema.yml` 的做法，扩展到 ACP schema；只比对协议，不需要登录 | M3 |
| **会话不留痕** | 每个 harness 必须声明「委派会话怎么从用户自己的会话列表里隐藏」，并在新 harness 接入时由测试强制（`TestEveryHarnessHidesItsSessions`）。codex：归档线程；kimi：默认写在独立配置目录里，天然隐藏（AJ-643）；`--with-user-config` 时调用 kimi 自己的归档接口（临时本机 `kimi web` 服务，CLI / web / Remote Control 一致），失败时退回会话文件的归档字段，并用官方 CLI 核对；workspace 由 kimi 从会话派生，不另行删除。厂商更新改了格式时以警告暴露 | M0（2026-10-15，Owner 要求覆盖所有接入的 coding agent） |
| **callee 最小配置启动** | 默认不继承用户的 MCP 服务器、插件、skills、记忆，压低固定开销并避免子代理拿到用户私有工具；`--with-user-config` 恢复全部配置，job 记录 `configMode`。codex：进程级 `-c` 覆盖项，加上经 `config/read` 逐个禁用 MCP（空 `mcp_servers` 表只会和用户配置合并、清不掉），实测输入 token 20,950 → 13,658（−35%）；kimi：见下一行；opencode 1.19.0：两个 XDG 镜像隔离，再加 `OPENCODE_DISABLE_CLAUDE_CODE=1`、`OPENCODE_DISABLE_EXTERNAL_SKILLS=1`、`OPENCODE_PURE=1`，空任务总 token 实测 18,930 → 7,690（AJ-652） | M1（AJ-636，2026-10-15 提前做） |
| **独立配置目录（harness home）** | pactify 为委派管理一个 harness 专用的配置目录（`$PACTIFY_HOME/harness-homes/<harness>`，所有 job 共用以便续接）。登录凭据与必要配置用**符号链接**指向用户原文件，不复制密钥，也不带用户的 MCP 等集成；委派会话写在这个目录里，天然不进用户的会话列表，不必再走各家的归档接口。job 结束后检查链接，被 harness 以「写临时文件再改名」替换成普通文件时，报警并删除副本。kimi：`KIMI_CODE_HOME`，链接 `config.toml`、`credentials/`、`device_id`，实测 MCP 不加载、用户会话索引不变、原文件不改（AJ-642/AJ-643）。agy：没有可以改数据目录的环境变量（数据目录由 `HOME` 推出），删除索引行或移走会话文件都会被 agy 从 protobuf 缓存重建，因此采用**镜像 HOME**（AJ-648，Owner 2026-10-15 同意）：受管目录里，真实 HOME 的每个顶层项都用符号链接映射进去，只有 `.gemini` 换成受管目录，并且只链接 `antigravity-cli/antigravity-oauth-token`；以 `HOME=<镜像>` 运行。实测登录有效、用户会话索引不变、git 与 Go 等工具经链接使用同一份文件、用户插件不加载。代价：子进程的 `$HOME` 改变，工具新建的顶层 dotfile 落在镜像里；每次启动前刷新镜像；删除操作绝不能穿过符号链接。**实现上是同一个公共组件**：受管目录、一组链接、启动前刷新、结束后检查副本、doctor 检查；各 harness 只声明自己要链接哪些文件和环境变量（AJ-649 从 kimi 的实现中抽取）。opencode 1.19.0（AJ-652）：同一组件支持两个镜像，`XDG_DATA_HOME=…/opencode/share` 镜像原 XDG 数据目录（默认 `~/.local/share`）、`XDG_CONFIG_HOME=…/opencode/config` 镜像原 XDG 配置目录（默认 `~/.config`），都排除 `opencode`，只显式链接数据目录的 `opencode/auth.json`；其他 XDG 项保留链接供 git 等工具使用。用户会话列表和原 auth/db 不变，受管数据库保持续接；`--with-user-config` 不隔离并警告会话可见 | kimi：M1（AJ-643）；agy：M2（AJ-649）；opencode：M2（AJ-652） |

**接入新 harness 时的约定**（M2 起，agy、opencode、claude-code 依次适用）：先实测独立配置目录能否同时做到三件事——保留登录、关闭用户集成、委派会话不进用户列表。三项都做到就优先采用它来满足「会话不留痕」和「最小配置启动」；做不到的（例如凭据在 macOS 钥匙串里、与配置目录绑定），再退回该 harness 自己的协议内覆盖项加归档，并把原因写进本表。实测结论与所用环境变量记入已验证版本表对应的 harness 条目。

CI 里无法登录各家账号，所以「真跑一遍」的自检只在用户本机执行；CI 只守协议层面的漂移。

For agy 1.4.0, an explicit effort must match the model's tier suffix. A live
`--model gemini-3.8-flash-low --effort high` probe returned
`--model gemini-3.8-flash-low conflicts with --effort=high` before inference.
Use the matching tier or omit effort; pactify preserves the requested model.
