# Pactify — Claude Code Context

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
**Version:**  v0.8.2（dashboard 隐藏 pact 内部 worktree，✅ 已发布 PR #31, tag v0.8.2）— `serve` 的 worktree 列表(`gitWorktrees`/`filterInternalWorktrees`)过滤掉 pact 自己的 run worktree(`.pact/orchestrate/` 下的 sandbox/parallel 树 + `pact-*-park` 分支),不再把它们当成可选项目板抛给 dashboard——消除「同一任务在不同 tree 下状态不一致」(run 中 worker 隔离 worktree 的 live `.pact` vs 被 park 的主树 seed,因 `.pact` gitignored 每树各一份)。primary(被 park 的主树)永远保留为规范视图;`resolveWorktreePath` 共用过滤,内部板也不可经 `state?wt=` 寻址。这是 option A(对症);彻底解=单一规范 ledger(ledger 与 git 工作目录解耦)留作后续重构。承接 v0.8.1（worker 投递完整性补丁，PR #29–#30, tag v0.8.1）— 修 worker 在隔离 worktree 下的「投递丢失」：**#29** runner 给 worker 注**绝对 `PACT_DIR=<RepoDir>/.pact`** + `pact.At(".")` 的 git 工作目录随绝对 `PACT_DIR` 取 `dirname`,把 worker 的 checkpoint 钉到 driver 的 worktree(此前 opencode 项目级 MCP 独立解析项目根 → checkpoint 落到岔开的 ledger/分支、worktree 销毁即丢 → driver 报「干完活但看不到、分支空、failure limit」);**#30** 回灌 `writeLedger` 按 `event_id` **union 合并**(不再整文件覆盖、不冲掉并发写)+ escalation **归因**(worker 干净退出却没 checkpoint 时明说「无提交落盘」,不再裸 failure limit)。承接 v0.8.0（coordination-authority + dark product UI，PR #24–#28, tag v0.8.0）— 把「协调权威」从隐式（进程 cwd / 裸 git ref）升格为**显式·可寻址·可加锁的资源**(spec `docs/specs/coordination-authority.md`)：**#24** runtime ignore 走 `.git/info/exclude`(零提交) + autonomous **默认 sandbox**(`--in-place` opt-out；status/streams/escalation 镜像主目录,dashboard 仍见 live) + 每个 `merge` 持**跨进程/worktree flock 锁**(`internal/lockx`) + MCP 每工具加 `project` 入参 + `projects` 工具(一个 session 按名驱动任意注册项目,解开 cwd 绑定)；**#25 base 写入契约**——只有 `merge` 写 base、merge **fetch-aware**(`fetch origin`→base ff 到 `origin/base`→feature rebase 其上,失败即报错,绝不分叉)、默认不 push(`--push` opt-in)、`accept` 不连带 merge、`checkpoint` 在 base 上而 feature 声明了分支即报错；**#26** per-project 硬门 `config gate`(类型默认 pnpm/npm/cargo/go,优先级 task `verify:` > config gate > 默认)；**#27** 空 feature 分支拒绝 ship(杜绝 phantom)；**#28** 机器提交一律 `--no-verify`(`CommitAll`/`CommitPaths`/`MergeNoFF` 不跑 commitlint/lint-staged——linx phantom 真因=pre-commit 钩子拒机器提交)。**dogfood=linx**(pnpm SvelteKit 应用)端到端揪出并修。gate：go test 25 包 · go vet · CI test/e2e/site 绿。**同版一并发布 dark product UI refresh** — dashboard 从浅色切到**深色**，照 `design_handoff_dark_product_ui` 设计稿重制全部 6 屏。本质是 token swap（`tokens.css` `@theme` 改深色，role 三色回归 brand.md 一直定义的深色基底，≈70% 经 `var()` 自动迁移）+ `index.css` 硬编码浅色值 re-tone + 少量新 pattern：`ui/MetricStrip`（mono `RUN/TOK/×iter` 统计条）；**Board** 全宽 5 列（Assigned/Working/Review/Accepted/Shipped）+ 上下文头条（feature chip + 座席簇 + New task）+ 卡片统计条 + Review 列内联 Accept/Changes，去固定左 dock，accepted+shipped 折叠到最近 6；**Canvas** 底部 NL 命令坞；**Live** 两栏 + 右侧事件流终端（`#07090d`）；**Setup** 旅程步进器 + 角色切换 pill；**Settings** scope 双栏（Project/Machine/Account）。附带修 stats：`WithLOC`→`WithTaskLOC`，按各任务 checkpoint commit 精确归因代码量（旧逻辑同分支每任务显示相同分支总量）。gate：vitest 438 · tsc clean · build green · 6 屏 playwright 实拍比对设计稿。**用 Pactify 自身 dogfood 产出**（kimi=worker 跑 t1–t9，配额耗尽后 claude 接管 t5/t6/t7；claude=reviewer 逐任务视觉评审）。承接 v0.7.3（Dispatch/orchestrate 健壮性，dogfood 揪出的 3 个 gap）— ①orchestrate run 守卫加 stale 判定（旧 run `done:false` 不再永久堵新 run，按 updated_at>10min 或不可解析视为 dead）；②serve 触发的 run 改 `resolveSeatKinds`（init→roster Kind→按座席名对 `agent.Kinds()` 启发式，opencode-worker→opencode 等，修「座席没记 kind 跑不动」）；③planner prompt 加「verify 范围化」规范（禁全仓 `eslint .` 之类会撞范围外历史债的门）。承接 v0.7.2（删 Canvas Plan mode）— Canvas 的 Plan 页面（拖拽/连线拆任务图）在有了 Dispatch 面板 + 只读 PlanDock 后冗余，整块移除：mode 切换 + TaskNode/SeatNode/FeatureGroup/ConnectionLine/ConnectingFlag 节点组件 + canvas.ts/comms.ts 的 Plan-only exports + canvas e2e spec/switchToPlan helper + CheatSheet plan 手势行；Canvas Office 视图与其余不动。**本版由 Dispatch dogfood 端到端产出**（opencode/kimi/gemini worker + claude 评审/e2e 收尾，vitest 398 + e2e 5 双绿）。承接 v0.7.1（Dispatch planner 错误透出）— `defaultRunPlanner` 改 `CombinedOutput()`，把 planner 子进程输出尾部带进 plan-gen error（不再只剩 "exit status 1"，能看到 "exec: claude: not found" 之类真实原因）。⚠️ 部署：launchd serve 的 plist PATH 必须含 `~/.local/bin`+`~/.opencode/bin`（claude/kimi/gemini/opencode 二进制位置），否则 Dispatch/orchestrate 起不了 agent。承接 v0.7.0（Sprint2 端到端下发 + worktree-aware 项目）— **Dispatch Panel**：dashboard 从「只能看」→「一句话下发」，右侧滑出面板 goal→后台拉起 planner agent 生成任务图→审阅→确认 = apply+orchestrate run（后端 `POST/GET .../plan/generate[/status]` claude 写；前端 DispatchPanel kimi 经 pact orchestrate dogfood 写，A/B worktree 隔离并行）。**worktree-aware 项目**：`GET .../worktrees` + `state?wt=`，ProjectMenu 项目下缩进列出 git worktree、可切换查看各 worktree 的 board（非主轮询、主 SSE）。承接 v0.6.1（IA v2 收尾 + UI 修正）— RosterDock 重设计为精致身份列表（按角色分组的 logo+名+角色标签，两张悬浮卡居中偏上、Board-only 避开 Canvas/Office 工具栏）、项目下拉每行状态灯、Board 左 gutter 防遮挡、accepted 列显示最近 10 张折叠其余；#22 清理（删 Sidebar/PlanReview/TopBar 死代码、View/CableMark 迁出、RosterDock 齿轮定位座席 Settings）。承接 v0.6.0（IA v2 dashboard 信息架构重做）— header 项目下拉（状态灯+改名+projwiz）、悬浮 RosterDock（磨砂卡片，orchestrator 第一）、Settings modal（整合 ops+setup）、视图 7→3（Board/Canvas/Live，默认 Board，快捷键 1/2/3）、PlanDock 只读悬浮窗、Recipes 进 ⌘K、Board accepted 列折叠/分组/限渲染、删 ReplayBar+`?at=` 回放、删 Sidebar；后端 `PUT /api/registry/{name}` 改名 + SeatDTO 透出 kind + planner kebab-slug 命名校验/prompt；承接 v0.5.2：SSE 反代健壮性修复；v0.5.1：orchestrate per-task token 捕获；v0.5.0：custom-agent manifest API；v0.4.0：orchestrate 自主驱动 + planner + 浅色 dashboard + native audit layer + pactify.dev 文档站

