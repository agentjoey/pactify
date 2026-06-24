# Pactify — Architecture

> Last updated: 2026-06-24 | Status: **v0.8.2 已发布**（dashboard 隐藏 pact 内部 worktree:`serve` worktree 列表过滤 `.pact/orchestrate/` 树 + `pact-*-park` 分支,消除「同一任务跨 tree 状态不一致」)+ v0.8.1（worker 投递完整性补丁:`PACT_DIR` 钉 worker 到 driver worktree · 回灌 event_id union 合并 · escalation 归因)+ v0.8.0（coordination-authority + dark product UI）— 协议 v1 冻结 · Go CLI + MCP + dashboard · orchestrate 自主驱动 + planner · 成本/可观测(D1) + 巡检(D2) · session 清理(opencode) · GLM 端点可配 · Settings agent 管理 · **深色 dashboard（dark product UI，6 屏照设计稿重制）** · native audit layer(claude-code hook + opencode 插件) · pactify.dev 文档站 · **coordination-authority**(base hygiene `.git/info/exclude` · autonomous 默认 sandbox · merge 跨进程 flock 锁 · MCP 项目按名寻址 · base 写入契约「只 merge 写 base / fetch-aware 不分叉 / 默认不 push / accept 不连 merge / checkpoint base 守卫」· per-project `config gate` · 空分支拒绝 ship · 机器提交 `--no-verify`)。下方「增量子系统」段记录这些子系统的细节。

## Overview

Pactify = **多 agent 协同协议 + 薄 CLI + 可视化编排**，分三产品层（[ROADMAP](ROADMAP.md)）：

```
Pact-Base   读 + 协议机制     免费开源
Pact-Squad  写 + 可视化编排   主功能免费 + 部分付费
Pact-Team   协作 + 云         付费商业化
```

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

## 通讯架构（地基，Phase 1 冻结）

**`log.jsonl` 既是审计事实源，也是 agent 之间的通讯总线。**

```
agents ──┬─ shell:  pactify checkpoint/accept   (任何 agent，零依赖)
         └─ MCP:    tools + event subscription   (MCP 客户端)
                          │
                   log.jsonl  (append-only 事件总线)
                          │
                   pactify serve
                     ├─ MCP server（事件订阅 + 工具暴露）
                     ├─ fsnotify watch → SSE/WS → 本地 dashboard
                     └─ (Phase 4) 云端 relay → Pact-Team
```

- **shell 写入与 MCP 调用产出同一种事件** → 一套 schema 服务所有入口
- **本地零依赖**（文件 + 单 binary），**云端只是在上面加 relay**，协议不变

### M3.4 relay 接口

`pactify serve` 内置 best-effort 异步 relay，将每个项目的 log 事件 POST 到可配端点。挂载点在 `drainNew` 中（`hub.broadcast` 之后，旁路非阻塞）：

```
pactify serve
  └─ fsnotify → watchLoop → drainNew
       ├─ hub.broadcast(id, line)    ← SSE 订阅者
       └─ relay.enqueue(id, line)    ← 远端 relay 端点（可选）
```

**语义：**
- **best-effort**：relay 失败/超时/排队满 不阻塞 SSE，不影响 offset 推进，不回传错误到 watcher。
- **异步队列**：有界 256 条，FIFO；满时丢弃最旧条目并递增 `dropped` 计数器。
- **重试**：单条最多 4 次尝试（1 次初始 + 3 次退避重试：1s、2s、4s），全部失败递增 `dropped` 后静默丢弃。
- **可中断**：`stop()` 在重试间隙检查，不会卡死 shutdown。

**配置：**
- CLI flag：`serve --relay-url <url> [--relay-token <token>]`
- 环境变量：`PACT_RELAY_URL`、`PACT_RELAY_TOKEN`
- 空 URL（默认）= relay 禁用，`newRelay("","")` 返回 nil，`enqueue` 是安全空操作

**线格式（POST JSON）：**
```json
{
  "project": "pactify",
  "event": { "event_id": "...", "event_type": "...", ... }
}
```
- `Content-Type: application/json`
- token 非空时附带 `Authorization: Bearer <token>`
- event 行非合法 JSON 时，原始文本被转义为 JSON string 后放入 envelope，不 panic

**端点要求：** HTTPS 端点；2xx 视为成功，4xx/5xx 触发重试；10s 超时。

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
pactify serve          # MCP server + SSE dashboard
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

见 [ADR-001](decisions/ADR-001-open-core-boundary.md)：守 Team。所有付费价值落在云端 relay 之上；log.jsonl 事件 schema 同时服务本地（免费）和云端（付费），零改动 agent 端。

