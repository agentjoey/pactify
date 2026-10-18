# Pactify — Architecture

> Last updated: **2026-10-18** | Status: **v0.12.0 已发布**；M1 已合入但未发布，v0.13.0 挂起、继续内部自用。历史与未发布变更见 [`CHANGELOG.md`](CHANGELOG.md)。
>
> 产品主线为 harness 路由器，当前支持 codex（默认）及实验性的 kimi、agy、opencode，见 [`specs/harness-subagents.md`](specs/harness-subagents.md)。下文保留的编排架构已冻结；单一规范账本暂停在 WS-B（flag 默认关）。
> AJ-710：opencode 委派改用每 job 独立的 authenticated serve（v1 HTTP/SSE），会话汇总用量、持久化数据库续接和进程树清理；详见 [harness-subagents.md](specs/harness-subagents.md)。
> 云端 Go、web 与共享 workspace 及其构建 / 发布 CI 已删除（AJ-664）；gemini-cli 已移除（#106、#107）。

## Overview

Pactify 是 **harness 路由器**，在 Claude Code 中通过 slash 命令委派大任务；`pactify sub` 管理 job、结果与隔离工作区，`/jobs` 与 `pactify sub panel` 提供任务查看。pact 协议与账本保留为可选治理模式，冻结新投入；本地看板现服务 harness jobs，旧 serve 治理 API 已由 AJ-723 删除。

## 制品层（用户 repo）

```
.pact/
  PROJECT.md      ← 章程（目标/技术栈/角色/约定）
  STATE.yml       ← 结构化活状态（log.jsonl 的投影）
  tasks/<id>.md   ← 单任务 spec+plan+验收项+交接日志
  log.jsonl       ← append-only 事件流（事实源 + 通讯总线）
CLAUDE.md / AGENTS.md / GEMINI.md  ← 各厂商入口，均 → .pact/
docs/specs|plans|decisions/        ← 知识库
```

## 通讯架构

