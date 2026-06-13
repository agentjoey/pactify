# 无 UI Dogfood — M3.4 Relay 交付 Implementation Plan

> **For agentic workers:** 本计划【不走】subagent-driven-development。实现者是真实外部 agent（opencode worker-1 / antigravity worker-2）经 pact 协议拉取工作；本计划由 orchestrator（claude，本 session）执行——按 §A 运行手册操作 .pact/，§B 的三份任务规格写入 `.pact/tasks/` 供 worker 领取。Steps 用 checkbox 跟踪。

**Goal:** 用 pact 协议让 claude 编排、opencode + antigravity 两个异构 worker 真实交付 M3.4 relay 接口；全程记录并当场修复协议/CLI 稳定性问题。

**Architecture:** M3.4 = serve watcher 旁路一份事件到可配置 HTTP relay 端点（best-effort 异步）。拆 3 个依赖链 task（t1 relay client → t2 watcher 钩子+serve flag → t3 集成测试+文档），依赖链使同一时刻至多一个 worker 动手，规避 F1 单工作树并发限制。orchestrator 写规格/评审/合并，worker 写实现，协议 bug 走独立 PR。

**Tech Stack:** Go（internal/serve）；pact 协议 v1（pactify CLI + MCP）；opencode 1.17.4（CLI worker）；Antigravity（MCP worker）。

**Spec:** docs/superpowers/specs/2026-06-13-headless-dogfood-m3.4-relay-design.md（LOCKED）。

**约定:** worker 实现走 TDD；M3.4 feature 经 `pactify merge` 进 main；协议/CLI 修复经 GitHub flow PR 进 main（两条线分开）；secrets 走 env/Keychain；每个 task 的验收以其规格里的测试命令为准。

---

## §A — Orchestrator 运行手册（claude 执行）

### Phase 0：环境与接线

- [ ] **A0.1 确认基线干净 + 二进制最新**

```bash
cd /Users/xtation/AgentWorks/Code_Claude/pactify
git checkout main && git pull --ff-only
go build -o /opt/homebrew/bin/pactify ./cmd/pactify   # worker/CLI 用的是这个
git status --short                                     # 必须干净
```

