# Pactify — Architecture

> Last updated: 2026-06-09 | Status: Draft（地基决策已锁，CLI 待实现）

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
