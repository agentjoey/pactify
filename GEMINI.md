# Pactify — Agent Context (GEMINI.md：gemini-cli · antigravity/agy)

## ⭐ Session 启动（每次必执行）
```bash
git pull
cat .agent/CURRENT.md
```

## Project Overview
**Use other coding agents as subagents from Claude Code.**

Delegate large tasks — multi-file changes, investigations, anything that needs test runs — from Claude Code to Codex (default) or Kimi (experimental). pactify runs them as background jobs, independently verifies the model that actually ran, reports tokens and quota, isolates the change in a git worktree for you to review, apply or discard, and leaves no trace in the agents' own session lists.

Claude Code commands: `/delegate` (default: codex), `/codex`, `/kimi` (experimental; no read-only mode).
CLI: `pactify sub run | wait | status | logs | result | cancel | apply | discard | doctor`.
Run `pactify sub doctor` after installation: it makes real harness calls and records verified CLI versions.
CLI isolation requires `--worktree`; the plugin uses it for large tasks. Resume rework with `--resume` in the same worktree.
Session hiding means archiving from the agents' own lists; pactify keeps job logs and reports hiding or verification failures as warnings.

**Governance mode (optional):** pact protocol, ledger, orchestrator and `pactify serve` dashboard are frozen; hosted relay is retired.
See [Quickstart](README.md), [Architecture](docs/architecture.md), [Harness design](docs/specs/harness-subagents.md),
[Phase 0 evidence](docs/phase0-report.md) and [Harness self-check](docs/sub-doctor.md).

**Location:** ~/AgentWorks/Code_Claude/pactify
**GitHub:**   agentjoey/pactify
**Version:** **v0.12.0**（2026-10-15）——**编排器方向的最后一个正式版本**，也是转向前的回滚锚点（`go install github.com/agentjoey/pactify/cmd/pactify@v0.12.0` 可装回）。自 v0.11.0 起 PR #48–#66：fallback 并行闭环、checkpoint run 守卫、ACP 模型绑定（opencode 根治）、单一规范账本 WS-A/WS-B（flag 默认关）、Settings Agents 面板、仓库公开前清理，以及发版前的死代码清理（#66）。完整版本历史见 **[`docs/CHANGELOG.md`](docs/CHANGELOG.md)**；开着的条目见 `docs/backlog.md`（gitignored）。

**当前状况（2026-10-15 发版时实测，非文档互证）**
- **⭐ 产品方向已转（2026-10-15 Human Owner 拍板，`[HARNESS-SUB]`）**：pactify 转为 **harness 路由器**——在 Claude Code 里一个 slash 命令就把其他 coding agent harness 当 subagent 拉起执行**大任务**（类比 claude-code-router，但路由 harness 不路由模型）。第一版**只做 Claude Code 作为 host**，callee 只做 **codex + kimi**；gemini-cli 已移出范围（由 agy 取代）。现有协议 / 账本 / 看板 / 云端**降级**：冻结新投入，协议保留为可选「治理模式」；云端退役不再恢复部署，单一规范账本暂停在 WS-B。方案见 [`docs/specs/harness-subagents.md`](docs/specs/harness-subagents.md)，Phase 0 实施计划见 [`docs/specs/harness-subagents-phase0-plan.md`](docs/specs/harness-subagents-phase0-plan.md)；进度在 Linear 项目 P-AJ-18 的 M0 里程碑。**Phase 0 已完成（2026-10-15）**：`pactify sub` 运行时 + Claude Code 插件 `pactify`（`/codex`、`/kimi`）已合入 main（PR #69–#71）；26 次真实委派里 codex 全部达标，kimi 的大任务一次完成率 6/10 未达标、只读档不收敛（第一版不开放 kimi 只读），详见 [`docs/phase0-report.md`](docs/phase0-report.md)；是否进 M1 待 Owner 拍板。上面几条里关于云端、`[UI-GATE]`、账本重构的内容**随降级冻结**，仅作背景。
- **本地闭环**：`go build ./...` 绿；`go test ./...` 全绿（随附 codex schema 已于 2026-10-15 刷新到 0.162.1，PR #69）。`-race`（pact/orchestrate/serve）· bats · vitest 639/639 · tsc · Playwright e2e 7/7 均绿。
- **云端已退役**：`pactify-relay` / `-staging` 两个 fly app 均 0 machines，`origin/production` 停在 2026-07-04。按转向决策**不再恢复部署**；`cloud/` 与 `internal/cloudauth` 等代码随后单独 PR 删除。（历史约束仅作背景：若将来重新拉起 relay，必须先部署 relay 再放二进制。）
- **随降级冻结、未修的已知问题**：`[RUNGUARD-DEAD]` checkpoint 的 run 守卫因 `updated_at` 时间格式不符在生产里从不生效；`[UI-GATE]` Board 无补 checkpoint 入口、`CommitAll` 全树提交的归属问题。证据链在 backlog。
- **`[ACP-MODEL]`**：opencode 已根治（`OPENCODE_CONFIG_CONTENT` 注入，#55）；kimi-cli / gemini-cli / claude-code / codex-cli 走 ACP 时模型绑定仍只得到警告。命令行传输下 6 种 kind 均能兑现 `-m`。
- **仓库已公开**（2026-09-04，全历史凭据审查零命中；两个曾在对话里出现的 token 是否已轮换仍待人工确认）。

