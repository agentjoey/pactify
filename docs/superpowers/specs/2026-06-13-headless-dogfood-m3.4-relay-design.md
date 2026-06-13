# 无 UI 全链路 dogfood —— 用 pact 协议交付 M3.4 relay 接口

日期：2026-06-13 · 状态：LOCKED（用户已批准设计）
战略背景：Dashboard v2 / Canvas P0 之后，UI 端实测体验差，决定**暂缓 UI，先跑通无 UI 状态下的协议/CLI 全链路稳定性**。手段 = 用 pact 协议本身，由 claude 编排、两个异构外部 agent 参与，交付 pactify 自己的下一个真实里程碑 M3.4。

本 spec 有两层：Layer A 是 agent 们要造的功能（M3.4 relay 接口），Layer B 是这次 dogfood 运行本身（真正的目的——稳定性验证）。两层都是交付物。

## 0. 目标与成功标准

**首要目标**：验证 pact 协议 + CLI 在真实异构多 agent 交付中的稳定性。M3.4 是载体，不是首要目的。

**成功标准（双）**：
1. M3.4 relay 接口经 pact 协议（assign → join → checkpoint → review → accept → merge）真实交付并合并到 main。
2. 一份 stability 报告，列出全程发现的每一个协议/CLI 摩擦点 + 根因 + 修复 + 回归。**第 2 条权重高于第 1 条**——这轮下来协议要变硬。

**非目标**：UI/dashboard 任何改动；relay 的落盘缓冲/云端接收侧（Phase 4 M4.1）；并发 worktree 隔离（F1 已知限制，本轮用依赖链规避）。

## 1. 座席与驱动

| 座席 | 角色 | 驱动方式 |
|---|---|---|
| claude（本 session） | orchestrator + reviewer | 我直接用 pact skill 操作 .pact/ |
| opencode（1.17.4，CLI） | worker-1 | 用户在终端 `pactify join` 冷启动 |
| antigravity（GUI IDE，MCP 接入） | worker-2 | 用户启动 Antigravity，pact MCP 工具自描述 |

**接线（已验证可行）**：
- opencode：`pactify agent add opencode --id <seat> --roles worker`（烘焙 AGENTS.md + opencode.json）。
- antigravity：`pactify agent add antigravity --id <seat> --roles worker`，MCP 配置写 `~/.gemini/config/mcp_config.json`（Antigravity 经 `~/.gemini/antigravity/mcp_config.json` 软链读取——已查实路径正确）。
- **已知接线缺口（记入 stability log #0）**：antigravity 这个 kind 的 entry file 为空，pactify 不烘焙角色说明文件，worker 角色指令需用户启动时 prompt（pact MCP 工具自描述可补足）。本轮观察这是否构成实际障碍；若是，候选修复 = 给 antigravity kind 补 GEMINI.md entry。

**铁律不变**：worker 不能自标 accepted（只能 awaiting_review）；reviewer（claude）≠ owner（separation of duties）；feature 内所有 task accepted 才能 merge。

## 2. Layer A —— M3.4 relay 接口设计

### 2.1 挂载点（已查实）
`internal/serve/watch.go` 的 `drainNew(id, lp)` 逐条读 log.jsonl 新增完整行，对每行调 `s.hub.broadcast(id, t)`（SSE 扇出）。relay 在**同一点旁路**一份：`broadcast` 之后 enqueue 到 relay client。

### 2.2 组件
- **`internal/serve/relay.go` —— relay client**：
  - 构造：`newRelay(url, token string) *relay`；`url==""` 返回 nil（relay 禁用，零行为变化）。
  - 接口：`enqueue(projectID, line string)`——非阻塞，投递到有界 channel（cap 例如 256）。
  - 后台 goroutine：从 channel 取，POST 到 `url`，header `Authorization: Bearer <token>`（token 非空时）+ `Content-Type: application/json`。
  - **best-effort 异步语义**：队列满 → 丢最旧并计数（`dropped` 计量）；POST 失败 → 有界退避重试（如 3 次，指数退避上限几秒），仍失败则丢弃并计数；**永不阻塞 watcher、永不影响 SSE**。
  - 关闭：`stop()` 关 channel + 等 goroutine 收尾（serve Stop 时调）。
- **watcher 钩子**：`drainNew` 在 `s.hub.broadcast(id, t)` 后，若 `s.relay != nil` 则 `s.relay.enqueue(id, t)`。Server 结构加 `relay *relay` 字段，`New(...)` 接受 relay 配置。
- **serve 命令行**：`pactify serve` 加 `--relay-url string` + `--relay-token string`；token 解析优先级 = flag > env `PACT_RELAY_TOKEN`（Keychain 规约：文档示例用 env/Keychain，不写明文 settings）。未传 `--relay-url` = relay 不启用。