- [ ] **A0.2 在 pactify repo 自身初始化/确认 .pact/**

pactify 是产品开发仓（`.agent/` 是工作台），**协议产品文件 `.pact/` 此前未用于自身开发**。本 dogfood 首次让 pactify 用自己的协议开发自己。检查并按需初始化：

```bash
ls -la .pact/ 2>/dev/null && echo "EXISTS — 复用" || pactify init --seat claude
# init 会 scaffold .pact/ + 烘焙入口文件托管区块。若已存在则跳过。
cat .pact/STATE.yml 2>/dev/null | head -20
```

注意：`pactify init` 可能改动 CLAUDE.md/AGENTS.md（托管区块 `<!-- pact:begin/end -->`）。**这是 stability log 第一条观察点**——确认它没破坏 pactify 既有的 CLAUDE.md 内容（F3 修过覆盖问题，验证它在自托管场景仍正确）。

- [ ] **A0.3 接线两个 worker 座席**

```bash
# worker-1: opencode（CLI，烘焙 AGENTS.md + opencode.json）
pactify agent add opencode --id opencode-worker --roles worker
# worker-2: antigravity（GUI/MCP，写 ~/.gemini/config/mcp_config.json）
pactify agent add antigravity --id gemini-worker --roles worker
git status --short   # 看 agent add 改了哪些文件
```

记录到 stability log：antigravity 无 entry file（角色指令不自动注入，§3.1 #0），观察启动时是否需手动 prompt。

- [ ] **A0.4 注册项目到 serve（如需观测）+ 创建 stability log**

```bash
mkdir -p docs/dogfood
# 创建 docs/dogfood/2026-06-13-stability-log.md（见 §C 模板），首条写入 A0.2/A0.3 的观察。
```

- [ ] **A0.5 创建 feature + 写三份 task 规格**

```bash
# feature: relay，分支 feat-relay
pactify assign --help    # 先确认 assign/feature 的确切子命令与参数（CLI 真实签名为准，勿臆测）
```

把 §B 的 t1/t2/t3 规格写入 `.pact/tasks/relay-client.md`、`.pact/tasks/watcher-hook.md`、`.pact/tasks/integration-docs.md`（或 CLI 约定的路径——以 `pactify init` scaffold 出的 tasks 目录结构为准）。
提交 .pact/ 任务规格到 main（docs/chore 直推）。

### Phase 1：t1 relay-client（owner opencode）

- [ ] **A1.1 assign t1 给 opencode-worker，reviewer=claude**（确切命令以 `pactify assign --help` 为准；语义 = owner≠reviewer）。
- [ ] **A1.2 通知用户在终端冷启动 opencode worker**：用户运行 `PACT_AGENT_ID=opencode-worker pactify join`（确切冷启动命令以 `pactify join --help` 为准），worker 读 STATE.yml 领取 t1，按 §B-t1 规格实现 + TDD，`pactify checkpoint` 提交 evidence。**等用户同步 worker 完成**。
- [ ] **A1.3 review t1**：`git log`/`git diff` 看 worker 的实现分支；跑 `go test ./internal/serve/`；核对 §B-t1 验收。通过 → `pactify accept`；否则 `pactify changes` 附具体返工点。
- [ ] **A1.4 记录本轮 stability 观察**（冷启动、evidence 渲染、validate、STATE 漂移等）。

### Phase 2：t2 watcher-hook（owner antigravity）

- [ ] **A2.1 assign t2 给 gemini-worker（deps=[t1]，t1 必须先 accepted）**，reviewer=claude。
- [ ] **A2.2 通知用户启动 Antigravity**：打开 Antigravity，pact MCP 工具已接（A0.3）。用户 prompt worker：「你是 pact 座席 gemini-worker，用 pact join 工具冷启动，领取并交付分配给你的 task」。worker 经 MCP 动词（join/checkpoint）工作。**重点观察 MCP 接入 vs CLI 接入的动词等价性 + t1 accepted 后 t2 的 deps 解锁在 MCP 侧可见性**。等用户同步。
- [ ] **A2.3 review t2**：核对钩子接入点正确（drainNew 在 broadcast 后 enqueue）、relay==nil 时零行为变化、serve flag/env 解析；跑 `go test ./...` + `go build`。accept 或 changes。
- [ ] **A2.4 记录 stability 观察**（MCP 动词行为、跨 worker 交接、deps 可见性）。

### Phase 3：t3 integration-docs（owner opencode）

- [ ] **A3.1 assign t3 给 opencode-worker（deps=[t2]）**，reviewer=claude。
- [ ] **A3.2 用户重新冷启动 opencode worker 领 t3**。等同步。
- [ ] **A3.3 review t3**：集成测试真实起 httptest receiver 并断言信封 + 失败隔离；文档更新到位；`go test ./...` 绿。accept 或 changes。
- [ ] **A3.4 记录 stability 观察**。

### Phase 4：合并 + 收尾

- [ ] **A4.1 全部 task accepted → 合并 feature**：`pactify merge relay`（确切命令以 `pactify merge --help` 为准；铁律：所有 task accepted 才允许）。验证 feat-relay 合回 base、main 上 `go test ./...` + `go build` 绿。
- [ ] **A4.2 重建二进制**：`go build -o /opt/homebrew/bin/pactify ./cmd/pactify`。
- [ ] **A4.3 完成 stability 报告**（docs/dogfood/2026-06-13-stability-log.md）：汇总所有发现/修复/PR 号，给出协议稳定性结论（哪些硬了、哪些仍是已知限制如 F1 并发、下一轮该补什么）。
- [ ] **A4.4 更新 .agent/CURRENT.md + sprint-003.md（M3.4 ✅ + dogfood 结论）+ memory project_pactify.md**。
- [ ] **A4.5 给用户终审报告**：M3.4 交付情况 + stability 结论 + 协议是否达到"无 UI 全链路稳定"。

### 协议/CLI 修复纪律（贯穿 Phase 1-4）

dogfood 中发现的真实协议/CLI bug：
1. 在 stability log 记现象 + 复现 + 根因；
2. 开 `fix/*` 分支（**独立于 feat-relay**）→ 改 + 加回归测试 → PR → CI 绿 → merge → 重建二进制；
3. stability log 记 PR 号；
4. 从卡点继续 dogfood。
worker 卡在协议 bug 上时，orchestrator 先修协议（这是 orchestrator 的活，不算替 worker 写实现），再让 worker 重试。

---

## §B — 三份 Pact 任务规格（写入 .pact/tasks/，供 worker 拉取）

> 每份规格是给【外部 worker】看的契约：目标、要碰的文件、确切签名、验收测试。worker 用 TDD 自己写实现；orchestrator 不替写。

### t1 · relay-client

**目标**：实现一个 best-effort 异步 relay client——把 serve 的日志事件 POST 到可配置 HTTP 端点，永不阻塞调用方。纯组件，本 task 不接 watcher。

**新建文件**：`internal/serve/relay.go` + `internal/serve/relay_test.go`

**契约（签名照此实现）**：
```go
package serve

// relay POSTs serve log events to a configured endpoint, best-effort & async.
// It NEVER blocks the caller and NEVER propagates failures back to the watcher.
type relay struct { /* url, token, queue chan relayMsg, done chan struct{}, dropped counter ... */ }