**Technical docs:** [Architecture](docs/architecture.md) · [Deployment](docs/deployment.md) · [Operations](docs/operations.md)

## Tech Stack
| Layer | Tech |
|-------|------|
| CLI | **Go** 单静态二进制（cobra），`cmd/pactify` + `internal/*` 37 包 |
| Dashboard | Vite + React + Tailwind v4（`web/`），构建产物经 `go:embed` 打进 `internal/serve/dist`（git 跟踪） |
| Protocol | Plain files (`.pact/`)：`log.jsonl` append-only 为源，`STATE.yml` 为投影 |
| Cloud | fly.io relay（`cloud/relay`，Node + Prisma）+ Vercel 静态站；zod 线协议在 `cloud/wire` |
| CI | GitHub Actions（`ci.yml` + `codex-schema.yml` + `release.yml`/goreleaser） |

## Key Implementation Details
- `.pact/` = 产品协议文件（用户 repo 使用）；`.agent/` = 本 repo 开发工作台（不是产品一部分）
- `log.jsonl` 为源，`STATE.yml` 为投影，CLI 重算；防多 agent 并发写 STATE 冲突
- worker 不能自标 `accepted`，只能置 `awaiting_review`；只有 reviewer 能转 `accepted`
- 拉取式派发：worker 启动时读 STATE.yml，人是"启动按钮"
- 画布工艺规约已随 Canvas 视图移除退役（2026-07-07 视图收敛：Canvas 删除、Live 并入 Board——见 docs/backlog.md [VIEWS]）；UI 改动合并门 = vitest + Playwright e2e 双绿保持不变

## Dev Commands
```bash
go build ./... && go test ./...          # Go 全仓（编译 + 37 包测试）
go test -race ./internal/{pact,orchestrate,serve}/...
cd web && npx tsc --noEmit && npx vitest run   # 前端类型 + 单测
cd web && npx playwright test            # e2e（会起 e2e/mock-server.mjs）
bats tests/                              # CLI 端到端
cd cloud && pnpm -r build                # 云端 workspace（wire/crypto/relay）

cd web && npm run build && go build -o pactify ./cmd/pactify   # 重建带 dist 的二进制
codesign --force -s - ./pactify          # ⚠️ 给 launchd 跑之前必做（否则 OS_REASON_CODESIGNING SIGKILL）
```

## Release 后必做（任何 agent 通用）
1. **`docs/CHANGELOG.md` 顶部加一节**（`## vX.Y.Z — 日期 · 一句话主线`），写「改了什么 + 为什么 + 跑了哪些门」。
2. **`CLAUDE.md` 的 Version 段改写为当前版**（只保留当前版 + 「当前状况」几条实测事实 + 指向 CHANGELOG 的链接）——**不要再把历史往这一段里堆**，2026-08-29 之前它已累积成 17KB 的单行，正是文档漂移的来源。
3. **`AGENTS.md` / `GEMINI.md` 同步**（三份 entry 文件的 Overview/Tech Stack/Dev Commands/Version 必须一致；pact 托管块与各自的 kinds 归属行不要动——**正文里绝对不要出现托管块的起止标记字面量**，`internal/agent` 靠首次出现定位块边界）。
4. 如有架构变更：更新 `docs/architecture.md`（它的状态行同样只写当前版 + 链 CHANGELOG）。
5. `.agent/CURRENT.md`（本地工作台，gitignored）：补充交接说明。

## 工程约定（2026-06-19 dark-ui 复盘）
- **已知妥协必登记**：在代码注释里写下 later/TODO/「精确做法需…」之类的妥协时，**同步在 docs/backlog.md 加一行**，否则债隐形——`internal/stats` 的 per-task LOC bug（每任务都显示整分支 +842/−307）正是 `WithLOC` 注释自承认「精确归因需 commit SHA」却搁置成债，直到用户肉眼发现才修。
- **前端改动看效果走 dev proxy**：`cd web && npm run dev` 经 vite proxy 直连常驻 serve（PACTIFY_SERVE_URL，默认 :17082）热重载；只有最终验收 / 要 live 才 `npm run build` + 重建二进制 + `launchctl kickstart`。
- **视觉门**：UI 改动提交前必须 playwright 截图实测（`node web/scripts/shots.mjs [view]`；escalated/review-gate 等无法按需触发的态用 `live-gate-shot.mjs` 注入 mock）——vitest/tsc 绿 ≠ 视觉对。要证明截图来自**最终 build**（而非长驻 daemon 可能提供的陈旧 dist）用 `node web/scripts/shot-dispatch-review.mjs`——它 spawn hermetic 的 `e2e/mock-server.mjs`，直接服务 `internal/serve/dist`。

