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
**Version:**  **v0.11.0**（2026-08-24）— backlog 全量核实后的清理批次（PR #47，14 个实质提交）。起点是对 `docs/backlog.md` 全部 113 个条目逐条**真实核实**（读代码/跑测试，非文本互证），据此排期实现；每条均过独立复核（新会话、不继承实现上下文），复核判 FAIL 的两处已定位修复。主线：**[SANDBOX] 默认沙箱可用**（tracked-`.pact` 仓里 seed 的账本是对 tracked 路径的未提交修改，checkout feature 分支必被 git 拒绝 → 此前只能退回有腐化风险的 `--in-place`；改为保账本的 checkout：快照→还原→checkout→union 回写，未 tracked 时逐字节走原路径）· **relay 广播 antigravity**（⚠️ 只改 Go 侧会造成线上事故：wire 枚举缺该值而 relay 走 throwing 的 `MachineInfo.parse`，一台 agy 机器能让整个账户机器列表 500；两侧同改 + conformance guard。**部署顺序硬约束：relay 必须先于二进制上线**）· **[KIND-2] `--seat-kind` 真的赢过 role binding**（并新增 `--roster-kind` 非显式渠道——独立复核揪出的跨进程回归：serve 派生的 kind 曾被当作显式覆盖，会让 role binding 对所有 dashboard/Dispatch/schedule/fallback 发起的 run 静默失效）· **[CODEX-MAP] codex cockpit 后端 7 条协议映射错误**（写下即错非漂移：MCP 工具调用从不产生事件、TOK 恒 0、`resume`/`interrupt` 发成通知从未生效、permissions 审批回复结构非法；真机抓包做 fixture）· **agy 审计接入**（`strings` 挖出原生 PreToolUse hook + 内嵌官方文档，机制与 gemini 血统完全不同）· **agy 会话清理**（无 CLI 且无法打标——`--project` 被接受但哪里都不落；改按 pactify 自己为 resume 记录的 `conversation_id` 做只删点名 id 的文件级清理，UUID 守卫承重：放宽后 id `..` 会 RemoveAll 整个数据目录）· **[CODEX-SCHEMA] 修好失效的预警**（本机恒红+CI 永远 skip，等于没有；新增不需装 codex 的 `TestSchemaContract` 常驻 CI + 周期拉最新版的 workflow，硬门=契约破坏、软信号=字节漂移）· **[DOCTOR] 退出码只由本项目用到的 kind 决定** · **[PROBE] 共享 entry 文件按 kind 精确归属**（并补掉 `pact.BakeEntry` 这条主路径）· **[REGISTER]/[REGISTRY-2] 自动注册跳过临时目录 + 未装机警告**。gate：go 全仓 + `-race` · bats · vitest · tsc · cloud workspace · CI 全绿。遗留（已记 backlog）：`[DIST-STALE]` main 上的 dist 本就陈旧（本机与 CI 各自重建产出同一新哈希，排除构建不确定性）· `[TEST-POLLUTION-2]` `acprunner_test.go` 写用户真实 audit 目录 · `[CODEX-APPROVAL-NAME]`/`[CODEX-DECISION-CANCEL]`。承接 v0.10.0（2026-08-23）— 自 v0.9.0 起 299 个实质提交的合并发布。主线：**phase-0 安全加固**（PR #36，3 critical + 3 high + M1 diff 脱敏 / M2 fs-browse 限域 / M8 cancel 守卫 / M9 merge 崩溃恢复 / M11 跨厂商凭据隔离 / M14 park 陈旧守卫 / M15 未回灌账本保全 / M16-17 fixRounds 重置+心跳）· **seat-identity 三段**（身份解析链 `.As()>env>.pact/seat` + `pactify seat` verb + 接线层去身份化 + 隔离 worktree 播种）· **role routing + failure policy**（PR #43-45：机器级 role profile + seat 绑定、按 seat 解析 kind/model、失败分 env/logic 两类、fallback 提案链 + `--approve-fallback`/`--reset-task`、planner 按 role 路由、dashboard 审批卡）· **execution tiering**（`tier: L0..L3` 经 spec 文件travel、不动冻结的 pact v1；`EffortForTier` + `budgetFor` 派生修复轮/返工/失败/critic/QA 预算，显式 flag 经 `Flags().Changed()` 优先；前端 tier 徽标三态 NO TIER/L0-L3/冲突）· **antigravity(agy) 接入**（GUI 座席改造为可 headless 驱动的 CLI kind：`--add-dir` 工作区锚定、`--conversation` 续接、单对象 JSON 的 head+tail 双窗口解析、status 门控会话记录、刻意不声明 `EffortArgs`——`--effort` 与模型名后缀冲突会 exit 1，档位改用带后缀的模型名）· **auto-register + registry 热重载**（PR #39/#41）· **ACP npx 握手根修**（PR #42，bridge 直调 bin —— 此前 `npx -y` 从不回 initialize，ACP 实际从未握手成功）· **RELAY-2**（relay 不可用不再阻塞 serve 启动）· **e2e 五连修**（PR #37）· **sandbox base 来源统一**（PR #40）· **测试污染根治**（`internal/testenv` + 7 个 TestMain——被编排 agent 跑 Go 测试曾把真账本从 622 条清成 1 条并覆写 CLAUDE.md/AGENTS.md/PROJECT.md，两次伪装成「worker 越界交付」误导评审）· product UI v2 / Flow 视图 / hosted cockpit / ACCT-A1。gate：go test 全仓（`internal/cockpit/codexschema` 的 `TestSchemaDrift` 红属既有——vendored schema 停在 codex 0.142.5，该包零 Go 消费方，仅为未开工的 DEEP-INT 留的参考件）· `-race`（pact/orchestrate/serve）· bats 114/114 · `-tags agye2e` 真跑 agy e2e。承接 review-runtime（包2，feat-review-runtime，2026-07-05）— 评审深化 + 运行时 8 任务:**fix-until-green 自修环**(checkpoint 后评审前跑 verify 门,红则同 worker fix 轮 default 2 不计 MaxFails,board 显示修复中 n/2)、**quorum 多评审**(`--reviewers/--quorum`,投影按本轮不同 reviewer accept 数判定,单 reviewer 字节不变,两不变量神圣)、**critic 预评**(CRITIC_SCORE 注入 reviewer 无门控权)、**QA 门**(`qa:` 行真跑软件 QA_RESULT,FAIL 走共享 fix 预算,宽松)、**账本持久快照**(`.pact/state-snapshot.json` 增量 replay,5k **11.5x**,损坏静默全量回退)、**动态座席**(`join --kind` + driver 每轮实时读 seat→kind + planner 自动 staff)、**定时 run**(`pactify schedule` daily/every,serve ticker)。gate:go 全仓+`-race` · vitest 521 · tsc · bats 111。**混合模式自驱交付**(承 P4/P5 后:pactify 账本协调 + subagent 执行 + orchestrator 亲做 pact/git,每棒 checkpoint 归属 owner 座席 + 即时提交)。（follow-up "validate 只认 init 座席" 已过时并于 2026-07-19 移除：validate 名册来自 log 投影，dynamic join 座席会入册，跨厂商强化测试 P5 实测 valid。）承接 **driver-modernization**（包1，feat-driver-modernization，2026-07-05）— orchestrate 驱动层现代化 9 任务:**ACP transport**(`internal/acp` Go JSON-RPC/stdio 客户端 + `orchestrate.AcpRunner` + `RoutedLocalRunner` 按 kind 选传输,`--transport kind=acp`,默认全 cmd 零行为变化;kimi=`kimi acp`、claude/codex/gemini 经 npx bridge;permission 三档 auto/escalate/deny)、**ACP usage→token store**(修 TOK=0 旧债,`session/update` usage 落 `internal/tokens`)、**session 续接**(`.pact/orchestrate/sessions.json` lockx,支持 loadSession 则续接省 token)、**知识层**(`.pact/memory.md` + `.pact/skills/*.md` role/keyword 注入 briefing,无文件字节不变)、**doctor per-vendor 预检**(binary+auth+ACP 可用性,serve 启动非阻断)、**per-seat 可靠度**(`AgentStat.Accepted/Reworked` fold + OfficeView 徽标)。gate:go 全仓+`-race`绿 · vitest 521 · tsc clean · bats 111。**dogfood 交付方式**:先经真 orchestrate 自驱(dm-acp-core/runner 高质量交付),后因 orchestrate `--in-place` 的 worker-对协议文件-git-surgery 腐化(P4)+ sandbox `total=0`(P5)切**混合模式**(pactify 账本做协调事实源 + 受控 subagent 执行 + orchestrator 亲做 pact/git),问题全记 dogfood 报告。承接 **v0.9.0**（全仓 review 驱动的三轮加固：ledger 并发/崩溃安全 + 协议语义修正 + 可观测性接线，✅ 已发布 tag v0.9.0）— 2026-07-02 对全仓做了一次 8 路并行 code review(`.agent/reviews/code-review-2026-07-02.md`),按 P0/P1 分三轮修复,commit `d8bd6bd`(RUN 时长改由 `assign` 事件推导,不再读不存在的 `in_progress` 事件)→`aff6509`(sandbox run 加**mid-run ledger mirror**——每轮迭代把 sandbox ledger union 回主目录,board 不再冻结到 run 结束;新增**任务级 `start` 事件**,worker 未走 `pactify join` 时也能让 board 显示 in_progress,spec §2.6 记录)→`5f10ba2`(第一轮 7 个 P0:driver 清洁退出救援补线、并行 drain 三分支处理、serve 项目重命名后 SSE 换键、双击 New task 竞态锁+子进程收尸、session 清理 tag 定界符防跨座席误删、Board Accept/Changes 补错误处理)→`595bd3d`(第二轮并发/崩溃安全:全 13 个 pact verb 统一 `withLedgerLock` 跨进程锁、`Checkpoint` 写序翻转为先 commit 后记事件、`GateConfig` fail-closed、orchestrator 角色门、Assign 时 slug/ref 校验;orchestrate sandbox 的 `writeLedger` 加锁+原子写、park 分支身份持久化+拒跑陈旧 park、teardown 走 defer 兜 panic)→`46e733e`(第三轮:serve 全量 fold 加 memo、worktree 视图补 events 端点让指标不再归零、live agent 流按 `Last-Event-ID` 续传防重连重复、orchestrate History 持久化跨重启存活、`join` 只抬座席第一个可动工任务(不再污染整批)、base 锁改走 `GitCommonPath`(修跨 worktree merge 不互斥)、cancelled 依赖不再死锁依赖者、TOK 指标接上真实 `/stats`(删掉读不存在 payload 的死机制)、audit 脱敏扩展、Plan-mode 死代码清理)。**同期产出**统一架构方案 `.agent/plans/unified-architecture-2026-07-03.md`(分析 linx 的 relay/认证/账户架构,规划 pactify 接入同一套 relay+账户体系的 U0–U4 路线,relay.go 空壳与 derive 纯函数层已按此方向保持薄/可复用)。gate:go test 25 包 + `-race`(pact/orchestrate/serve)绿 · vitest 450 · tsc clean · bats e2e 111/111。承接 v0.8.2（dashboard 隐藏 pact 内部 worktree，PR #31, tag v0.8.2）— `serve` 的 worktree 列表(`gitWorktrees`/`filterInternalWorktrees`)过滤掉 pact 自己的 run worktree(`.pact/orchestrate/` 下的 sandbox/parallel 树 + `pact-*-park` 分支),不再把它们当成可选项目板抛给 dashboard——消除「同一任务在不同 tree 下状态不一致」(run 中 worker 隔离 worktree 的 live `.pact` vs 被 park 的主树 seed,因 `.pact` gitignored 每树各一份)。primary(被 park 的主树)永远保留为规范视图;`resolveWorktreePath` 共用过滤,内部板也不可经 `state?wt=` 寻址。这是 option A(对症);彻底解=单一规范 ledger(ledger 与 git 工作目录解耦)留作后续重构。承接 v0.8.1（worker 投递完整性补丁，PR #29–#30, tag v0.8.1）— 修 worker 在隔离 worktree 下的「投递丢失」：**#29** runner 给 worker 注**绝对 `PACT_DIR=<RepoDir>/.pact`** + `pact.At(".")` 的 git 工作目录随绝对 `PACT_DIR` 取 `dirname`,把 worker 的 checkpoint 钉到 driver 的 worktree(此前 opencode 项目级 MCP 独立解析项目根 → checkpoint 落到岔开的 ledger/分支、worktree 销毁即丢 → driver 报「干完活但看不到、分支空、failure limit」);**#30** 回灌 `writeLedger` 按 `event_id` **union 合并**(不再整文件覆盖、不冲掉并发写)+ escalation **归因**(worker 干净退出却没 checkpoint 时明说「无提交落盘」,不再裸 failure limit)。承接 v0.8.0（coordination-authority + dark product UI，PR #24–#28, tag v0.8.0）— 把「协调权威」从隐式（进程 cwd / 裸 git ref）升格为**显式·可寻址·可加锁的资源**(spec `docs/specs/coordination-authority.md`)：**#24** runtime ignore 走 `.git/info/exclude`(零提交) + autonomous **默认 sandbox**(`--in-place` opt-out；status/streams/escalation 镜像主目录,dashboard 仍见 live) + 每个 `merge` 持**跨进程/worktree flock 锁**(`internal/lockx`) + MCP 每工具加 `project` 入参 + `projects` 工具(一个 session 按名驱动任意注册项目,解开 cwd 绑定)；**#25 base 写入契约**——只有 `merge` 写 base、merge **fetch-aware**(`fetch origin`→base ff 到 `origin/base`→feature rebase 其上,失败即报错,绝不分叉)、默认不 push(`--push` opt-in)、`accept` 不连带 merge、`checkpoint` 在 base 上而 feature 声明了分支即报错；**#26** per-project 硬门 `config gate`(类型默认 pnpm/npm/cargo/go,优先级 task `verify:` > config gate > 默认)；**#27** 空 feature 分支拒绝 ship(杜绝 phantom)；**#28** 机器提交一律 `--no-verify`(`CommitAll`/`CommitPaths`/`MergeNoFF` 不跑 commitlint/lint-staged——linx phantom 真因=pre-commit 钩子拒机器提交)。**dogfood=linx**(pnpm SvelteKit 应用)端到端揪出并修。gate：go test 25 包 · go vet · CI test/e2e/site 绿。**同版一并发布 dark product UI refresh** — dashboard 从浅色切到**深色**，照 `design_handoff_dark_product_ui` 设计稿重制全部 6 屏。本质是 token swap（`tokens.css` `@theme` 改深色，role 三色回归 brand.md 一直定义的深色基底，≈70% 经 `var()` 自动迁移）+ `index.css` 硬编码浅色值 re-tone + 少量新 pattern：`ui/MetricStrip`（mono `RUN/TOK/×iter` 统计条）；**Board** 全宽 5 列（Assigned/Working/Review/Accepted/Shipped）+ 上下文头条（feature chip + 座席簇 + New task）+ 卡片统计条 + Review 列内联 Accept/Changes，去固定左 dock，accepted+shipped 折叠到最近 6；**Canvas** 底部 NL 命令坞；**Live** 两栏 + 右侧事件流终端（`#07090d`）；**Setup** 旅程步进器 + 角色切换 pill；**Settings** scope 双栏（Project/Machine/Account）。附带修 stats：`WithLOC`→`WithTaskLOC`，按各任务 checkpoint commit 精确归因代码量（旧逻辑同分支每任务显示相同分支总量）。gate：vitest 438 · tsc clean · build green · 6 屏 playwright 实拍比对设计稿。**用 Pactify 自身 dogfood 产出**（kimi=worker 跑 t1–t9，配额耗尽后 claude 接管 t5/t6/t7；claude=reviewer 逐任务视觉评审）。承接 v0.7.3（Dispatch/orchestrate 健壮性，dogfood 揪出的 3 个 gap）— ①orchestrate run 守卫加 stale 判定（旧 run `done:false` 不再永久堵新 run，按 updated_at>10min 或不可解析视为 dead）；②serve 触发的 run 改 `resolveSeatKinds`（init→roster Kind→按座席名对 `agent.Kinds()` 启发式，opencode-worker→opencode 等，修「座席没记 kind 跑不动」）；③planner prompt 加「verify 范围化」规范（禁全仓 `eslint .` 之类会撞范围外历史债的门）。承接 v0.7.2（删 Canvas Plan mode）— Canvas 的 Plan 页面（拖拽/连线拆任务图）在有了 Dispatch 面板 + 只读 PlanDock 后冗余，整块移除：mode 切换 + TaskNode/SeatNode/FeatureGroup/ConnectionLine/ConnectingFlag 节点组件 + canvas.ts/comms.ts 的 Plan-only exports + canvas e2e spec/switchToPlan helper + CheatSheet plan 手势行；Canvas Office 视图与其余不动。**本版由 Dispatch dogfood 端到端产出**（opencode/kimi/gemini worker + claude 评审/e2e 收尾，vitest 398 + e2e 5 双绿）。承接 v0.7.1（Dispatch planner 错误透出）— `defaultRunPlanner` 改 `CombinedOutput()`，把 planner 子进程输出尾部带进 plan-gen error（不再只剩 "exit status 1"，能看到 "exec: claude: not found" 之类真实原因）。⚠️ 部署：launchd serve 的 plist PATH 必须含 `~/.local/bin`+`~/.opencode/bin`（claude/kimi/gemini/opencode 二进制位置），否则 Dispatch/orchestrate 起不了 agent。承接 v0.7.0（Sprint2 端到端下发 + worktree-aware 项目）— **Dispatch Panel**：dashboard 从「只能看」→「一句话下发」，右侧滑出面板 goal→后台拉起 planner agent 生成任务图→审阅→确认 = apply+orchestrate run（后端 `POST/GET .../plan/generate[/status]` claude 写；前端 DispatchPanel kimi 经 pact orchestrate dogfood 写，A/B worktree 隔离并行）。**worktree-aware 项目**：`GET .../worktrees` + `state?wt=`，ProjectMenu 项目下缩进列出 git worktree、可切换查看各 worktree 的 board（非主轮询、主 SSE）。承接 v0.6.1（IA v2 收尾 + UI 修正）— RosterDock 重设计为精致身份列表（按角色分组的 logo+名+角色标签，两张悬浮卡居中偏上、Board-only 避开 Canvas/Office 工具栏）、项目下拉每行状态灯、Board 左 gutter 防遮挡、accepted 列显示最近 10 张折叠其余；#22 清理（删 Sidebar/PlanReview/TopBar 死代码、View/CableMark 迁出、RosterDock 齿轮定位座席 Settings）。承接 v0.6.0（IA v2 dashboard 信息架构重做）— header 项目下拉（状态灯+改名+projwiz）、悬浮 RosterDock（磨砂卡片，orchestrator 第一）、Settings modal（整合 ops+setup）、视图 7→3（Board/Canvas/Live，默认 Board，快捷键 1/2/3）、PlanDock 只读悬浮窗、Recipes 进 ⌘K、Board accepted 列折叠/分组/限渲染、删 ReplayBar+`?at=` 回放、删 Sidebar；后端 `PUT /api/registry/{name}` 改名 + SeatDTO 透出 kind + planner kebab-slug 命名校验/prompt；承接 v0.5.2：SSE 反代健壮性修复；v0.5.1：orchestrate per-task token 捕获；v0.5.0：custom-agent manifest API；v0.4.0：orchestrate 自主驱动 + planner + 浅色 dashboard + native audit layer + pactify.dev 文档站

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
- 画布工艺规约已随 Canvas 视图移除退役（2026-07-07 视图收敛：Canvas 删除、Live 并入 Board——见 docs/backlog.md [VIEWS]）；UI 改动合并门 = vitest + Playwright e2e 双绿保持不变

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
- **视觉门**：UI 改动提交前必须 playwright 截图实测（`node web/scripts/shots.mjs [view]`；escalated/review-gate 等无法按需触发的态用 `live-gate-shot.mjs` 注入 mock）——vitest/tsc 绿 ≠ 视觉对。要证明截图来自**最终 build**（而非长驻 daemon 可能提供的陈旧 dist）用 `node web/scripts/shot-dispatch-review.mjs`——它 spawn hermetic 的 `e2e/mock-server.mjs`，直接服务 `internal/serve/dist`。

<!-- pact:begin (managed by pactify — edit outside this block) -->
<!-- pact:kinds: claude-code -->

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
