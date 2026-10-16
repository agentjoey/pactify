# Pactify — Agent Context (GEMINI.md：antigravity/agy)

## ⭐ Session 启动（每次必执行）
```bash
git pull
cat .agent/CURRENT.md
```

## Project Overview
Pactify 是 **harness 路由器**：在 Claude Code 里用 slash 命令委派大任务给其他 coding agent harness。方案见 [`docs/specs/harness-subagents.md`](docs/specs/harness-subagents.md)，Phase 0 结论见 [`docs/phase0-report.md`](docs/phase0-report.md)。
现有 pact 协议、账本与看板冻结新投入，保留为可选的「治理模式」。
**Location:** ~/AgentWorks/Code_Claude/pactify
**GitHub:**   agentjoey/pactify
**Version:** **v0.12.0**（2026-10-15，当前发布版；编排器方向的回滚锚点）。未发布变更与完整历史见 **[`docs/CHANGELOG.md`](docs/CHANGELOG.md)**。

**当前状况（2026-10-16，基于当前 checkout）**
- **4 个 harness**：codex 为默认；kimi、agy、opencode 为实验性。agy 大任务一次完成 1/10，见 [`docs/agy-eval.md`](docs/agy-eval.md)；opencode（deepseek-flash）一次完成 6/10，见 [`docs/opencode-eval.md`](docs/opencode-eval.md)。
- **M1 功能已合入、未发布**：v0.13.0 挂起，Owner 选择继续内部自用；已支持后台 job、续接、取消、日志、worktree 隔离与 `sub doctor`。
- **任务查看**：Claude Code 新增 `/jobs`，CLI 新增本地只读面板 `pactify sub panel`；摘要契约见 [`docs/sub-status.md`](docs/sub-status.md)。
- **云端与 gemini-cli 清理**：云端 Go、web 与共享 workspace 及其构建 / 发布 CI 已删除（AJ-664）。gemini-cli 已移除（web #106、Go #107）。
- **本地闭环**：当前有 37 个 `internal` 包；本次 `go build ./...` 与隔离 `PACTIFY_HOME` 的 `go test ./...` 通过，bats 结果见 CHANGELOG。
- **治理模式冻结**：单一规范账本暂停在 WS-B（flag 默认关）；`[RUNGUARD-DEAD]` 与 `[UI-GATE]` 等既有问题不再排期。
- **仓库已公开**（2026-09-04，全历史凭据审查零命中）；**两个曾在对话里出现的 token 是否已轮换，仍待人工确认。**

**Technical docs:** [Architecture](docs/architecture.md) · [Deployment](docs/deployment.md) · [Operations](docs/operations.md)

## Tech Stack
| Layer | Tech |
|-------|------|
| CLI | **Go** 单静态二进制（cobra），`cmd/pactify` + 37 个 `internal` 包 |
| Dashboard | Vite + React + Tailwind v4（`web/`），构建产物经 `go:embed` 打进 `internal/serve/dist`（git 跟踪） |
| Protocol | Plain files (`.pact/`)：`log.jsonl` append-only 为源，`STATE.yml` 为投影 |
| Cloud | 已删除（AJ-664） |
| CI | GitHub Actions（`ci.yml` + `codex-schema.yml` + `release.yml`/goreleaser） |

## Key Implementation Details
- `.pact/` = 产品协议文件（用户 repo 使用）；`.agent/` = 本 repo 开发工作台（不是产品一部分）
- `log.jsonl` 为源，`STATE.yml` 为投影，CLI 重算；防多 agent 并发写 STATE 冲突
- worker 不能自标 `accepted`，只能置 `awaiting_review`；只有 reviewer 能转 `accepted`
- 拉取式派发：worker 启动时读 STATE.yml，人是"启动按钮"
- 画布工艺规约已随 Canvas 视图移除退役（2026-07-07 视图收敛：Canvas 删除、Live 并入 Board——见 docs/backlog.md [VIEWS]）；UI 改动合并门 = vitest + Playwright e2e 双绿保持不变

## Dev Commands
```bash
go build ./... && PACTIFY_HOME="$(mktemp -d)" go test ./...  # Go 全仓（编译 + 测试；internal 37 包）
PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/{pact,orchestrate,serve}/...
cd web && npx tsc --noEmit && npx vitest run   # 前端类型 + 单测
cd web && npx playwright test            # e2e（会起 e2e/mock-server.mjs）
bats tests/                              # CLI 端到端

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
<!-- pact:kinds: antigravity -->

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