**Technical docs:** [Architecture](docs/architecture.md) · [Deployment](docs/deployment.md) · [Operations](docs/operations.md)

## Tech Stack
| Layer | Tech |
|-------|------|
| CLI | TBD (Go 单静态二进制 / Node) |
| Protocol | Plain files (.pact/) |
| CI | GitHub Actions |

## Key Implementation Details
- `.pact/` = 产品协议文件（用户 repo 使用）；`.agent/` = 本 repo 开发工作台（不是产品一部分）
- `log.jsonl` 为源，`STATE.yml` 为投影，CLI 重算；防多 agent 并发写 STATE 冲突
- worker 不能自标 `accepted`，只能置 `awaiting_review`；只有 reviewer 能转 `accepted`
- 拉取式派发：worker 启动时读 STATE.yml，人是"启动按钮"
- 画布工艺规约（spec 2026-06-12 §5）：节点位置只有两个写入者（placeNew 首现一次 + 用户拖拽），禁止渲染时算位置；RF 节点数组只走 merge-by-id；生产代码禁止伪造 RF 几何（measured/handles）；画布 PR 合并门 = vitest + Playwright e2e 双绿

## Dev Commands
```bash
# CLI 语言未定，Sprint 001 T1 决策后补充
```

## Release 后必做（任何 agent 通用）
1. .agent/CURRENT.md：补充 Version History 描述
2. 更新 Current Sprint Summary
3. 如有架构变更：更新 docs/architecture.md
4. squash-to-main 时强制过一遍 CLAUDE.md 的 Version 行 + docs/architecture.md 状态行（2026-06-19 复盘：两者曾分别滞后到 v0.3/v0.4，与 main 脱节）