type relayMsg struct { project, line string }

// newRelay builds a relay posting to url with optional bearer token. A "" url
// returns nil (relay disabled — callers must nil-check before enqueue).
func newRelay(url, token string) *relay

// enqueue hands one event (project id + raw log.jsonl line) to the relay's
// bounded queue. Non-blocking: if the queue is full, the OLDEST pending message
// is dropped and a dropped-counter incremented. Safe to call on a nil relay
// (no-op) so callers need not branch.
func (r *relay) enqueue(project, line string)

// start launches the background poster goroutine (called by newRelay or by the
// Server when wiring — your choice, document it).
// stop drains/closes and waits for the goroutine to exit (called from serve Stop).
func (r *relay) stop()
```

**行为要求**：
- 队列有界（cap 256）；满时丢最旧 + `dropped` 计数（暴露一个读取方法供测试断言）。
- 后台 goroutine POST：`Content-Type: application/json`；token 非空时加 `Authorization: Bearer <token>`。
- POST 失败 → 有界指数退避重试（≤3 次，退避上限几秒），仍失败则丢弃 + 计数。**失败绝不向调用方传播**。
- POST body = `{"project":"<id>","event":<line 解析成的 JSON 对象>}`。line 本身是一条 JSON event；解析失败的 line 仍要安全处理（跳过或原样包裹，记一条计数，别 panic）。
- `newRelay("", "")` 返回 nil；`(*relay)(nil).enqueue(...)` 是安全 no-op。

**验收测试（relay_test.go 必须覆盖）**：
- `newRelay("","")` 返回 nil；nil relay 的 enqueue 不 panic。
- token header 注入（用 httptest.Server 断言收到的 Authorization）。
- body 信封格式正确（project + event 对象）。
- 队列满 → 丢最旧 + dropped 计数自增（灌满 cap+N 条，断言 receiver 收到的条数 ≤ cap 且 dropped≥N，或等价确定性断言）。
- receiver 返回 500 → enqueue 不阻塞、不报错；重试后最终丢弃 + 计数。

**测试命令**：`cd /Users/xtation/AgentWorks/Code_Claude/pactify && go test ./internal/serve/ -run Relay -v`

**不要碰**：watch.go / api.go / serve 命令行（那是 t2）。

### t2 · watcher-hook

**目标**：把 t1 的 relay client 接进 serve——watcher 每广播一条事件就旁路一份给 relay；`pactify serve` 暴露 `--relay-url`/`--relay-token`（env `PACT_RELAY_TOKEN` 兜底）。relay 未配置时 serve 行为字节级不变。

**deps**：t1（relay client 已 accepted）。

**修改文件**：
- `internal/serve/api.go`：`Server` 结构加 `relay *relay` 字段；`New(projects []registry.Project)` 维持现签名不变，**另加** `SetRelay(url, token string)`（mirror 既有 `SetSeat`），内部 `s.relay = newRelay(url, token)`。
- `internal/serve/watch.go`：`drainNew` 在 `s.hub.broadcast(id, t)` 之后加 `s.relay.enqueue(id, t)`（nil-safe，relay==nil 时 no-op）。`Stop()` 加 `if s.relay != nil { s.relay.stop() }`。
- `cmd/pactify/cmd_serve.go`：加 `--relay-url`（默认 ""）、`--relay-token`（默认 `os.Getenv("PACT_RELAY_TOKEN")`）两个 flag；`srv.SetSeat(seat)` 后调 `srv.SetRelay(relayURL, relayToken)`。

**行为要求**：
- relay-url 为空 → `SetRelay` 内 `newRelay("",...)` 返回 nil → drainNew 的 enqueue no-op → **SSE 扇出、offset 推进、所有现有行为零变化**（这是硬约束）。
- relay 启用时，每条被 broadcast 的完整行也 enqueue（同一条、同一 id）。
- token flag > env `PACT_RELAY_TOKEN`（flag 非空时用 flag）。serve 启动输出可加一行 relay 目标提示（不打印 token）。

**验收测试**：
- `internal/serve/watch_test.go`（或新增）：relay==nil 时 drainNew 行为与现状一致（现有 watcher 测试不回归）。
- relay 启用时 drainNew 既 broadcast 又 enqueue（可用一个记录型 fake，或断言 relay 收到——与 t3 集成测试边界协调：t2 只需证明"接上了"，端到端在 t3）。
- `go build ./...` 通过；`pactify serve --help` 显示新 flag。

**测试命令**：`cd /Users/xtation/AgentWorks/Code_Claude/pactify && go test ./internal/serve/ && go build ./... && pactify serve --help`

**不要碰**：relay.go 的内部实现（t1 已定）；新增的逻辑只在接线层。

### t3 · integration-docs

**目标**：端到端集成测试证明"append 日志 → relay 端点收到正确信封"，且"relay 端点失败不影响 watcher/SSE"；更新文档。

**deps**：t2（钩子+flag 已 accepted）。

**新建/修改文件**：
- `internal/serve/relay_integration_test.go`（新建）：
  - 起 `httptest.Server` 当 relay receiver（记录收到的 body）。
  - 构造 Server（`New` + `SetRelay(receiver.URL, "")`）over 一个临时 .pact/ 项目（参考既有 watch_test.go 怎么建临时 project + 写 log.jsonl）。
  - 启动 watcher → append 一条合法 event 行到该项目 log.jsonl → 在超时内断言 receiver 收到 `{"project":...,"event":{...}}` 且字段正确。
  - 第二例：receiver 恒返回 500 → append 行 → 断言 SSE 订阅者仍收到该行、offset 仍推进（relay 失败隔离）。
- `docs/architecture.md`：加 "M3.4 relay 接口" 小节（挂载点 = drainNew 旁路；best-effort 异步；配置 = serve --relay-url/--relay-token + PACT_RELAY_TOKEN；线格式信封；失败隔离语义）。
- `docs/operations.md`：加 relay 配置/运维说明（如何指端点、token 走 env/Keychain、丢弃计数含义、未配置=禁用）。

**行为要求**：测试必须真实经过 fsnotify→drainNew→relay 全链路（不是直接调 enqueue）。文档示例用 env/Keychain，不写明文 token。

**验收测试**：`cd /Users/xtation/AgentWorks/Code_Claude/pactify && go test ./internal/serve/ -v && go test ./...`（全绿）。

**不要碰**：relay.go / watch.go 的实现逻辑（t1/t2 已定）；只加测试 + 文档。

---

## §C — Stability Log 模板（docs/dogfood/2026-06-13-stability-log.md）

```markdown
# Headless Dogfood Stability Log — 2026-06-13