## Squad (Phase 3, M3.1+M3.2)

The serve dashboard gains author powers — the same binary, `pactify serve --seat <id>`:

- **Dir-aware engine**: `pact.At(dir).As(seat)` runs any verb against any registered
  project from any cwd; package-level funcs remain `At(".")` wrappers (CLI/MCP unchanged).
- **Author API** (localhost trust model): `POST /api/projects/{id}/tasks` (spec file),
  `POST .../verbs/{assign,accept,changes,merge}` executed as the acting seat (must be in
  the project roster; engine rules apply — the acting human cannot self-accept either),
  engine errors returned verbatim as 422. Writes serialize per project (mutex).
  `GET/PUT .../squad/layout` stores the canvas layout sidecar at `.pact/squad/layout.json`
  (UI-only, outside the protocol).
- **deps** (protocol v1 additive): optional `deps` on assign — validated (same feature,
  exists, acyclic), enforced at join (a task with un-accepted deps cannot be joined).
  Deps-free logs render byte-identical to the bash reference.
- **Canvas** (web/, React Flow): task/seat/feature/draft nodes, role-colored per
  docs/brand.md (orchestrator→product, reviewer→design, worker→dev), dep edges,
  drag-to-dispatch onto seats, review accept/changes/merge from the rail, SSE-live
  updates, layout persistence, awaiting-review pulse + stale indicators.
  Build mode keeps drafts local until dispatch — the log stays protocol-pure.

### Ops panel (M3.3a)

The serve binary also runs the squad's ops surface — managing which projects are
served and how they are wired, without leaving the dashboard:

- **Registry endpoints** (`GET /api/registry`, `POST /api/registry {path}`,
  `DELETE /api/registry/{name}`): add/remove projects at runtime (no server restart) —
  add validates the path (absolute → exists → git repo → has `.pact/`, no implicit init)
  and persists to the shared `~/.pactify` registry file so CLI and serve stay consistent;
  remove stops the live watcher but never touches the on-disk `.pact/` files. The list
  folds a content-aware status (validity, seat count, last-event ts) per project.
- **Wiring probes** (`GET .../wiring`, `POST .../wiring/{kind}`): the probe is the single
  source of truth shared with `pactify doctor` (`agent.ProbeWiring`) — each kind reports
  `wired` from its config marker or entry managed-block; POST bakes the entry block and
  merges the pact server (TOML kinds are doc-only, returning the snippet to copy).
- **Join client provenance** (advisory): the seats endpoint (`GET .../seats`) folds the
  roster with each seat's most recent join client+version — a CLI join stamps
  `pactify-cli`, richer hosts stamp their own identity — and flags `clientChanged` when a
  seat's last two joins name different clients. Provenance is advisory, never gating.

### Comms visualization (M3.3b)

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
- **Replay = prefix fold**: the projection is a pure fold, so historical state =
  `Project(evs[:n])`. serve adds two read-only endpoints — `GET .../timeline`
  (`{total, events:[{n, ts, type, actor, task?, feature?}]}`, a light index with no
  payloads) and `GET .../state?at=N` (folds the first N events; `at=0`→empty, `N≥total`
  clamps to the full/live shape, malformed `at`→400, absent `at`→unchanged). Both share the
  existing read path (`event.ReadAll`); no mutex beyond today's `handleState`. The
  **ReplayBar** scrubs `0..total`, fetching `state?at=N` per position; SSE snapshots are
  ignored while scrubbing.
- **Replay mode is read-only**: every author/ops mutation (dispatch, task editor, drag,
  review verbs) is disabled or hidden while not live — the `replaying` flag short-circuits
  the handlers; LIVE refetches the current state and resumes the SSE stream.
- **Live pulse**: each SSE-applied snapshot (live mode only) diffs the changed task(s) and
  pulses their node + wait edges once in the actor's role color (the site's cable-pulse
  brand idiom). Pure CSS keyframe, fully gated off under `prefers-reduced-motion`.

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

### Canvas P0 (interaction-foundation rework, 2026-06-12, PR #20-#22)
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
- **per-kind headless runner**（agent.Adapter.Runner）：opencode→`opencode run`、claude-code→`claude -p`、gemini-cli→`gemini -p`；GUI/desktop kind 无 runner → fail-closed。Runner 接口化（生产 exec 与测试 fake 共用）。座席→kind 经 `--seat-kind` 映射（kind 未持久化进协议态）。
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

**session 清理（`internal/sessions`，#6）**：按 kind 声明 list/prune 命令的框架，gemini 已接，其余优雅 unsupported（no-op 报告，非 error）。CLI `sessions list/prune`。**（2026-06-15 升级见下「session 清理升级」。）**

