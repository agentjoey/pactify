# Spec — Review & Runtime Deepening(评审深化 + 运行时增强,包2)

> Status: APPROVED-FOR-BUILD · 2026-07-05
> 前置:包1 `docs/specs/driver-modernization.md`(ACP/知识层/doctor/统计)已合并。
> 本包 = **第2梯队(C-24 自修环 + C-3 quorum 评审 + C-23 critic 预评 + C-25 QA 门)+ 第3梯队剩余(C-4 账本快照 + C-19 动态座席 + C-21 定时 run)**。
> 交付方式:pactify 自驱长跑(承接包1 dogfood);worker = claude-worker / kimi-worker / opencode-worker,reviewer = claude。

## 0. 包目标与不变量

**主题**:把「评审即协议」做深(worker 自愈、多评审、预评分、真跑验证),同时补运行时三块地基(投影快照、动态座席、定时驱动)。

- **I-1 协议向后兼容**:所有新语义 opt-in;不带新配置的既有项目行为逐字节不变。单 reviewer 仍是默认;quorum/critic/QA 都是任务级/项目级显式开启。
- **I-2 两不变量神圣**:worker 不能自我 accept;merge 门全任务 accepted——任何新机制(自修环/critic)只能**收紧**进入评审的质量,绝不绕过或代行 accept。
- **I-3 账本单一事实源**:新状态(fix 轮次、critic 分、quorum 计数)全部从既有/新增账本事件推导,不引入旁路状态文件(orchestrate runtime 文件除外,一如既往)。

## 1. WS-F — fix-until-green 自修环 + draft 语义(C-24)

**问题**:现在 worker checkpoint → `awaiting_review` → 门/评审失败 → `changes` → 重跑,每次失败烧一个完整评审回合。
**设计**:驱动器在 **worker checkpoint 之后、启动 reviewer 之前**插一个「预门循环」:

1. worker 棒结束且有 checkpoint → driver 立即跑该任务的 verify 门(task `verify:` > config gate,与现有 gate 解析同源)。
2. **红** → 不进评审:对同一 worker 发「fix 轮」briefing(含 verify 输出尾部 2KB + 「上一轮你已 checkpoint,现在修复直到门绿」),重跑。轮数上限 `--max-fix-rounds`(默认 2);fix 轮**不计** MaxFails/rework(它是棒内自愈,不是失败)。
3. **绿**(或轮次耗尽)→ 现有路径:轮次耗尽走现有 changes/escalation 语义(reason 带最后一次 verify 输出)。
4. **draft 语义**:fix 循环期间任务在投影上仍显示 `awaiting_review`(协议事件不动——checkpoint 已发生),但 driver 的 runtime status 标 `fixing`,board 经 status 端点可见「修复中 n/2」。**不加新 event_type**(I-3:轮次从「同一任务连续 checkpoint 且无 accept/changes 间隔」推导即可,driver 内存计数)。

**落点**:`internal/orchestrate/loop.go` decide 流(gate 执行已有,移动/复用其调用点);flag 进 Options。
**测试**:fake runner + 门命令注入:红→fix→绿→评审、轮次耗尽→escalate、门绿直通(现状不变 golden)、fix 轮不烧 MaxFails。

## 2. WS-G — quorum 评审(C-3)

**设计**(协议扩展,严格 opt-in):
- `assign` 新增 `--reviewers a,b,c --quorum 2`(与现有 `--reviewer` 互斥;单数路径完全不动)。账本 assign payload 加 `reviewers[]` + `quorum`(缺省=旧格式,投影兼容读)。
- **投影规则**:任务 `awaiting_review` 后,统计**该次 checkpoint 之后**的 accept 事件的不同 reviewer 座席数 ≥ quorum → `accepted`。任一 reviewer 的 `changes` → 立即 `rework`(**清零**本轮 accepts——重新 checkpoint 后所有 reviewer 重新表态)。
- **引擎守卫**:accept 时校验 acting seat ∈ reviewers(现有单 reviewer 校验的推广);worker 自 accept 依旧拒绝(I-2)。
- **驱动**:reviewer 棒按 reviewers 顺序逐个跑(in-place 串行),已 accept 的跳过;凑齐 quorum 提前停。
- **UI 兼容**:STATE.yml/DTO 的 `reviewer` 字段保留(填第一个 reviewer),新增 `reviewers`/`quorum`/`accepts` 字段;board 卡片显示 `2/3 ✓`(web 小改,进 rp-e2e-docs 一起)。

