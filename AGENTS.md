# Pactify — Agent Context (AGENTS.md：opencode · codex-cli · codex-app · kimi-cli · cursor-cli)

## ⭐ Session 启动（每次必执行）
```bash
git pull
cat .agent/CURRENT.md
```

## Project Overview
多 agent 协同协议 + 薄 CLI。任何能读文件的 agent 均可参与；协议基于 git + repo，零外部依赖。  
协议核心：`.pact/` 文件契约；CLI 是协议的标准实现；各厂商入口是薄封装。
**Location:** ~/AgentWorks/Code_Claude/pactify
**GitHub:**   agentjoey/pactify
**Version:** **v0.11.0**（2026-08-24，tag 已发）。2026-09-04 起 `main` 领先 tag 两个 PR：**#48** `[FALLBACK-PAR]` 并行 run 的 fallback 提案闭环 + 版本历史拆出 CHANGELOG + CI 缓存/上限修复；**#49** checkpoint 的 in-place run 守卫（`[UI-GATE]` 止血）+ ACP 丢弃 model pin 的警告（`[ACP-MODEL]` 止血）+ codex 审批卡标题（`[CODEX-APPROVAL-NAME]`）。两者 CI 双绿后合入，**尚未发新 tag**。完整版本历史见 **[`docs/CHANGELOG.md`](docs/CHANGELOG.md)**；开着的条目见 `docs/backlog.md`（gitignored）。

**当前状况（2026-08-29 首次核实 · 2026-09-04 更新，全部逐条实测，非文档互证）**
- **本地闭环健康**：`go build ./...` + `go test ./...` 全绿（37 包）· vitest 669/669 · tsc clean。`main` 与 `origin/main` 同步、CI 绿、无未合 PR（#48/#49 已于 2026-09-04 合入）。本地分支只剩 `main` / `staging`。
- **⚠️ 云端整层离线**：`pactify-relay` 与 `pactify-relay-staging` 两个 fly app **都是 0 machines**，`/health` 连不上；`origin/production` 停在 2026-07-04（落后 main **652** 提交），`origin/staging` 落后 **224**。docs 站 pactify.dev 与 dashboard orx.pactify.dev（Vercel）本身正常。⇒ **hosted 模式当前不可用**，只有 local 模式（`pactify serve`）是活的。连带：v0.11.0 记的「relay 必须先于二进制部署」这条硬约束**至今未执行**——因整层没跑，眼下没有线上事故面，但**一旦拉起 relay，必须先部署 relay 再放二进制**，否则线上 `origin/production` 的 wire 枚举缺 `antigravity`，而 relay 走 throwing 的 `MachineInfo.parse`，一台 agy 机器就能打掉整个账户的机器列表。
- **⚠️ 产品级缺口 `[UI-GATE]`**（2026-08-29 用户实撞，已记 backlog）：账本滞后于现实时（agent 交付了但没走完协议流转），Board 只给 Accept/Changes 两个按钮，需要的「代 owner 补 checkpoint」没有入口，Human Owner 只能回命令行。同一次实撞还坐实：`Checkpoint` 的 `CommitAll`（`engine.go:975`）在**有并发 run 的仓库里本质不安全**——两次 checkpoint 之间 worker 写出的文件被一并提交，代码量归因失真。**2026-09-04（#49）已做止血**：`checkpoint` 加了 in-place run 守卫（`internal/runguard`，拒绝「有 live run 且它驱动的不是本任务」，run 自己的任务放行——因为 `brief.go:32` 要求每个 worker 自己 checkpoint；CLI 有 `--force` 逃生舱）。**Board 侧仍没有补 checkpoint 的入口**（serve 根本没有该端点），`CommitAll` 全树提交这个根因也没动。
- **架构债主线未动**：账本与 git 工作目录耦合（"单一规范 ledger" 重构）**已有 spec 草案** `docs/specs/single-canonical-ledger.md`（推荐方案：canonical 移到专用 git ref，工作树 `.pact/log.jsonl` 降级为导出产物；**Human Owner 2026-09-04 拍板：工作树文件不再 git tracked**）——尚未开工，WS-A（读写入口收敛，零行为变化）可先做；`[SANDBOX]` 只是定点修复，ledger 漂移无检测、tracked-`.pact` 恢复限制两条根因仍在。
- **⚠️ `[ACP-MODEL]` per-seat 模型绑定在 opencode 上 100% 失效**（2026-08-31 实撞并逐层坐实）：`opencode` 是**默认走 ACP** 的 kind（`DefaultTransportModes()`），而 ACP 路径根本不调 `agentcfg.ResolveSeat*`、`acpCommand` 也不传 `--model`——role binding 的模型钉被静默丢弃，实跑的是 opencode 全局默认。已用 opencode 自己的日志坐实（243 次 `modelID=deepseek-v4-pro`，绑定写的是 `minimax/MiniMax-M3`）。**这个静默失真直接导致 Human Owner 误判「worker 没干活」**。临时绕过 `--transport opencode=cmd`。**2026-09-04（#49）已做止血**：ACP 路径在 spawn 前检出 role binding 的 model pin 并打警告（`agentcfg.SeatModelPin` + `AcpRunner.Warn`）。**根治（让 ACP 真的传模型）仍未做**——第一步是实测 `opencode acp` 是否接受 `-m`。
- **✅ `[REPO-PRIVATE]` 已解决：仓库于 2026-09-04 公开**。翻开关前做过全历史泄露审查：扫了全部 1476 个提交的每一个 blob（12 类凭据模式），**零真实凭据**——命中项全是脱敏功能自身的测试样本与检测正则（`AKIAIOSFODNN7EXAMPLE` 等）；历史记录里那两个 token（npm / Vercel）**从未进过提交**，只存在于对话，公开不暴露它们（是否已轮换仍待人工确认）。顺带清掉两处：`.understand-anything/` 误提交的 209 文件 / 16MB 工具垃圾（#51，由一次 worker checkpoint 的 `CommitAll` 扫入）与 12 处本机绝对路径（#52，只洗当前树不重写历史，`pactify validate` 复验通过）。`go install` 与 plugin marketplace 两条安装路径已实测可用。
- **`[COMPETITOR]` getpaseo/paseo**：同赛道，15.7k star / Apache-2.0 / 五端客户端，功能重叠面很大但**没有**任务状态机与验收不变量（其 agent 生命周期只有 5 个纯进程状态）。详见 backlog `[COMPETITOR]` 与对比 artifact。战略取向需 Human Owner 决策，**agent 不要自行推进方向性改动**。

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
<!-- pact:kinds: opencode -->

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