**配方（`internal/recipe`，#11）**：命名任务图模板（add-tests/review-harden/spec-to-plan），`{{goal}}` 占位展开成 pact 任务 spec。CLI `recipe list/show`。把门槛从"手写任务图"降到"选配方"。

**项目设置向导（`internal/wizard`，#1）**：从机器注册的 agents 建议项目座席 roster（claude 系 lead=orchestrator+reviewer，其余 worker），`Validate` 查角色覆盖。CLI `setup suggest`（打印 roster + gaps + 精确 apply 命令）；`GET /api/setup/suggest`。是"注册 agent → 能干活"的桥。

**planner review（serve `plan.go` + web `PlanReview.tsx`，#7）**：`GET /api/projects/{id}/plan/{feature}` 读 plan manifest + 校验；Plan 视图（key 5）展示任务图（owner→reviewer/deps/verify）+ valid 徽章 + apply 提示。

**UI polish（#12，部分）**：Spinner / Button-loading / Alert 原语 + 微交互（active-press / hover-lift / fade-rise，全 prefers-reduced-motion 兼容）+ RightRail 三动作 loading + Alert 错误。后续 UI 规划见 `docs/roadmap-next.md`。

### 增量子系统（2026-06-14~16，v0.4.0）

**成本 / 可观测 D1（`internal/stats` + `internal/diffstat` + `internal/tokens`）**：per-task / per-agent 的工时、代码量、token 统计，纯派生 + 真实数据。
- `stats.Compute(events, now)` 从 log 时间戳算每任务/座席工作时长（pure derived）；`WithTaskLOC` 叠加代码量、`WithTokens` 叠加 token。
- `diffstat.NumStat(repoDir, from, to, pathspec…)` 用 `git numstat` 算 added/deleted/files；`diffstat.Commits(repoDir, rev, grepFixed)` 按 commit subject 字面匹配找一个任务自己的 checkpoint commits；`IsRepoRoot` 守卫（仅当路径是自身 git root 才算 LOC，避免 monorepo 误算）。
- **LOC 按任务精确归因（2026-06-19 修复）**：旧 `WithLOC` 对整条 feature 分支 `base..branch` 做一次 diff，导致同一分支上每个任务都显示相同的分支总量；改为 `WithTaskLOC`——对每个任务**自己的** checkpoint commits（subject `pact <task>:`，经 `diffstat.Commits` 找到）逐个 `sha~1..sha` numstat 求和，排除 `.pact` 簿记。每任务 LOC 各异，与 `WithTokens` 同形。
- `tokens` 在 `.pact/orchestrate/tokens.json` 存 per-task token，`Parse(kind, output)` 解析各 agent 输出（claude `usage.input+output` 已验证；JSONL last-usage + 顶层 fallback）。
- 经 `GET /api/projects/{id}/stats` 暴露；dashboard RightRail inspector + office「Cost」镜头展示（⏱ 时长 · +added/−deleted · ⛁ token）。

**巡检 watchdog D2（`internal/orchestrate/runner_idle.go`）**：把钝的"无输出即杀"升级为**进度感知巡检**——`fsProgress(dir)`（有界 mtime 遍历，跳过 .git/node_modules，可注入）；仅当 `idleFor() >= idle 且 fsProgress() >= idle`（既无输出又无落盘）才杀，否则发"⟳ patrol: still working"通知。安静但在写盘的 agent 不被误杀。

**GLM 端点可配（`internal/orchestrate/runner.go` + `internal/secret`）**：GLM 不是独立 kind，是 claude-code 指向 GLM 的 Anthropic 兼容端点（glm-* 模型时经 `glmEnv` 注入 `ANTHROPIC_BASE_URL` + Keychain token）。端点按计划可配：Keychain `pactify/glm-base-url` 有值用之（中国版 `open.bigmodel.cn`），否则全球默认 `api.z.ai`；解析永不报错（缺省回退）。token 存 Keychain `pactify/glm`。

**Settings agent 管理（`internal/agent` + serve `agents.go` + web `ops/`）**：
- **一键扫描 + 手动添加**（`AgentRoster.tsx`）：`GET /api/agents` 每次实时 `agent.Scan()` 重探安装情况；Scan 按钮重拉、一键 Register 已装未注册项；「Add manually」登记支持清单内未检测到的 kind（自定义 binary 路径延后）。
- **模型下拉**：`agent.CandidateModels(kind)`（每 `RunnerProfile` 一份候选清单）经 config DTO 的 `candidate_models` 暴露；AgentConfig 的 model 字段升级为下拉（default · 候选 · custom…），无候选回退纯文本。
- **统一 agent logo**（`web/src/lib/agentLogos.tsx`）：`kind → AgentLogo` 品牌渐变实色块，接进 Setup / Agents 引导条 / Ops AgentConfig 真卡片。