<!-- pact:begin (managed by pactify — edit outside this block) -->
<!-- pact:kinds: gemini-cli -->

# pact protocol

This repo uses the **pact protocol** (v1). Seats (who does what) are listed in
`.pact/PROJECT.md` and `.pact/STATE.yml`.

**Your identity — bind it to this working copy first.** Your seat is resolved
from `PACT_AGENT_ID` (env), else the untracked `.pact/seat` file. Set the
file once per working copy:
```bash
pactify seat use <your-seat-id>   # from the roster in .pact/PROJECT.md
```
For concurrent seats in the same repo, use a separate git worktree per seat.

**Primary — MCP:** the `pact` MCP server is wired into your config. Use its tools
(projects / status / join / assign / checkpoint / accept / changes / merge / validate) and
resources (`pact://state`, `pact://log`). Cold start: call `status`, then `join`
(registers your seat and checks out your feature branch). Every action tool takes an
optional `project` (a name from `projects`) to act on another registered repo without
restarting — default is this repo.

**Fallback — shell** (if MCP is unavailable):
```bash
pactify seat use <your-seat-id>   # if not already bound
pactify join --roles <your-roles>
```
then `pactify help` for the verbs.

**The two rules:** a worker cannot self-accept (only the task's reviewer accepts); a
feature cannot merge until all its tasks are accepted.
<!-- pact:end -->


<!-- BEGIN SHARED-INTAKE v2 -->
## Requirement intake and work tracking

Owner decision 2026-10-15. Needs are discussed in Notion first; Linear holds only work the Owner has confirmed (approved on the Requests page, or confirmed directly in a session). The Intake page holds the stage table, the page templates and the databases: https://app.notion.com/p/3f98c5850c4380588d3fcfa22d8bc62e

- Project: `Pactify` (the value of `Project` in the Requests and Slices databases; always filter by it).
- Notion data sources: **Requests** `b20c9483-9aab-42fe-bea0-edafb0ee193d`, **Slices** `53cfefa3-69e8-4fcd-99c9-296946092dd2`. Query them directly by id with `Project = Pactify`. Do not find them by searching Notion.
- Linear: team `AJ`, project `Pactify` (P-AJ-18). Resolve the project by its id, because names change. A slice is a **Milestone** inside this project, named after the slice goal.

### Where to look

- Needs, requests, ideas, "pending", "inbox", "approved" → the Requests database (Notion). Issues, tickets, tasks, "todo", "in progress" → Linear. "How many pending requests" is a Requests question, so answer it from Notion and never from Linear counts.
- Read only rows where `Project = Pactify`. Do not open other projects' rows or pages, and do not search Notion by keyword (search returns other projects' pages). Notion pages that predate this flow (old backlogs, decision lists) are not the source for this flow; read them only when the Owner points at one.
- If Notion is not reachable in this tool, say so. Do not substitute a Linear count for a Requests count.

### Two kinds of session

- **Discussion session:** reads and writes Requests, analyses, records the discussion on the request page, and after the Owner confirms creates the Linear Milestone and issues. Writes no code.
- **Development session:** takes issues from Linear and implements them. It reads Requests when the Owner asks about requests (see above); otherwise it reads only Linear.

### Rules

1. A Linear issue is created only after the Owner has confirmed the need: the request's Stage is Approved in Notion, or the Owner confirmed it directly in any session, including a development session (small, clear items found outside Intake). Agents cannot delete Linear issues, so an unconfirmed need must not be created there; write it to Requests as Inbox instead.
2. A Linear issue is self-contained: put the analysis conclusion and the acceptance criteria in its description. Sub-agents and worktrees cannot rely on reading Notion. The first line of the description is its source: `Request: <Notion page URL>` or `Confirmed: <YYYY-MM-DD> <who> <one line of what was confirmed>`. An issue with neither line is a violation; list them by searching the project.
3. Ideas and defects found while developing that the Owner has not confirmed go to Requests as Inbox. If the Owner confirms one on the spot, create the issue directly with a `Confirmed:` line, in a Milestone if it belongs to a slice and otherwise loose in the Project.
4. When the last issue of a Milestone is Done, write one Slices record (Delivered, Evidence, Deviation, and an Issues list of every issue id with title and result) and set the Request to Done.
5. The Owner deletes finished Linear issues by hand and tells the agent first. Before any bulk delete, write the Slices record or an archive page so issue ids cited in the repo can still be resolved.
6. Never track progress in two places. If this repo also has its own record-keeping rules, follow them in addition.
7. Renaming the project: rename it in Linear (the id stays), rename the `Project` option in the Notion UI for both databases (the option id stays), update the `Project` and `Linear` lines above, and keep a `Formerly:` line for one version.
<!-- END SHARED-INTAKE -->
