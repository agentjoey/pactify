# pactify orchestrate — 协议状态机自主驱动器设计

日期：2026-06-13 · 状态：LOCKED（用户已批准设计）
来源：无 UI dogfood（2026-06-13）的头号发现 **#9**——pactify 缺自动按 log 状态机调起 worker 的能力，导致人退化成调度器。本设计让"消灭人肉中继"在产品层兑现，而非靠聪明 orchestrator 手搓 `opencode run`。
前置：M3.4 relay 已交付；本设计独立，不依赖 relay。

## 0. 问题与目标

**问题**：pact 协议把协调【内容】放进文件（log.jsonl 是源），但协调【时机】仍需有人触发——worker 是一次性会话，每个状态变迁（assigned→该干活、awaiting_review→该评审、全 accepted→该合并）都要有人把对应 agent 重新拉起。dogfood 里靠 orchestrator(claude) 手搓 `opencode run` 在运营层补位，但 pactify 自身没有这个能力。

**目标**：`pactify orchestrate` —— 一个中心化驱动进程，监听 pact 状态机，在每个变迁自动 exec 对应 agent，串行跑完整个 worker→评审→合并闭环，直到 feature shipped 或卡住升级给人。人写一次任务图就走开。

**成功标准**：给定一个预定义任务图（task 规格 + owner/reviewer/deps），`pactify orchestrate` 零人工把 feature 跑到 shipped；卡住时暂停并升级给人，人修完 `--resume` 继续。

**非目标**：自动规划（planner agent 拆任务）；git worktree 并发多 worker（先串行）；分布式 worker 守护；relay 反哺；跨进程锁。

## 1. 决策（用户已拍）

1. **自动化边界 = 全闭环**：驱动 worker（assigned/changes_requested）+ reviewer（awaiting_review）+ merge（全 accepted）。
2. **任务来源 = 消费预定义任务图**：人/编排者先写好 task 规格 + owner/reviewer/deps（就像 dogfood 的 t1/t2/t3），orchestrate 读它跑到底。不含自动规划。
3. **卡住 = 升级给人**：阈值（per-task 返工 ≥3 轮 / agent 协议外异常）超过则暂停+通知+等人，非终止。
4. **中心化而非分布式守护**（全闭环需全局视角判断 merge/reviewer 时机）。
5. **硬测试门**：LLM 评审之上垫一层确定性安全网（详见 §2.4）。

## 2. 架构

中心化驱动进程，串行主循环（串行天然规避 F1 单工作树）：
```
投影 STATE → nextAction(STATE, 历史) → 生成简报 → exec 座席 runner（阻塞）
  → agent 用 pact 动词干活 → exec 返回 → 重投影 → 循环
  → 无可动作且有未完成 / 阈值超限 → 升级 + 暂停
```

### 2.1 nextAction —— 纯函数决策（可测性支点）
签名（拟）：`nextAction(state, history) → Action`，`Action ∈ {RunOwner(task), RunReviewer(task), Merge(feature), Idle, Stuck(reason)}`。
优先级与规则：
- task `assigned` 或 `changes_requested` 且 deps 全 `accepted` → `RunOwner(task)`
- task `awaiting_review` → `RunReviewer(task)`
- 某 feature 所有 task `accepted` 且**硬测试门通过** → `Merge(feature)`
- 无上述可动作但仍有未完成 task → 检查阈值：超限 `Stuck`，否则（理论不应发生）`Idle`
- 全部 feature shipped → 正常结束
纯函数：只读 state + 本进程维护的变迁历史（per-task 返工计数、连续失败计数），无 IO。全单测覆盖。

### 2.2 per-seat agent runner（核心 novel 件）
怎么 headless 拉起座席的 agent 喂简报。扩展现有 `internal/agent` 的 `Adapter`，加 headless 运行能力（与既有 `Invocation()`=MCP wiring 区分）。已核验的 per-kind 默认 runner：
- `opencode` → `opencode run "<briefing>"`
- `claude-code` → `claude -p "<briefing>"`（claude CLI 在 PATH）
- `gemini-cli` → `gemini -p "<briefing>"`
- **GUI/desktop kind**（`antigravity`/`claude-desktop`/`codex-app`/`codex-cli`(若无 headless)）→ **无 runner**。
runner 解析：座席 kind 的默认 runner，可在 roster 声明时 per-seat 覆盖（如 `--runner`）。**关键路径上的座席无可用 runner（GUI）→ orchestrate fail-closed 报错**，提示换 CLI 座席或把那一棒留人工。runner 接口化（见 §2.5）以便测试注入。

### 2.3 briefing 生成器（复用 + 扩展 briefing.go）
现有 `briefing.go` 已渲染 agent-agnostic 入职简报，扩展为两种角色简报：
- **worker 简报**：你是座席 X(roles)，冷启动 `pactify join`，`pactify status` 找你 owner 的 assigned/changes_requested task，读其 spec，TDD 实现（只碰 spec 列的文件），跑验收命令，`pactify checkpoint` 附 evidence，**不自接受**。changes_requested 时附上该 task 的最近 changes reason（从 log 取）。
- **reviewer 简报**：你是座席 X(reviewer)，task <id> 待审，读 spec + worker 的 diff（`git diff` / `git log`），**跑该 task spec 的验收命令**，通过 `pactify accept <id>`，否则 `pactify changes <id> --reason "<具体返工点>"`。不自己改实现。
简报内容由 state 动态填充（座席/角色/task id/spec 路径/changes reason）。纯函数，可单测。

