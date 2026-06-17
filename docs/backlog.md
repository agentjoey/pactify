# Pactify Backlog

产品候选功能（未排期）。源头标注。

## 来自竞品分析（AgentHub，2026-06-13）
- **动作模板 / orchestrate 配方**：把常见多 agent 工作流做成一键配方（"加测试""共同评审""从需求到设计计划"），架在 orchestrate 之上——把门槛从"手写任务图 + assign"降到"选配方"。竞品 AgentHub 已有同款。
- **自然语言命令面板**：dashboard 里一个"输入指令 → 驱动 agent"的对话入口 + "等待你的"人在环状态，作为人→编排者的交互面（当前 orchestrate 仅 CLI 驱动）。

## 差异化备忘（对 AgentHub）
- AgentHub 靠中心化 "Hub"（服务器）协调，更新日志在反复修"Hub 连接不稳定"。Pactify 用 git+文件当唯一事实源、零服务器——**主打"没有 Hub 可以掉线、你的 repo 就是协调层"**，是 ADR-001"守 Team"之外更底层的技术护城河。

## 差异化备忘（对 Mind Agency，2026-06-14 调研）
[Toufumind/mind-agency](https://github.com/Toufumind/mind-agency)：「From Agent to Agency — 一个 AI 做不了的事，一群 AI 可以」，Multi-Agent Collaboration Platform，Apache-2.0，v0.8 beta（2026-06-05 建，活跃）。**同品类/同口号/同时代/同中文圈**，是定位上的 peer/竞品，但**架构相反、非技术冲突**：
- **它 = 厚客户端 App**（Electron + Next.js 前端 + Node WebSocket 后端，端口 3000/3001），内置自己的 agent 运行时（Claude Agent SDK 跑 Alice/Bob/Charlie 角色人格），群聊@/邮件/投票/多轮辩论/对抗评审 + 内存 EventBus + 本地 filesystem（Agents/、Groups/、.audit/、.mind/）信号。**单机、需常驻后端进程（本质就是个本地 Hub）**。
- **Pactify = 协议 + 薄 CLI**（Go 单二进制），git+`.pact/` 文件当唯一事实源、**零服务器**；不自带 agent，而是**协调你已装的真实 agent CLI**（claude-code/opencode/codex/kimi/cursor…），headless orchestrate 驱动；**跨机/跨 repo 走 git**，审计在 git 历史。
- **护城河对照**：Mind Agency 是"做一个能跑 agent 社会的桌面工作台"（GUI-first、共识/辩论功能更厚）；Pactify 是"做 agent 之间的协调协议"（infra-first、骑现有厂商 CLI、no-Hub、git-native、多机）。**可能互补**而非互斥（Pactify 协调跨机/跨仓，Mind Agency 是单机工作台）。
- **要盯的点**：① 命名/口号撞车（都打"agency / 一个 AI 不行一群行"，都中文起家）→ 营销同渠道会被直接比较；Pactify 要强化"协议非平台 / no-Hub / 骑你的真 agent / git 多机"的差异话术。② 它的共识投票/多轮辩论/对抗评审是 Pactify 没有的 agent-society 功能 → 是要不要追的取舍（Pactify 的"评审"是两条硬规则的协议约束，不是辩论）。

## 链路完善候选（2026-06-13 链路走查）
按用户旅程：①装+扫描注册 ✓ → ②建项目/配座席 → ③定义活 → ④orchestrate 跑 ✓ → ⑤看进展/介入 → ⑥交付远端
- **[#1] 项目设置向导（座席绑定）**：扫描注册完 agent 后，引导"用我注册的 agent 配这个项目"（选 agent→派座席+角色），接通"注册完"到"能干活"。是 #2/#3 的前置。
- **[#2] 目标→任务图自动规划（最大摩擦杀手）**：planner agent 把一句话目标拆成 pact 任务图（owner/reviewer/deps/verify），人 review/微调→orchestrate 跑。让 Pactify 从"专家能用"变"说人话就能用"。
- **[#3] 实时编排视图 + 升级 UX**：dashboard 看见正在发生的自主运行（谁在干/哪个 task/进度/token），一等的升级面（暂停→为什么→看 diff→续跑/改/接手）。把黑箱变可信的可见过程。
- **[#4] 人审门（可选开关）**：全自动 ←→ 监督式光谱，合并前/每棒停下等人点头（orchestrate 已有 --dry-run，加 --review-gate）。
- **[#5] 收尾交付步**：orchestrate 合并到本地 main 后集成"开 PR/推送"，链路终点是"交付到远端"。
- 次级：成本/可观测（每轮 token+花费、运行历史审计）、并行编排（worktree 隔离破 F1 单树）、作用域权限（--dangerously-skip-permissions → per-run allowedTools）。

**下一阶段已启动：#2 自动规划 + #3 实时编排视图（#1 作前置）。UI 开发拟交 antigravity。**

## 来自 #2/#3 brainstorm（2026-06-13）
- **scan/registry 标记 headless 可驱动性**：agent 扫描/注册时标出哪些 agent 能被 orchestrate 自动驱动（有 headless runner：opencode/claude-code/gemini-cli）vs 不能（GUI：antigravity/*-desktop——需人工或经 escalation 交接）。用户体验上明确"哪些能无人跑、哪些要你上手"。底层信号 = RunnerSpec ok。
- **headless runner 权限姿态**：claude 需 --dangerously-skip-permissions、gemini 需 --approval-mode yolo --skip-trust 才能自主开发（实测）。当前硬编码进 kind runner；候选改 per-run 可配权限姿态（allowedTools / approval-mode）。
- **antigravity 2.1.4 = 纯 GUI 无 headless 入口**（实测 double-confirmed）；自主编排的第三异构 agent 用 gemini-cli 替代。

## 来自 #2 planner brainstorm（2026-06-13）
- **planner review 在 UI 里做 + 配置默认开关**：`pactify plan` 的人审默认 ON（生成后停）、`--auto` 跳过；后续支持在 UI 里 review/编辑生成的任务图，并把"是否要人审"做成可配置默认（CLI 已有 --auto，UI 加开关）。
- **导入 planner 文件**：支持导入外部 planner 生成的任务图 manifest。
- **plan apply 事务化**：apply 中途某 assign 失败时回滚已 assign 的（当前非原子，需人工清理）。

## 来自 planner 真跑（2026-06-13）
- **orchestrate 空闲超时（比总超时更精）**：本次 opencode/deepseek-v4-pro 在 p-manifest 写完代码+测试过后【挂死】（进程 sleeping、0.7% CPU、25min 无输出、不 checkpoint），无限阻塞驱动器。已加 `--run-timeout`（总运行超时，默认 30min）兜底防无限阻塞；更优 = 空闲超时（无输出 N min 即杀），需在 osExec 包输出活动检测。
- **opencode/deepseek-v4-pro 慢+偶挂观察**：简单 manifest 棒耗时 ~25min 且 checkpoint 前挂死一次。记录为模型/runtime 稳定性观察；per-agent 模型配置（backlog）后可换标准棒模型。

## 错误处理设计（worker 挂死/部分完成的恢复）—— 需专门设计（2026-06-13）
**问题**：worker 卡住时，"orchestrator 接手完成"是错的一般模式——若 worker 只干了 5% 就挂，后面主要工作都由 orchestrator 做，自主性破产（orchestrator 变 worker）。本次手动抢救 p-manifest 只因它恰好 100% 完成（活+测试都过），是一次性补救，不可推广。
**正确方向**：挂死/失败 → 杀掉 + **重试 worker**（worker 重读状态+树里半成品，自己续/重做、自己 checkpoint），worker 始终是干活的人；反复失败到上限 → 升级给人。`--run-timeout` 已走这条路（超时→软失败→重派 owner=worker，非 orchestrator 接手）。
**待设计点**：
- 半成品续接：重试 worker 从未提交半成品续（省力但可能被半残状态搞晕）vs 先 reset 树干净从头（干净但浪费）——怎么选/怎么提示 worker。
- 何时放弃重试转人工：每次全量重试重做+烧 token，代价高，阈值/成本上限怎么定。
- 分类恢复："活干完只是 checkpoint 挂" vs "活没干完挂了"——能否更聪明地分类（前者只补 checkpoint，后者续/重做）。
- 空闲超时（无输出 N min）vs 总超时：空闲更快兜挂死、又不误杀合法慢任务（需 osExec 包输出活动检测）。

## 来自 liveview 三 agent 自主建（2026-06-13）
- **agent 任务 session 清理** ✅ 已做（2026-06-15，commit 55b829a，opencode-first）：task accept 后自动关闭该棒 owner+reviewer 的 session。**实测各 agent session CLI 后定 opencode 优先**：只有 opencode 有干净的 `session list` + `session delete <id>`（且最该清——常驻 daemon、重 session DB）；gemini 按易变 index 删、codex 只 archive、kimi/claude 无 headless 删除。机制：runner 给 opencode run 打 `--title pact:<seat>` → accept 后 `CleanupByTitle` list→匹配 title→按 id 删；CLI 默认开、`--keep-sessions` 关。已对真 opencode CLI 端到端验证。
  - **遗留**：① gemini（按 index 删，需 list→解析 index）/ codex（archive）/ kimi/claude（无 CLI，文件级）后续接；② 触发点目前是 accept 后（设计点「worker checkpoint 自清 vs orchestrate accept 后清」选了后者——orchestrate 集中清、worker 无需关心）；③ 保留 vs 清理：默认清，留 `--keep-sessions` 给要审计/debug 的场景。
- **--run-timeout 15min 太短/钝超时误杀慢任务**：本轮 step3（前端面板）合法慢，15min 超时杀了一次→重试续建才完成。默认 30min 较合理；正解 = 空闲超时（无输出 N min 才杀，不误伤慢任务），已记。本轮也验证了"超时→重试→读半成品续建"恢复链可用。
- **（待查）merge 后 STATE.yml 可能滞后**：liveview merge 后工作树 STATE 一度显 shipped、但 HEAD 提交的 STATE.yml 显 in_progress——疑似 pact Merge 的 STATE 提交时序问题，需查（feature 实际已 merged、代码在 main、测试绿，仅 STATE 文件可能滞后）。

## orchestrate merge 分支不匹配 ✅ 已修（2026-06-16，commit c3235f0）
**现象**：`pactify assign <task> --feature F --branch feat-X` 后跑**串行** `orchestrate --feature F`，两 task 都 worked→accepted，但 `pact merge F` 失败 `exit status 1`：`Please specify which branch you want to merge with`，且把工作树 `git checkout` 到了 base（main），人需手动 `git checkout` 回去。
**根因**：串行单树 orchestrate 的 worker **就地在 orchestrate 启动分支上干活+提交**（join 没真的切到 feat-X），但 `pact.Merge` 仍按 STATE 里记录的 feature 分支 `feat-X` 去 `git merge feat-X`——该分支从未被创建 → git 报错。merge 的 `git checkout base` 已执行、merge 本身失败 → 树停在 base。
**影响**：feature 卡在 accepted-but-not-shipped；代码其实已就地提交在启动分支（本次 = feat-audit-layer，`git checkout feat-audit-layer` 即恢复，无丢失）。
**正解候选**：① 串行 orchestrate 检测"feature 提交已在当前分支"→ merge 变 no-op（只置 shipped + 提交 merge 事件）；② 或 assign 时 `--branch` 与串行运行分支一致性校验/忽略；③ 文档明确：串行就地跑别设异名 feature 分支。关联并行 merge 逻辑（worktree park base）。

## gemini-cli 免费档静默降级 flash ✅ 已修（2026-06-16）：runner 在 gemini 座席注入 Keychain `pactify/gemini` 的 `GEMINI_API_KEY`（切 API-key 档，绕开 oauth 免费档的 FLASH_FALLBACK，-m pin 才 hold）；无 key 时 no-op。用户设 `security add-generic-password -s pactify -a gemini -w <key>` 即生效。可选增强：`--output-format json` 解析 stats.models 在实际≠pin 时告警（仍未做）。原记录：
**现象**：liveview step2 由 gemini 跑时，用户在 live 里看到模型是 `gemini-3-flash-preview`，而非 runner pin 的 `gemini-3.1-pro-preview`。
**根因（已查 gemini-cli 0.46.0 bundle 证实）**：不是我们 runner 的 bug——隔离短跑 `gemini -p ... -m gemini-3.1-pro-preview --output-format json` 实测 `stats.models` 就是 `gemini-3.1-pro-preview`，`-m` pin 生效。问题是 gemini CLI 内置 `FLASH_FALLBACK`/`fallbackModelHandler`，**仅在 `authType === "oauth-personal"`（免费 Google 登录档）或 `compute-default-credentials` 下挂载**；当免费档 pro/preview 撞配额(429/quota)时**静默降级到 `gemini-3-flash-preview`**。当前 `~/.gemini/settings.json` 正是 `oauth-personal`，长任务（liveview 多轮、上万 token）把免费 pro 配额跑光后被自动降级。
**修复方向（需用户决策，涉及成本/密钥）**：
- **换 API key 档**：用 `GEMINI_API_KEY`（AI Studio）或 Vertex 认证，付费档不触发静默 fallback，`-m` pin 才真正 hold。密钥按规约存 macOS Keychain，禁止明文写 settings.json/shell rc；runner 启动 gemini 时从 Keychain 取出注入子进程 env。
- 或接受降级但**让它可见**：用 `--output-format json` 解析 `stats.models`，发现实际模型 ≠ pin 时在 orchestrate 状态/日志里告警（至少不静默）。
- 关联 [per-agent 模型配置]：模型 pin 落地后，gemini 棒的认证方式应一并配置化。

## 来自 greet live demo 真跑（2026-06-13）
- **`join` 冷启动被未来任务的 dep 误杀（真 bug）✅ 已修（2026-06-16，commit c3235f0：checkJoinGate 只在「所有可开工任务都被 dep 卡」时拦，可运行任务不再被未来 gated 任务拖累）**：worker `pactify join <seat>` 冷启动会校验该座席**全部** assigned 任务，若某个**未来**任务（如 t2）合法 blocked by unaccepted dep（t1 还没验收），join 直接硬失败 exit 1，且在 checkout feature 分支**之前**就挂——导致 feat 分支没被创建。worker 本轮手动 `git checkout -b feat-greet` 绕过。正解：join 冷启动只需保证「我即将要干的可运行任务」能开工，不该因未来 gated 任务而失败；dep 门控应在该任务**真正开工时**校验，而非 join 时全量校验。
- **live demo 全链路验证 ✓**：claude 单 binary 扮 orchestrator+reviewer+worker(opencode 座席)，per-task 翻转满足 owner≠reviewer，reviewer 每棒独立重跑验收命令；t1→t2→t3（带 deps 链）全自动 worked→reviewed→accepted→merged→shipped（iter=8），CLI 实测可用。status.json 实时刷新驱动 live 面板四态流转正常。

## ✅ 已实现（2026-06-14，8h 自主交付）—— 详见 docs/roadmap-next.md
本 backlog 中以下条目已 shipped 到 main + 推 origin：
- #1 项目设置向导（CLI + endpoint；UI 待做）、#3 并行编排、#4 作用域权限、#5 收尾交付步、#6 session 清理、#7 planner review UI、#8 drivability 标记、#9 权限姿态、#10 per-agent 模型、#11 配方、#12 UI polish（部分）、#5 错误处理（idle-timeout）。
**仍未实现的（汇总见 `docs/roadmap-next.md` A 部分）**：#4 人审门/review gate、自然语言命令面板、gemini 降级修复、join 冷启动 bug、错误处理完整设计、成本/可观测、plan apply 事务化、post-merge STATE 滞后、make install 自检、各后端 feature 的 UI（见 roadmap-next.md B 部分）。
**新 agent 接入候选**：见 `docs/agent-integration-candidates.md`（13 agent 联网调研，Codex/Goose/Kimi/Cursor/Amp 一等可接）。

## 新 agent 接入候选（2026-06-14 联网调研，详见 docs/agent-integration-candidates.md）
按"接入价值 × headless 可行性"排（接 runner 前必须装上跑 `--help` 实测，禁止凭文档断言）：
- **一等（drivable runner，照搬现有 pattern）**：
  - **codex-cli**（kind 已在、只差 runner）：`codex exec "{briefing}" --sandbox workspace-write`，MCP TOML `[mcp_servers.*]`，binary `codex`。**最低成本增量**。
  - **goose**（Block）：`GOOSE_MODE=auto goose run -t "{briefing}" --no-session`，MCP-native（extensions），`~/.config/goose/config.yaml`，binary `goose`。
  - **kimi-cli**（MoonshotAI/kimi-code）：`kimi -p "{briefing}"`（默认 auto），entry `AGENTS.md`，MCP `.kimi-code/mcp.json` JSON `mcpServers`，binary `kimi`。与 Claude Code 几乎同构。
  - **cursor-cli**：`cursor-agent -p --force "{briefing}"`，entry `AGENTS.md`/`.cursor/rules`，MCP `.cursor/mcp.json`，binary `cursor-agent`。
  - **amp**（Sourcegraph）：`amp -x "{briefing}"` + `AMP_API_KEY`，entry `AGENTS.md`，MCP `amp.mcpServers`（`.amp/settings.json`），binary `amp`。
- **二等（小适配）**：continue（`cn -p --auto`，binary `cn`）、cline（`cline -y --json`）、crush（`crush run --yolo`，flag 位置待测）、devin-cli（原 Windsurf，`devin -p --permission-mode bypass`，JSON 输出待测）、aider（`aider --message --yes`，**无 MCP** 走 entry `CONVENTIONS.md` + shell fallback）。
- **三等（需 adapter）**：hermes（Nous，`hermes -z --yolo`，MCP 用 **YAML `mcp_servers` 全局** 需新 Format+Global 适配，entry 不明）、q-developer（AWS，`q chat --no-interactive --trust-all-tools`，**trust 有已知 bug 约 50% 生效需重试包裹**）。
- **仅座席/人工交接**：zed（内置 agent 无 headless，但可挂 pact context server）、各 desktop/GUI kind。
- **不接**：openclaw（self-hosted 消息网关/个人助手，非仓库内 coding 座席）。
- **协议层扩展方向**：若 Pactify 自身说 ACP（Agent Client Protocol），Zed/JetBrains/Kimi 可 host Pactify 驱动的 agent——单列调研。
- **配套**：每个新 kind 接入后，`agent config`（model/权限姿态）即时可用；Codex 的 `--sandbox` 分级提示 PermPosture 未来可加"sandbox 级别"维度。

## 自定义 agent 接入标准 API（2026-06-16 用户，待具体讨论）
- **设计一套标准 API/契约，让用户自定义 agent 可接入**：当前接入新 agent 需在 `internal/agent` 硬编码 spec + RunnerProfile（kind/entry/MCP format/runner argv/权限姿态/候选模型）。候选 = 把这套抽象成**用户可声明的契约**（如一个 `agent manifest` 文件：binary、headless argv 模板、entry 文件、MCP 配置形态、权限 flag、候选模型），让用户无需改 Go 源码即可接入自有/小众 agent。关联：现有 RunnerProfile/spec/CandidateModels、agent-integration-candidates.md（13 agent 调研）、ACP 协议方向。回头细化范围与 schema 再开 spec。

## 剩余 UI（2026-06-14 暂停，待后续）—— 详见 docs/roadmap-next.md + ui-design-spec.md
已做：Setup(#1)、Recipes(#11)、Plan 只读(#7)、Live 并行聚合(#3)、Agent Config(#10/#9/#4)、Ops polish(#12 部分)。
**剩余（都是有副作用 HTTP 操作，需先设计确认/安全 UX）**：
- **B3 Plan apply**：Plan 视图内编辑 owner/reviewer/deps + Apply（`POST .../plan/{feature}/apply`，含事务化）+ Run。
- **B5 Run + 自然语言命令面板**：dashboard 启动 orchestrate（选 feature/并发）+ 一句话→拆解→预览→跑（`POST /api/projects/{id}/orchestrate`，需沙箱/权限设计）。
- **B6 Review Gate / 升级 UX**：Live 里暂停态显「为什么停/看 diff/批准/打回/接手/续跑」（resume endpoint）。
- **B7 Ship 按钮**：shipped 后 push/开 PR（`POST /api/projects/{id}/finish`，加确认弹窗）。
- **B8 Sessions prune**：Ops 内每 agent prune 按钮（`sessions prune`，加确认）。
- **setup/apply 一键**：Setup 视图「Apply」直接 init+wire（`POST /api/setup/apply`，mutate .pact 需确认）。
- **横切设计**：有副作用 HTTP 操作的**确认弹窗 + acting-seat 校验 + 安全/沙箱** 规范，先定再实现。
- **[UI] agent logo 统一接入（用于 agent 卡片）**：收集各 agent 品牌 logo（claude-code/opencode/gemini/codex/cursor/kimi/amp/goose/aider/…），**统一风格处理**（同一描边/留白/圆角画框、duotone 或单色化以适配浅色主题、统一尺寸），做成 `kind → logo` 资产/组件，替换 agent 卡片现用的 Phosphor 概念图标（`kind-*`）。来源：2026-06-14 用户。关联元素库 Icon library。✅ 已做（2026-06-15，commit 8b467a5）：AgentLogo 组件已接进 Setup / Agents 引导条 / Ops AgentConfig 真卡片。

- **[UI] Settings 视图 agent 管理三件套**（来源：2026-06-15 用户）✅ 已做（2026-06-15，commit af9fe8b）：
  1. ✅ **一键自动扫描**：Ops/Settings 新增 `AgentRoster` 面板，Scan 按钮重拉实时探测（`GET /api/agents` 每次 `agent.Scan()` 重探），一键 Register 已装未注册的 agent。
  2. ✅ **手动添加（已知 kind）**：「Add manually」展开列"支持但未检测到"的 kind，Register anyway 登记。**自定义 binary 路径延后** → 后端 register 端点暂不收 path。
  3. ✅ **模型下拉**：新增 `agent.CandidateModels(kind)`（每 RunnerProfile 一份候选），经 config DTO 的 `candidate_models` 暴露；model 字段升级为下拉（default · 候选 · custom…），无候选回退纯文本（codex-cli 不 pin 默认→纯文本）。
  - **遗留后端 backlog**：① register 收自定义 binary 路径（启用真·自定义手动添加 + scan 探测自定义安装）；② 候选模型清单做大/可配（当前 opencode/kimi 各仅默认 1 项）。

## audit 层 opencode 捕获（JS 插件）✅ 已做（2026-06-16，实测 + 端到端验证）
**实测结论**：opencode 无命令式 PreToolUse hook，工具拦截唯一入口是 **JS 插件**（`@opencode-ai/plugin` 的 `tool.execute.before(input:{tool,sessionID}, output:{args})`），自动从 `.opencode/plugin/*.ts` 加载。
**已做**：`pactify audit install --opencode` 写 `.opencode/plugin/pact-audit.ts`——其 `tool.execute.before` 把 opencode 工具调用（实测 `tool="write" args.filePath` / bash `args.command`）翻成 claude-style JSON 喂 `pactify audit hook --kind opencode`，best-effort 不阻断工具。**真 opencode run 端到端验证**：write 调用被捕获进审计日志、归属正确、文件照写。Detect 按插件文件在否报告；Uninstall 删文件。
**遗留**：opencode 的 MCP 工具名映射（当前只接 bash/write/edit/read/patch）；codex hook 形态待查。

## CI: 升级 GitHub Actions 到 Node 24（2026-06-16 发布时告警）
v0.4.0 发布与 CI run 均报：`actions/checkout@v4`、`actions/setup-node@v4`、`actions/setup-go@v5`、`goreleaser/goreleaser-action@v6` 跑在 **Node.js 20**，GitHub 自 2026-06-16 起强制 Node 24、9-16 移除 Node 20。非阻断，但需升级这些 action 到支持 Node 24 的版本（或暂时 `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true`）。改 `.github/workflows/ci.yml` + `release.yml`。

## 多 agent 真跑（2026-06-17，manifest C+D）发现
- **kimi-cli headless 需全 key 模型 id** ✅ 已修（commit e459c51）：runner pin `kimi-for-coding`（短名）→ kimi 报 `LLM not set`；正解是配置全 key `kimi-code/kimi-for-coding`（=K2.7 Code）。教训：接 CLI runner 的模型 id 要用该 CLI 配置里的**完整 key**，别用底层短名。kimi 交互登录不影响 headless——问题是模型 id。
- **serve 路由命名要避让既有 `{kind}` 通配（Go 1.22 ServeMux）** ✅ 已修（C 改 `/api/manifests`）：`DELETE /api/agents/manifests/{kind}` 与既有 `DELETE /api/agents/{kind}/register` 在同段歧义 → 启动 panic。新 endpoint 别塞进已有 `{kind}` 通配的命名空间。
- **D2 进度感知巡检现场验证 ✓**：kimi 停滞 5min（无输出+无落盘）被 patrol 杀+重试，最终收敛。

## ~~token 捕获未接进 orchestrate runner（D1 遗留「最后一块」）~~ ✅ DONE 2026-06-17
**已完成**：写入侧接通——`CmdRunner.Run` 经 execFn 的 `capture io.Writer` 旁路（`tailWriter`，1 MiB tail 上限，不改 streaming/idle/session-title）siphon agent stdout → `tokens.Parse(kind,output)` → `recordTokens` 写 `.pact/orchestrate/tokens.json`（keyed by task；空 task 或无 usage 静默 no-op；失败 run 仍记已产生用量）。无锁安全：串行循环一次一棒，并行特性各自独立 worktree（不同 RepoDir）。读取侧（stats.go → RightRail `⛁` + Cost 镜头）原已接好，闭环打通。
**已知边界（可接受）**：① 未捕获的现有 kind 输出若不带 usage JSON 则无数据（best-effort 遥测）；② claude 单体 pretty-JSON 若 >1 MiB 会被 tail 截断导致整体解析失败（极少见，token 暂缺一棒，不影响 run）；③ 并行 worktree 的 tokens.json 随 worktree 清理而丢——并行成本视图待后续统一回主 repo。