## 工程约定（2026-06-19 dark-ui 复盘）
- **已知妥协必登记**：在代码注释里写下 later/TODO/「精确做法需…」之类的妥协时，**同步在 docs/backlog.md 加一行**，否则债隐形——`internal/stats` 的 per-task LOC bug（每任务都显示整分支 +842/−307）正是 `WithLOC` 注释自承认「精确归因需 commit SHA」却搁置成债，直到用户肉眼发现才修。
- **前端改动看效果走 dev proxy**：`cd web && npm run dev` 经 vite proxy 直连常驻 serve（PACTIFY_SERVE_URL，默认 :17082）热重载；只有最终验收 / 要 live 才 `npm run build` + 重建二进制 + `launchctl kickstart`。
- **视觉门**：UI 改动提交前必须 playwright 截图实测（`node web/scripts/shots.mjs [view]`；escalated/review-gate 等无法按需触发的态用 `live-gate-shot.mjs` 注入 mock）——vitest/tsc 绿 ≠ 视觉对。

<!-- pact:begin (managed by pactify — edit outside this block) -->
# pact protocol — seat `claude`

This repo uses the **pact protocol** (v1). You are seat `claude`, roles: orchestrator,reviewer.

**Primary — MCP:** the `pact` MCP server is wired into your config. Use its tools
(status / join / assign / checkpoint / accept / changes / merge / list) and resources
(`pact://state`, `pact://log`). Cold start: call `status`, then `join`
(registers your seat and checks out your feature branch).

**Fallback — shell** (if MCP is unavailable):
```bash
export PACT_AGENT_ID=claude
pactify join claude --roles orchestrator,reviewer
```
then `pactify help` for the verbs.

**The two rules:** a worker cannot self-accept (only the task's reviewer accepts); a
feature cannot merge until all its tasks are accepted.
<!-- pact:end -->