### 2.4 硬测试门（质量底线，回应"LLM 批 LLM"顾虑）
`Merge(feature)` 前，即便所有 task 已被 reviewer agent accept，orchestrate **独立**再跑一遍该 feature 的验收命令（来源：feature/task 规格里声明的验收命令，或一个 feature 级 `verify` 命令）。**不过则不 merge，转 Stuck 升级**。给 LLM 评审垫确定性安全网。验收命令从何取需在 task 规格约定一个机器可读字段（见 §4 规格扩展）。

### 2.5 escalation / 护栏
- per-task 返工上限（默认 3 轮 changes）；agent 非零退出或"未产生预期变迁"算一次失败，重试 1 次再升级；全局迭代上限兜底（防失控烧 token）。
- 升级 = 写一条升级记录（escalation 文件 `.pact/orchestrate/escalation-<ts>.md`，含 task/原因/最近 evidence/建议）+ 桌面通知（复用现有 notify 能力如有）+ **暂停等人**。
- 人修完（手动改协议 bug / 改实现 / 重写规格）后 `pactify orchestrate --resume` 从暂停点继续（重投影 state，从 nextAction 接着跑）。
- **可中断**：Ctrl-C / SIGTERM 优雅停（不杀正在跑的 agent 子进程的中途提交——等当前 exec 结束或安全点）。

## 3. 组件 / 文件结构

- `internal/orchestrate/`（新包）：
  - `loop.go` —— 主循环 + exec 编排 + 暂停/resume/中断。
  - `decide.go` —— `nextAction` 纯函数 + 变迁历史/阈值。
  - `brief.go` —— worker/reviewer 简报生成（或扩展 `internal/agent/briefing.go`，二选一，spec 倾向放 orchestrate 包以免 agent 包膨胀）。
  - `runner.go` —— `Runner` 接口（`Run(ctx, seat, briefing) error`）+ 真实 exec 实现 + per-kind 解析；测试注入 fake。
  - `gate.go` —— 硬测试门（跑 feature 验收命令）。
  - `escalate.go` —— 升级记录 + 通知。
- `internal/agent/`：`Adapter` 加 headless runner 模板（per-kind），GUI kind 返回空。
- `cmd/pactify/cmd_orchestrate.go`：`pactify orchestrate [--feature <id>] [--resume] [--max-rework N] [--max-iters N] [--dry-run]`。
- watcher 复用：orchestrate 串行模型下不必常驻订阅——每步 exec 后主动重投影即可；fsnotify 仅用于"等待 agent 产生变迁"的可选优化（先用 exec 返回 + 重投影，YAGNI）。

## 4. task 规格扩展（机器可读验收命令）

硬测试门与 reviewer 简报都需要"该 task/feature 的验收命令"。当前 task 规格是自由 markdown。最小扩展：在 task 规格里约定一个 fenced 块或 frontmatter 字段 `verify:`，orchestrate 提取。
- 倾向：task 规格 frontmatter 加 `verify: "go test ./internal/serve/ -run Relay"`；feature 级验收 = 各 task verify 的并集或一个显式 feature verify。
- 缺 verify 字段 → 硬测试门退化为 `go build ./... && go test ./...`（保守全量）并告警。

## 5. 数据流 / 错误处理

- 每步 exec 阻塞等 agent 这一轮结束 → 重投影 → 校验是否产生预期变迁（RunOwner 后该 task 应进 awaiting_review；RunReviewer 后该 task 应进 accepted 或 changes_requested）→ 未变迁记一次失败。
- agent 报"协议外异常"（约定：agent 在输出里打特定标记，或非零退出）→ 直接升级。
- 卡住升级后暂停；`--resume` 续跑。
- `--dry-run`：只打印 nextAction 序列与将 exec 的命令，不真拉 agent（编排逻辑的安全预演）。

## 6. 测试

- **纯函数单测**：`nextAction`（各状态/deps/阈值分支）、简报生成、per-kind runner 解析、verify 字段提取。
- **集成测试注入 fake runner**：`Runner` 接口塞确定性脚本——worker 简报→跑预设 `pactify checkpoint`；reviewer 简报→跑 `pactify accept`（或前 N 次 `changes` 测返工回路与升级）。在临时 .pact/ 项目上端到端验证 orchestrate 把 feature 跑到 shipped、返工回路、阈值升级、硬测试门拦截。**不用真 LLM**。
- e2e（可选，后置）：真 `opencode run` 跑一个玩具 task 验证真实 runner 路径。

## 7. 工艺约束

- runner exec 必须接口化，生产 exec 与测试 fake 共用接口（杜绝测试桩混入生产）。
- 硬测试门独立于 reviewer 判断——LLM accept ≠ 可 merge。
- secrets：runner 不在命令行明文传 token；agent 自身的凭据由其自身配置管。
- 升级是暂停不是终止；人永远能介入。
- orchestrate 不替 agent 写实现/做评审判断——它只是调度器 + 确定性测试门。