座席：claude(orchestrator+reviewer) / opencode-worker / gemini-worker(antigravity)
载体 feature：relay（M3.4）

## 观察 / 摩擦点
### #N <一句话标题>
- 阶段：A0.2 / t1 / t2 ...
- 现象：
- 复现：
- 根因：
- 处置：修复 PR #__ / 记录为已知限制 / 文档补充
- 回归：

## 协议稳定性结论
- 硬了：
- 仍是已知限制：
- 下一轮该补：
```

每个 A 阶段的 "记录 stability 观察" step 往这里追加。即便某阶段顺畅无摩擦，也写一条"#N 顺畅，无摩擦"作正向证据。

---

## Self-Review 结论

- **spec 覆盖**：spec §0 成功标准→A4.3/A4.5；§1 座席驱动→A0.3+各 Phase 启动 step；§2 M3.4 设计→§B 三份规格逐条（2.2 组件→t1+t2、2.3 线格式→t1 body+t3 断言、2.4 测试→t1/t3、2.5 范围外→各规格"不要碰"）；§3.1 拆解→§B 表、§3.2 闭环→A 各 Phase 五步、§3.3 观察→§C log + 各"记录"step、§3.4 修复纪律→A 末"修复纪律"段；§4 约束→各规格行为要求 + 信封/隔离硬约束。无缺口。
- **占位符扫描**：CLI 确切子命令（assign/join/checkpoint/accept/changes/merge 的参数）有意标注"以 `--help` 为准"——因为这是真实 dogfood 的第一个验证点（CLI 的人体工学本身在受测），不预先臆造参数。其余无 TBD。
- **签名一致性**：relay/newRelay/enqueue/stop/SetRelay/relayMsg 在 t1 定义、t2/t3 引用一致；drainNew/broadcast/Stop/New/SetSeat 均对照真实代码（api.go:62 New、watch.go:124 broadcast、cmd_serve.go SetSeat）。
- **已知风险（有意）**：t2 的"证明接上了"与 t3 的"端到端"边界靠规格里的协调说明划清，避免 t2 写重端到端测试。