### 2.3 线格式
POST body = JSON 信封，复用 log schema 零改动（ROADMAP M4.1）：
```json
{ "project": "<project-id>", "event": <原始 log.jsonl 行解析出的 JSON 对象> }
```
原始行本就是一条 JSON event（event_id/ts/agent_id/role/event_type/task_id/feature/payload）；信封只加 project 包裹，不重塑字段。

### 2.4 测试
- 单测：relay client 队列满丢弃 + 计数；POST 失败重试后丢弃；token header 注入；`url==""` 返回 nil。
- 集成测试：起一个 stub HTTP receiver（httptest.Server）→ `New` 带 relay-url → append 一行到某 project 的 log.jsonl → 断言 receiver 在超时内收到正确信封；再令 receiver 返回 500 → 断言 watcher 的 SSE 扇出与 offset 推进不受影响（relay 失败隔离）。
- 全部 Go TDD；`go test ./internal/serve/` 绿。

### 2.5 范围外（A 层）
落盘缓冲、云端接收 daemon、relay 的 mTLS/签名、多 relay 端点、事件过滤/批量。本轮只做单端点 best-effort POST。

## 3. Layer B —— dogfood 运行

### 3.1 任务拆解（依赖链串行化，规避 F1 单工作树并发）
M3.4 作为一个 feature（id 例如 `relay`，分支 `feat-relay`），拆 3 个 task，依赖链使同一时刻至多一个 worker 动手：

| task | 内容 | owner | deps |
|---|---|---|---|
| t1 relay-client | `internal/serve/relay.go` + 单测（纯组件，无 watcher 集成） | opencode | — |
| t2 watcher-hook | drainNew 钩子 + Server.relay 字段 + serve flag/env | antigravity | t1 |
| t3 integration-docs | 集成测试（stub receiver + 失败隔离）+ architecture.md/operations.md | opencode | t2 |

每个 task 一份 `.pact/tasks/<id>.md` 规格（目标 / 文件 / 验收 / 测试命令），由 orchestrator（claude）撰写并 assign。

### 3.2 运行闭环（每 task 一轮）
1. orchestrator 写 task 规格 + `pactify assign <task> --owner <seat> --reviewer claude`。
2. 用户在对应 worker 终端冷启动（`pactify join`，读 STATE.yml 拉取式领取）。
3. worker 实现 + `pactify checkpoint`（置 awaiting_review，附 evidence）。
4. orchestrator review：通过 → `pactify accept`；否则 `pactify changes`（worker 返工）。
5. 全部 accepted → `pactify merge <feature>`。

### 3.3 稳定性观测（核心产出）
全程维护 `docs/dogfood/2026-06-13-stability-log.md`（dogfood 运行账本），每个摩擦点一条：现象 / 复现 / 根因 / 修复 commit / 回归。**边跑边修**：协议或 CLI 的真实 bug 当场根因、修复、回归，再从卡点继续。预期重点观察面：
- 冷启动（join）在 opencode vs antigravity 两种接入下的一致性；
- antigravity 无 entry file 的角色注入缺口（#0）是否构成实际障碍；
- worker 交接时 STATE.yml 投影一致性（t1 accepted 后 t2 的 deps 解锁可见性）；
- checkpoint 的 evidence 多行/特殊字符渲染与 validate；
- reviewer 铁律实战（owner≠reviewer、worker 不能自接受）；
- MCP 接入（antigravity）vs CLI 接入（opencode）的动词行为等价性。

### 3.4 修复纪律
dogfood 中发现的协议/CLI 修复，走正常 GitHub flow（feat/fix 分支 → PR → CI 绿 → merge），**与 M3.4 的 feature 分支分开**——M3.4 经 pact merge 进 main，协议修复经 PR 进 main，两条线不混。stability log 记录每条修复的 PR 号。

## 4. 工艺与约束
- relay 失败必须与 watcher/SSE 完全隔离（best-effort 的硬约束）。
- relay 未配置时 serve 行为字节级不变（回归基线）。
- secrets 走 env/Keychain，文档示例不得明文 token。
- 每个 task 的 worker 产出在该 task 的分支提交；orchestrator 不替 worker 写实现（除非修复协议 bug）。
- M3.4 验收以 task 规格里的测试命令为准；go test + 集成测试绿才 accept。