**落点**:`internal/pact/engine.go`(assign/accept 校验)、`internal/projection`(fold 规则)、`internal/orchestrate/loop.go`(多 reviewer 驱动)、cmd assign flags。
**测试**:fold 用例(2/3 凑齐、changes 清零、重 checkpoint 重计)、引擎拒绝(非 reviewer accept、worker 自 accept)、驱动顺序与提前停、旧格式账本回读不变(golden)。

## 3. WS-H — critic 预评分(C-23)

**设计**(默认关,项目级开启):
- `.pact/config` 加 `critic: <seat>`(记账本 config 事件,同 gate 配置机制);orchestrate flag `--critic seat=<seat>` 覆盖。
- 时机:worker checkpoint 且 **verify 门绿之后**(WS-F 之后接力)、reviewer 之前:driver 发一个 critic 棒(该 seat 的 kind 照常解析),briefing = 「只读评审:按 spec 检查 diff,末行输出 `CRITIC_SCORE: 0.0-1.0` + 一句理由」。
- driver 解析末行分数 → 写入任务级 note 事件(payload `{critic_score, critic_by, reason}`,**复用现有 note/start 类事件机制,不加 event_type**)→ **注入 reviewer briefing**(「critic 预评 0.4:<理由>——请重点核查」)。
- 分数 **无门控权**(I-2):低分不自动打回(那是 verify 门的事);只影响 reviewer 的注意力。解析失败(无标记行)→ 记 note `critic_score: null`,流程照走,绝不阻塞。
- critic 棒超时/失败 = 跳过(软),不重试不升级。

**落点**:loop.go(插棒时机)、brief.go(reviewer 注入)、config 读取。
**测试**:分数解析(正常/缺失/越界钳制)、注入内容、critic 失败不阻塞、默认关=零行为变化。

## 4. WS-I — QA-agent 门(C-25)

**设计**(实验性,任务级 opt-in):
- 任务 spec 里声明 `qa: <一句要验证什么>`(与 `verify:` 同格式解析,可共存;无 `qa:` = 现状)。
- 时机:verify 门绿之后(与 critic 并列,先 QA 后 critic;都开则串行)。driver 发 QA 棒:briefing = `.pact/skills/qa-guide.md`(有则,包1 知识层管道复用)+ 任务上下文 + 「把软件真的跑起来验证 <hint>;报告写到 `.pact/orchestrate/qa-<task>.md`;末行输出 `QA_RESULT: PASS|FAIL: <一句>`」。
- `FAIL` → 等价 verify 红:进 WS-F fix 轮(轮次共享上限);`PASS`/解析失败 → 继续(解析失败记 note,宽松——实验性功能失败不能卡主流程)。
- QA 报告文件路径写进 reviewer briefing(「QA 报告见 …」)。

**落点**:任务 spec 解析处(找现有 `verify:` 行解析)、loop.go。
**测试**:PASS/FAIL/无标记三分支、FAIL 进 fix 轮、无 `qa:` 零变化。

## 5. WS-J — 账本持久快照 + 增量 replay(C-4)

**设计**:
- `.pact/state-snapshot.json`:`{version: 1, ledger_bytes: <已fold字节偏移>, ledger_head_hash: <前N字节内容hash>, state: <投影JSON>}`。
- **读**:fold 入口(CLI verbs + serve 冷启动)先读快照:version 匹配 && `ledger_bytes ≤ 当前文件大小` && 头部 hash 匹配(防重写/截断)→ 从偏移起只 fold 新行;任何不满足 → 全量 fold(静默,快照只是缓存)。
- **写**:每次 verb 成功后(withLedgerLock 内)重写快照(原子 tmp+rename)。serve 的内存 memo 保留(它管热路径,快照管冷启动)。
- gitignore:快照进 `.git/info/exclude` 机制(同 orchestrate runtime 文件,**绝不提交**——它是缓存不是事实源,I-3)。
- **性能验收**:构造 5k 行账本,快照路径 vs 全量 fold 基准(`go test -bench`),快照应 >5x。

**落点**:`internal/pact`(fold 入口)、`internal/projection`(状态可序列化——检查现有 State 字段可 JSON 往返)。
**测试**:等价性(任意样例账本:全量 fold == 快照+增量 fold,含空/单行/并发追加后)、损坏回退(截断/篡改/版本不符→全量)、锁下并发写。**这是正确性关键任务,评审最严。**

## 6. WS-K — 动态座席(C-19)