**session 清理升级（`internal/sessions`，opencode-first，2026-06-15）**：从"按 kind 框架（gemini 仅 list）"升级为**任务 accept 后自动关闭该棒 session**。实测各 agent session CLI 后定 opencode 优先（唯一有干净 `session list` + `session delete <id>`，且常驻 daemon 最该清）。机制：runner 给 opencode `run` 打 `--title pact:<seat>`（不改输出格式）→ accept 后 `sessions.CleanupByTitle` list→匹配 title→按 id 删 owner+reviewer 的 session；门控在 `Options.SessionRun`（仅 `CleanupSessions` 开时注入真 exec，测试不 spawn）；CLI 默认开、`--keep-sessions` 关。gemini（按 index）/codex（archive）/kimi/claude（无 CLI）延后。

**dark product UI refresh（web/，本地 main 未发布，2026-06-19）**：dashboard 从浅色切到**深色**，照 `design_handoff_dark_product_ui` 设计稿把全部 6 屏重制。本质是 **token swap + 少量新 pattern**——`tokens.css` 的 `@theme` 改深色（背景/边框/文字/语义；role 三色回归 brand.md 一直定义的深色基底 `#ffd479/#8ab4ff/#6ee7a0`，≈70% 的 app 经 `var()` 自动迁移）+ `index.css` 中硬编码浅色值（`canvas-stage`/`skeleton`/`rgba(18,22,31,…)` hover/inset）逐一 re-tone。新 pattern：`ui/MetricStrip`（mono `RUN/TOK/×iter` 统计条，live 值蓝、估算斜体）；**Board** 全宽 5 列（Assigned/Working/Review/Accepted/Shipped）+ 上下文头条（feature 过滤 chip + 座席簇 + New task）+ 卡片统计条 + Review 列内联 Accept/Changes，去掉固定左 dock（座席并入头条），accepted+shipped 折叠到最近 6；**Canvas** 底部中央 NL 命令坞（目标 + 配方 chip + 并发段 + Run，接现有 dispatch）；**Live** 两栏 + 右侧事件流终端（`#07090d`，pact log 着色 + 座席在场）；**Setup** 旅程步进器 + 角色切换 pill + scope 交叉链；**Settings** scope 双栏（Project/Machine/Account）。早先的**蚂蚁运动语言**（in_progress 爬行 / awaiting 绕行 / changes 顿挫 + status-colored 脉冲 + dep 边行进蚁）保留。Canvas 合并门 = vitest + Playwright e2e 双绿（工艺规约见 CLAUDE.md）。

**pactify.dev 文档站外壳（`site/`，已上线生产）**：Astro 站新增 `DocsLayout`（左目录 `docsNav` + 右内容，scrollspy active），`/introduction` 重做为文档落地页，`/protocol`·`/onboarding` 共用同一外壳。

**native audit layer（`internal/audit` + serve + web，spec `docs/superpowers/specs/2026-06-16-native-audit-layer-design.md`）**：本地优先的工具调用权限审计。
- **捕获**：`pactify audit hook --kind <kind>` 读客户端 PreToolUse JSON（stdin）→ `FromHook` 归一（per-kind tool 映射 + 脱敏）→ 追加一行 JSONL 到 `~/.pactify/audit/<project>/<date>.jsonl`；log-only（恒 allow，永不阻断 agent）。治理(deny/ask)/预设是同一条缝上的 P2。
- **归属**：orchestrate runner（`LaunchContext`）把 `PACT_AGENT_ID/PACT_TASK_ID/PACT_PROJECT` 注入 agent env，hook 读出 → 每条记录带座席/任务/项目。
- **客户端接入**（实测）：claude-code 用命令式 PreToolUse hook（`.claude/settings.json`，`audit install --claude-code`）；opencode 无命令 hook → JS 插件 `.opencode/plugin/pact-audit.ts`（`tool.execute.before` → shell out hook，`audit install --opencode`，真 run 端到端验证）。
- **读取**：CLI `audit log/summary/prune` + serve `GET /api/projects/{id}/audit` + dashboard RightRail 的 Audit 镜头（紧挨 D1 Cost）。
- **门控**：`Options.SessionRun` 式注入，测试不 spawn CLI。脱敏：summary 短截断 + 掩 `Bearer/sk-/token` 等。