已删除（2026-10，见 [CHANGELOG](CHANGELOG.md#unreleased)）。

### M3.4 relay 接口

已删除（2026-10，见 [CHANGELOG](CHANGELOG.md#unreleased)）。

### 事件 schema（草案，M1.1 定稿）

```jsonc
{
  "ts": "2026-06-09T09:00:00Z",
  "agent_id": "claude-opus",
  "role": "orchestrator",        // orchestrator | worker | reviewer | human
  "event_type": "checkpoint",    // join | assign | checkpoint | accept | changes_requested | merge | ...
  "task_id": "T1",
  "feature": "BL-042",
  "payload": { /* event-specific，evidence/diff-ref/reason 等 */ }
}
```

设计约束：event_type 枚举要同时覆盖 shell CLI 命令语义和 MCP 工具语义；payload 自由扩展但顶层字段冻结。

## 核心设计决策

### log.jsonl 为源，STATE.yml 为投影
多 agent 并发写 STATE.yml → git 冲突；append-only log 天然 merge 友好。写=追加 event；读=读 STATE；`pactify log` 从 log replay 重建 STATE。

### 拉取式派发
异构 agent 无法互相 ping → worker 启动时读 STATE（pull）。人是"启动按钮"。推送式 daemon = Phase 4。

### 角色与职责分离
| 角色 | 职责 |
|---|---|
| orchestrator | 拆 spec→tasks；派发；合并；维护章程 |
| worker | 实现；checkpoint → awaiting_review + 写证据 |
| reviewer | 验证 diff+证据 → accepted / changes_requested |
| human | 启动按钮 + 最终权威 |

worker 不能自标 `accepted`（职责分离）。

### 驱动层（orchestrate driver — driver-modernization）
`orchestrate` 把座席作为子进程「一棒」驱动。传输可选:
- **CmdRunner**（默认）— headless 一次性 spawn（`claude -p`/`opencode run`…），解析 stdout；巡检 idle-timeout 兼看工作树变更(静默但在写不误杀)。
- **AcpRunner**（opt-in,`--transport kind=acp`）— 对 vendor CLI 说 **Agent Client Protocol**（`internal/acp`,JSON-RPC/stdio）:`initialize→session/new→session/prompt→session/update`。给结构化事件流:usage（token）经 `session/update` 落 `internal/tokens`（**根治 TOK=0**）、`session/request_permission` 三档策略 auto/escalate/deny（escalate 写 escalation,与 OpenHands 无脑 auto-approve 相反）。kind→命令映射:kimi=`kimi acp`、claude/codex 经 npx bridge。`RoutedLocalRunner` 按 kind 选传输,空映射=全 cmd(零行为变化)。
- **会话续接**（AcpRunner）— `.pact/orchestrate/sessions.json`（lockx）记 (seat,task)→sessionID;重试时 server 支持 `loadSession` 则续接会话（保上下文、省 token）,失败回退新会话;accept/终态清理。
- **知识注入** — briefing 尾部注入 `.pact/memory.md`(累积决策)+ role/keyword 匹配的 `.pact/skills/*.md`(4KB 预算);无文件时字节不变。
- **可观测** — 当前 dashboard 使用 sub job 用量与成本；旧 per-seat stats API 及其计算包已在 AJ-723 删除。`pactify doctor` 保留 per-vendor 预检(binary+auth+ACP 可用性)。

### 评审深化 + 运行时（review-runtime — 包2）
在「驱动一棒」外把评审与运行时做深(全部 opt-in,不带配置行为不变):
- **fix-until-green 自修环** — worker checkpoint 后、评审前先跑 verify 门;红则**同 worker fix 轮**(默认 2 轮,不计 MaxFails),绿才进评审;board 显示「修复中 n/2」。省 reviewer 回合。
- **quorum 多评审** — `assign --reviewers a,b,c --quorum N`;投影按「本次 checkpoint 后不同 reviewer 的 accept 数 ≥ N」判 accepted,任一 changes 清零重投;accept 门校验 caller∈reviewers。**单 reviewer 路径字节不变**;两不变量神圣(worker 仍不能自 accept)。
- **critic 预评 / QA 门**(顺序:gate 绿 → QA → critic → reviewer)— critic 出 `CRITIC_SCORE` 注入 reviewer(无门控权,只提示);QA(`qa:` 行)真跑软件出 `QA_RESULT`,FAIL 走 fix 轮(共享轮次预算);两者失败都宽松放行。note 复用 `start` 事件类型,不加新类型。
- **账本持久快照** — `.pact/state-snapshot.json`(offset+prefix-hash 校验,损坏静默全量 fold,`PACT_NO_SNAPSHOT=1` 逃生),冷启动增量 replay,**5k 账本 11.5x**;gitignored,非事实源。
- **动态座席 / 定时** — `join --kind` + driver 每轮实时读 seat→kind(mid-run join 下轮可驱动)+ planner 可提议新座席(apply 自动 join);`pactify schedule`(`daily@HH:MM`/`every:Nh`,serve ticker,复用 scheduler 的运行冲突门)。
- **已知 follow-up**:`pactify validate` 仍只从 `init` 事件推导座席 roster,`join` 动态加入的座席会被误判「不在 roster」——待专门修(改 golden 向量有风险,单列)。

## CLI 命令集（Phase 1）

```
pactify init           # 生成 .pact/ 骨架
pactify join           # worker 冷启动 + 追加 join 事件
pactify status         # 打印 STATE.yml 现状
pactify checkpoint <id># task → awaiting_review + 追加 log
pactify accept <id>    # reviewer 用；task → accepted + 追加 log
pactify merge <feat>   # 全任务 accepted → --no-ff 合并 + feature → shipped
pactify log            # replay log.jsonl → 重建 STATE（投影验证）
pactify validate       # 校验 .pact/ schema（防漂移）
pactify serve          # /api/sub + 静态 dashboard；另保留定时 scheduler
```

## 技术栈

| 层 | 选型 | 理由 |
|---|---|---|
| CLI | **Go** 单静态二进制 | 零 runtime 依赖，agent shell drop-in；启动 ~5ms |
| 本地 dashboard | Vite + React SPA，`go:embed` | 静态嵌入 binary，与云端共享 React 组件 |
| 云端 app（Team）| Next.js standalone | 与本地共享组件；**不绑 Vercel 专属特性**（可迁自托管）|
| 设计系统 | Tailwind + shadcn/ui | 两端通用 |
| 可视化 | React Flow（编排画布）+ 时间线 | Squad 编排画布 |
| 实时 | 本地 SSE/WS · 云端 Supabase Realtime | 本地零依赖，云端复用 Supabase |
| 后端（Phase 4）| Supabase | 可自托管，大规模商业化前可用 |

## Open-core 边界

**ADR-001（决策记录留在本地工作台，未随公开仓库发布）**：守 Team。所有付费价值落在云端 relay 之上；log.jsonl 事件 schema 同时服务本地（免费）和云端（付费），零改动 agent 端。

## Squad (Phase 3, M3.1+M3.2)

AJ-723 已删除旧 serve 治理 API 及项目 watcher。serve 现在只提供 `/api/sub`
（Jobs、Dashboard、Settings、Live，含 `jobs/{id}/events` SSE）和嵌入静态资源，保留 CSRF
Origin guard；未知 `/api/*` 返回 404。治理动词、项目注册、接线、座席和规划继续走 CLI/MCP。

`internal/serve/scheduler.go` 保留 `StartScheduler` 的 schedule → orchestrate 最小链路：
启动时的项目注册表、当前目录和 `--project` 发现、`--seat`（默认 `PACT_AGENT_ID`）座席、派生 roster kinds、串行运行冲突门、
子进程日志和回收。注册表变更需重启 serve。

协议引擎的 `pact.At(dir).As(seat)`、任务依赖校验仍保留；下面的 UI 里程碑是历史记录，
相关治理界面和 serve API 已退役，不代表当前 dashboard 的能力。

### Comms visualization (M3.3b) — Canvas 载体已移除（2026-07-07；pulseTargets 仍服务 Board 脉冲）

The canvas gains a comms lens + replay scrubber — pure visualization, **zero protocol
changes** (this milestone writes no events):

- **Waits overlay** (`web/src/lib/comms.ts`, client-side): wait edges and seat markers are
  derived from the existing `StateDTO` snapshot alone — no engine or new endpoint. Each
  status maps to an edge/marker (`awaiting_review`→owner→reviewer, `changes_requested`→
  reviewer→owner, unmet `deps`→task→dep, never-joined owner/reviewer→warning badge, an idle
  joined seat→dimmed) with a reason chip; tasks transitively blocked through dep edges get
  an amber outline (cycle-free graph reachability). The overlay is a canvas-toolbar toggle
  (default off, component-local) — derived edges merge into `deriveFlow` when on, never
  written back to the layout sidecar.
- **Replay**: removed with the 2026-07 view consolidation; its leftover serve endpoints
  (`GET .../timeline`, `state?at=N`) were deleted in v0.12.0.
- **Live pulse**: each SSE-applied snapshot (live mode only) diffs the changed task(s) and
  pulses their node + wait edges once in the actor's role color (the site's cable-pulse
  brand idiom). Pure CSS keyframe, fully gated off under `prefers-reduced-motion`.

### 视图收敛（2026-07-07，PR1/PR2）— dashboard 单视图化

三视图（Board/Canvas/Live）收敛为 **Board 单视图**。Canvas 整体删除（功能与 Dispatch
面板重复——与 v0.7.2 删 Plan mode 同一逻辑；@xyflow/react 依赖、画布工艺规约、office-zoom
flake、`squad/layout` 端点与 `.pact/squad/layout.json` sidecar 一并退役）。Live 的三件
承重件迁入 Board，能力零损失：

- **RunRail**（`web/src/components/board/RunRail.tsx`）：Board 顶部运行横幅——RunControl
  聚合条（phase/修复中 n:m/iter/进度 + Resume/Ship）+ 每个 driver 触达 feature 一条 lane
  （任务管道、可展开 AgentTerminal、gate 暂停时的**五动作 ReviewGate**）。driver 空闲或
  hosted 源（无 status 方法）时渲染为空——board 保持干净。
- **EventDrawer**（`board/EventDrawer.tsx`）：pact log 终端 = Board 底部折叠 ticker，
  展开为着色事件流 + 座席在场脚注。
- **RightRail 增 Output 段**：非终态任务内嵌 AgentTerminal（stream SSE, Last-Event-ID
  续传）——这也是 Cockpit V1 只读探针的落点（铺位工程）。

单视图 IA：View 类型/快捷键/Toolbar lens 段控/⌘K 视图项移除；`shots.mjs` 只拍 board；
当时的 Board 视觉门脚本已在 AJ-722 删除，当前截图使用 `shots.mjs`、`shots-live.mjs` 与 `shots-panel.mjs`。下方 Canvas P0 / comms visualization /
Dashboard v2 的 Canvas-Office 段保留为历史记录。

### Dashboard v2 (Linear-grade re-skin + Office mode)

The embedded dashboard was rebuilt to the locked design system (spec
`docs/superpowers/specs/2026-06-11-dashboard-v2-polish-design.md`, six validated mockup
boards in `docs/superpowers/mockups/dashboard-v2/`) — still **zero protocol changes**;
the only sidecar growth is an additive `office` key in the opaque layout JSON:

- **Token foundation** (`web/src/tokens.css`, Tailwind v4 `@theme static`): a **dark**
  4-layer background ramp (`#0a0e14` void → `#10151e` surface → `#171e2a` raised →
  `#0c1119` inset), role + semantic tokens, self-hosted Inter + JetBrains Mono. Because
  almost every component reads tokens via `var()`, swapping this `@theme` block migrates
  the whole app light↔dark with no component edits; legacy `--role-*` vars are re-pointed
  too. `ui/` primitives (Button/Modal/Popover/Badge/MetricStrip/…) are the sole control
  source. (Earlier light-theme mockup: B·Indigo; the shipped theme is dark.)
- **Ant colony avatars** (`web/src/components/ui/ants/`): one colony, eight castes
  (queen=orchestrator, guard=reviewer, builder=worker + qa/pm/designer/research/ops by
  roster-role keyword, fallback builder); seat individuality = deterministic pad-gradient
  hash. Used across seats, desks, task-card chips, and the TopBar acting-seat avatar.
- **Canvas dual mode — Office is the default landing**: Office renders one draggable
  desk per joined seat (doing/inbox/waiting-on zones from `lib/office.ts`, a pure
  derivation; status precedence busy>review_due>waiting>idle), wall-chart + shipped-tray
  furniture, draft-dock drag-to-desk dispatch, and pulses-driven carrier-ant parcel
  transit in viewport space. Plan mode keeps the feature/task frames with the ambient
  stage, zoom HUD, minimap, ant-crawl dep/wait edges (cap 6, reduced-motion → static),
  context menus, snap guides, and feature focus mode.
- **Shell**: searchable project switcher, segmented views with 1/2/3 keys, ⌘K command
  palette (cmdk; tasks/actions/navigation, observe-safe), `?` cheat sheet, observe badge,
  dynamic title/favicon, task detail slide-over (spec/evidence/session timeline/verbs),
  replay timeline with typed ticks + `?at=N` deep link, humanized engine errors on the
  toast rail, first-load skeletons, and an empty-registry onboarding hero.

### Canvas P0 (interaction-foundation rework, 2026-06-12, PR #20-#22) — 已随视图收敛移除（2026-07-07，历史记录）
- **Position materialization** (`web/src/lib/canvas.ts`): layout sidecar **v2** is the
  single source of truth for node positions — top-level nodes absolute, children
  parent-relative (RF-native coords, zero conversion on drag-save). `deriveGraph`
  produces identity/data/edges only; `placeNew` assigns a grid slot ONCE when a node
  first appears (idempotent, orphan entries excluded from occupancy but preserved as
  replay position memory); `mergeNodes` updates the RF node array by id, preserving
  RF-written position/measured/selected/dragging and returning identical references
  for unchanged nodes. Legacy v1 layouts (no `v` field) are dropped and re-materialized.
  A `layoutLoaded` gate prevents materializing against another project's (or an empty)
  layout during project switches. Desk positions follow the same model via the
  additive `office` key.
- **Connect UX**: Dify-style full-width 16px strip handles with center port marks
  (visible to authors, hidden for observers), v12-correct connection classes
  (`connectingfrom`/`valid`; the v11-era `.connecting` rules were dead), a stage-level
  `connecting` state lifted from `useConnection`, and a custom bezier connection line.
  All invalid-connection notices (committed target / cross-feature / cycle) surface
  via `onConnectEnd` — `isValidConnection` blocks `onConnect` for invalid drops, so
  notice branches there are unreachable defensive code.
- **Engineering rules** (spec §5): production code never fabricates RF geometry
  (`measured`/`handles` are SSR inputs + RF write-back fields); node arrays update
  only via merge-by-id; positions have exactly two writers — `placeNew` on first
  appearance and user drags.
- **Acceptance gate**: Playwright e2e (`web/e2e/`, chromium) against a hermetic mock
  server (real serve JSON shapes, SSE push hook, PUT capture). Seven regression specs
  pin the four user-reported failures (drag isolation, connect + two negative cases,
  office zoom, office authoring chain, drag-during-SSE stability). CI `e2e` job is a
  required merge gate alongside vitest for canvas PRs; jsdom is no longer the
  authority on interaction correctness.

### orchestrate 驱动器（autonomous loop, 2026-06-13, #9）
源自 dogfood 头号发现 #9：协议把协调【内容】放进文件，但协调【时机】仍需人触发，人退化成调度器。`pactify orchestrate` 在产品层兑现"消灭人肉中继"。
- **中心化串行驱动**（`internal/orchestrate`）：读 `.pact/log.jsonl` → `projection.Project` → `nextAction(纯函数)` → 在状态变迁 headless 拉起对应 agent → 重投影 → 循环，直到 feature shipped 或升级暂停。串行天然规避 F1 单工作树。
- **nextAction 纯决策**（decide.go）：优先级 RunReviewer(awaiting_review) > RunOwner(assigned/changes_requested/**in_progress** + deps 全 accepted) > Merge(feature 全 accepted) > Done。in_progress 是可派态——`pactify join` 会把座席所有 assigned task 一次翻成 in_progress，未 checkpoint 的须重派（顺带给崩溃重试）。阈值不在此纯函数里（churning task 总有 action），由 loop 的 `tripped()` 在派发前执行。
- **per-kind headless runner**（agent.Adapter.Runner）：opencode→`opencode run`、claude-code→`claude -p`；GUI/desktop kind 无 runner → fail-closed。Runner 接口化（生产 exec 与测试 fake 共用）。座席→kind 经 `--seat-kind` 映射（kind 未持久化进协议态）。
- **硬测试门**（gate.go）：merge 前 orchestrate 独立复跑 feature 的验收命令（task 规格 `verify:` 行，缺则全量 `go build && go test` 回退），不过不 merge——给 LLM 评审垫确定性安全网（LLM accept ≠ 可 merge）。
- **升级=暂停非终止**：返工/失败阈值或硬门失败 → 写 `.pact/orchestrate/escalation-*.md` + 通知 + 暂停；人修后重跑续行。agent 瞬时崩溃算软失败（重试至 MaxFails），不杀驱动；ctx 取消透传。
- **测试**：纯函数单测 + 注入 fake Runner/cmdExec 的端到端集成（happy/返工/升级/硬门拦截/崩溃软失败/dry-run），不用真 LLM。

### 8h 自主交付新增子系统（2026-06-14，12 功能）

**per-agent 配置体系（`internal/agentcfg` + `agent/launch.go`）**：把硬编码的 model pin / 权限 flag / drivability 参数化。`RunnerProfile` 用 per-kind builder 闭包渲染 argv（默认输出 = 历史硬编码 args，保持契约）；`PermPosture`（blanket 自动批准 ↔ scoped allowlist）支持作用域权限；`agentcfg.Resolve(kind)` 把机器注册表的 per-agent override（model/权限）叠加到内置默认上。orchestrate runner 改走 `agentcfg.Resolve`。CLI `agent config <kind> --model/--allowed-tools/--restricted`；`agent scan` 显示 drivable/manual。

**idle-timeout（`internal/orchestrate/runner_idle.go`）**：子进程 stdio tee 到 `idleTracker`，watchdog 在【无输出】N 分钟后杀进程（`errIdle` → 软失败 → 重试 worker）。比钝的总超时精准——既不过杀挂死、也不误杀合法慢任务。重试时 worker briefing 加"续接半成品"段（查 git status/diff 续或重做，worker 始终是干活的人）。

**并行编排（`internal/orchestrate/parallel.go` + `gitx/worktree.go`，#3）**：feature 级并行——每 feature 在独立 git worktree 推进（`driveFeature`：owner/reviewer 循环，不 merge），最多 `MaxConcurrency` 个并发；merge 串行回 base。**关键约束/洞察**：
- pact.Merge 内部 `git checkout base`，而 base 同一时刻只能一个 worktree 持有 → **主树 park 到一次性分支释放 base，各 worktree 在 mergeMu 锁下串行完成 merge**（复用 pact.Merge 不改协议引擎）。
- **并行 merge 的 ledger 冲突**：两 feature 从同一 base 独立追加 log.jsonl/STATE.yml，第二个 merge 撞冲突。解法靠"StateProjection 从 log.jsonl 重算、STATE.yml 只是缓存"这一事实——`.gitattributes` 给 `.pact/log.jsonl` + `.pact/STATE.yml` 设 `merge=union`（log union=全部事件正确；STATE 变 garbage 但无所谓），`.pact/orchestrate/` 运行时文件入 .gitignore。
- pact.Merge 把 merge 事件 append 到工作树但不 commit（串行流程读工作树即可）；并行流程 RemoveWorktree 会丢它 → merge 后须显式 commit merge 事件。
- CLI `orchestrate --max-concurrency N`（=1 串行回退；与 --feature/--dry-run 互斥）。

**收尾交付步（`internal/finish`，#5）**：merge 到本地 main 后 push / 开 PR（`gh pr create`，gh 缺时降级打印手动命令）。CLI `pactify finish`。

**session 清理（`internal/sessions`，#6）**：按 kind 声明 list/prune 命令的框架，不支持的 kind 优雅 unsupported（no-op 报告，非 error）。CLI `sessions list/prune`。**（2026-06-15 升级见下「session 清理升级」。）**

**配方（`internal/recipe`，#11）**：命名任务图模板（add-tests/review-harden/spec-to-plan），`{{goal}}` 占位展开成 pact 任务 spec。CLI `recipe list/show`。把门槛从"手写任务图"降到"选配方"。

**项目设置向导（`internal/wizard`，#1）**：从机器注册的 agents 建议项目座席 roster，`Validate` 查角色覆盖。CLI `setup suggest` 保留；serve setup API 已在 AJ-723 删除。

**planner review（#7）**：治理规划与 apply 使用 CLI `pactify plan` / `pactify plan apply`；serve plan API 和旧 Plan 视图已退役。

**UI polish（#12，部分）**：Spinner / Button-loading / Alert 原语 + 微交互（active-press / hover-lift / fade-rise，全 prefers-reduced-motion 兼容）+ RightRail 三动作 loading + Alert 错误。后续 UI 规划见 `docs/roadmap-next.md`。

### 增量子系统（2026-06-14~16，v0.4.0）

**成本 / 可观测**：AJ-723 删除仅由旧 serve stats API 使用的 `internal/stats` 与
`internal/diffstat`。当前 dashboard 用 `/api/sub/cost` 和 job summary 展示 harness 成本与用量；
编排器仍使用 `internal/tokens` 记录 token。

**巡检 watchdog D2（`internal/orchestrate/runner_idle.go`）**：把钝的"无输出即杀"升级为**进度感知巡检**——`fsProgress(dir)`（有界 mtime 遍历，跳过 .git/node_modules，可注入）；仅当 `idleFor() >= idle 且 fsProgress() >= idle`（既无输出又无落盘）才杀，否则发"⟳ patrol: still working"通知。安静但在写盘的 agent 不被误杀。

**GLM 端点可配（`internal/orchestrate/runner.go` + `internal/secret`）**：GLM 不是独立 kind，是 claude-code 指向 GLM 的 Anthropic 兼容端点（glm-* 模型时经 `glmEnv` 注入 `ANTHROPIC_BASE_URL` + Keychain token）。端点按计划可配：Keychain `pactify/glm-base-url` 有值用之（中国版 `open.bigmodel.cn`），否则全球默认 `api.z.ai`；解析永不报错（缺省回退）。token 存 Keychain `pactify/glm`。

**Settings**：当前 dashboard 通过 `/api/sub/router`、`/api/sub/harnesses` 与 quota 写接口
管理 harness 路由；旧 agent 扫描、注册、配置 API 已删除，治理 agent 管理继续使用 CLI。

**session 清理升级（`internal/sessions`，opencode-first，2026-06-15）**：从"按 kind 框架"升级为**任务 accept 后自动关闭该棒 session**。实测各 agent session CLI 后定 opencode 优先（唯一有干净 `session list` + `session delete <id>`，且常驻 daemon 最该清）。机制：runner 给 opencode `run` 打 `--title pact:<seat>`（不改输出格式）→ accept 后 `sessions.CleanupByTitle` list→匹配 title→按 id 删 owner+reviewer 的 session；门控在 `Options.SessionRun`（仅 `CleanupSessions` 开时注入真 exec，测试不 spawn）；CLI 默认开、`--keep-sessions` 关。gemini-cli 集成已退役；codex（archive）/kimi/claude（无 CLI）在当时延后。

**dark product UI refresh（web/，本地 main 未发布，2026-06-19）**：dashboard 从浅色切到**深色**，照 `design_handoff_dark_product_ui` 设计稿把全部 6 屏重制。本质是 **token swap + 少量新 pattern**——`tokens.css` 的 `@theme` 改深色（背景/边框/文字/语义；role 三色回归 brand.md 一直定义的深色基底 `#ffd479/#8ab4ff/#6ee7a0`，≈70% 的 app 经 `var()` 自动迁移）+ `index.css` 中硬编码浅色值（`canvas-stage`/`skeleton`/`rgba(18,22,31,…)` hover/inset）逐一 re-tone。新 pattern：`ui/MetricStrip`（mono `RUN/TOK/×iter` 统计条，live 值蓝、估算斜体）；**Board** 全宽 5 列（Assigned/Working/Review/Accepted/Shipped）+ 上下文头条（feature 过滤 chip + 座席簇 + New task）+ 卡片统计条 + Review 列内联 Accept/Changes，去掉固定左 dock（座席并入头条），accepted+shipped 折叠到最近 6；**Canvas** 底部中央 NL 命令坞（目标 + 配方 chip + 并发段 + Run，接现有 dispatch）；**Live** 两栏 + 右侧事件流终端（`#07090d`，pact log 着色 + 座席在场）；**Setup** 旅程步进器 + 角色切换 pill + scope 交叉链；**Settings** scope 双栏（Project/Machine/Account）。早先的**蚂蚁运动语言**（in_progress 爬行 / awaiting 绕行 / changes 顿挫 + status-colored 脉冲 + dep 边行进蚁）保留。Canvas 合并门 = vitest + Playwright e2e 双绿（工艺规约见 CLAUDE.md）。

**pactify.dev 文档站外壳（`site/`，已上线生产）**：Astro 站新增 `DocsLayout`（左目录 `docsNav` + 右内容，scrollspy active），`/introduction` 重做为文档落地页，`/protocol`·`/onboarding` 共用同一外壳。

**native audit layer（`internal/audit`，spec `docs/superpowers/specs/2026-06-16-native-audit-layer-design.md`）**：本地优先的工具调用权限审计。
- **捕获**：`pactify audit hook --kind <kind>` 读客户端 PreToolUse JSON（stdin）→ `FromHook` 归一（per-kind tool 映射 + 脱敏）→ 追加一行 JSONL 到 `~/.pactify/audit/<project>/<date>.jsonl`；log-only（恒 allow，永不阻断 agent）。治理(deny/ask)/预设是同一条缝上的 P2。
- **归属**：orchestrate runner（`LaunchContext`）把 `PACT_AGENT_ID/PACT_TASK_ID/PACT_PROJECT` 注入 agent env，hook 读出 → 每条记录带座席/任务/项目。
- **客户端接入**（实测）：claude-code 用命令式 PreToolUse hook（`.claude/settings.json`，`audit install --claude-code`）；opencode 无命令 hook → JS 插件 `.opencode/plugin/pact-audit.ts`（`tool.execute.before` → shell out hook，`audit install --opencode`，真 run 端到端验证）。
- **读取**：CLI `audit log/summary/prune`；serve audit API 与旧 RightRail Audit 镜头已退役。
- **门控**：`Options.SessionRun` 式注入，测试不 spawn CLI。脱敏：summary 短截断 + 掩 `Bearer/sk-/token` 等。