**设计**(小步,不做完整 auto-staff):
- `pactify join` 加 `--kind <kind>`:roster 事件记 kind(投影 Agents[].Kind 已有字段,补写入路径)。
- driver 的 seat→kind 映射从「启动时定死」改为**每轮迭代重读**(state 里 roster kind 变化/新 seat 生效;`--seat-kind` flag 仍最高优先)。
- planner prompt 补一条规范:可提议新座席(名字+kind+角色),apply 时对不在 roster 的 seat 自动生成 join 事件(带 kind)。
- 解散不做(seat 留在 roster 无碍;生命周期管理留后续)。

**落点**:cmd join、`internal/pact` join payload、loop.go km 重读、planner prompt。
**测试**:join --kind 落账本、mid-run 新 seat 下轮可驱动(fake runner)、planner apply 自动 join。

## 7. WS-L — 定时/周期 orchestrate(C-21)

**设计**(serve 侧,简单格式,零新依赖):
- `pactify schedule add <project> --at "daily@03:00"|"every:6h" [--feature f]` / `list` / `remove <id>`:写 `~/.pactify/schedules.json`(机器级,非项目账本——调度是机器运维不是协议事实)。
- serve 启动一个 ticker goroutine(分钟粒度):到点 → 复用 `spawnOrchestrate`(冲突守卫已有:orchestrate 在跑则跳过本次并记日志)。
- 只支持两种表达式(`daily@HH:MM` 本地时区、`every:Nh|Nm`),手写解析,**不引 cron 依赖**;非法表达式 add 时即拒。
- serve 日志记每次触发/跳过;web UI 不做(记 backlog)。

**落点**:新 `internal/schedule`(解析+ticker,纯函数可测)、cmd_schedule.go、serve 接线。
**测试**:表达式解析表驱动、next-fire 计算(含跨午夜)、触发调用 spawn(注入 fake)、在跑冲突跳过。

## 8. Dogfood 任务图

Feature:`review-runtime`,branch `feat-review-runtime`。worker = claude-worker / kimi-worker / opencode-worker(用户指定),reviewer = claude。

| task | WS | owner | deps | verify |
|---|---|---|---|---|
| rp-fix-loop | F 自修环 | claude-worker | — | `go test ./internal/orchestrate/ -run 'Fix|Gate'` |
| rp-quorum | G quorum 评审 | claude-worker | — | `go test ./internal/pact/ ./internal/projection/ ./internal/orchestrate/` |
| rp-snapshot | J 账本快照 | claude-worker | — | `go test ./internal/pact/ ./internal/projection/`(含等价性+基准) |
| rp-critic | H critic 预评 | kimi-worker | rp-fix-loop | `go test ./internal/orchestrate/ -run Critic` |
| rp-qa-gate | I QA 门 | kimi-worker | rp-fix-loop | `go test ./internal/orchestrate/ -run QA` |
| rp-dynamic-seats | K 动态座席 | opencode-worker | — | `go test ./internal/pact/ ./internal/orchestrate/ -run 'Join|Seat'` |
| rp-schedule | L 定时 run | opencode-worker | — | `go test ./internal/schedule/ ./internal/serve/` |
| rp-e2e-docs | 收尾:quorum board 小 UI(`2/3 ✓`)+ architecture/CLAUDE.md 更新 + 全仓门 | kimi-worker | rp-quorum, rp-fix-loop | 全仓 `go test ./...` + bats + `cd web && npx vitest run && npx tsc -b --noEmit` |

**运行方式**:同包1——`--in-place` 串行,claude=orchestrator+reviewer,45min run / 8min idle。claude-worker 三个硬任务(fix-loop / quorum / snapshot 都动核心语义);kimi 三个中型;opencode 两个自包含小型(其稳定性风险最低的形状:边界清晰、独立包)。

## 9. 风险与边界

- **loop.go 改动密度**:F/H/I 都插 decide 流。任务序上 F 先行(H/I deps F),减少冲突;同 owner(F)与(G/J)串行天然避免自我冲突。
- **快照正确性**(J):等价性测试是硬门;任何怀疑 → 快照只读路径可用 env `PACT_NO_SNAPSHOT=1` 逃生。
- **opencode 稳定性**(历史 phantom checkpoint):两个任务都是边界清晰的独立包;失败限额触发即换 kimi 接手(orchestrator 手册动作)。
- **范围外**:quorum 的 web 完整 UI(仅 board 徽标)、critic 模型成本优化、QA 门截图、schedule web UI、auto-staff 解散。
