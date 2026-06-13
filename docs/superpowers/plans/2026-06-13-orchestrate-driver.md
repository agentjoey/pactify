# pactify orchestrate 驱动器 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `pactify orchestrate`——监听 pact 状态机、在每个变迁自动 headless 拉起对应 agent、串行跑完 worker→评审→合并闭环，卡住升级给人。

**Architecture:** spec = `docs/superpowers/specs/2026-06-13-orchestrate-driver-design.md`（LOCKED）。中心化串行驱动：读 `.pact/log.jsonl` → `projection.Project` → `nextAction(纯函数)` → 生成简报 → 经 `Runner` 接口 exec 座席 agent（opencode/claude/gemini headless）→ 重投影 → 硬测试门 → merge/升级。orchestrate 直接调的协议动词仅 `Merge` + 读 state；join/checkpoint/accept/changes 由被驱动 agent 经简报执行。

**Tech Stack:** Go。复用 `internal/projection`（State 投影纯折叠）、`internal/pact`（engine 动词）、`internal/agent`（kind 适配器）。新包 `internal/orchestrate`。

**约定:** Go TDD（先失败测试）；每 task 一 commit；CLI 子命令最后接；运行子进程 exec 必须接口化（生产 exec 与测试 fake 共用 `Runner`/命令执行接口，杜绝测试桩入生产，spec §7）。Wave1=T1-T4（纯核心，可并行无依赖）；Wave2=T5-T7（loop+exec）；Wave3=T8-T9（CLI+文档）。

---

## 共享类型锚点（所有 task 以此为准）

```go
// internal/orchestrate/decide.go
package orchestrate

import "github.com/agentjoey/pactify/internal/projection"

type ActionKind int
const (
	ActIdle ActionKind = iota
	ActRunOwner       // 拉 owner（worker）跑该 task
	ActRunReviewer    // 拉 reviewer 审该 task
	ActMerge          // 该 feature 全 accepted + 硬门通过 → 合并
	ActStuck          // 阈值超限 / 无可动作但有未完成
	ActDone           // 全 feature shipped
)

type Action struct {
	Kind    ActionKind
	Feature string // ActMerge / ActRunOwner / ActRunReviewer 关联 feature
	Task    string // ActRunOwner / ActRunReviewer 关联 task
	Seat    string // 要拉起的座席（owner 或 reviewer）
	Reason  string // ActStuck 的原因
}

// History 是驱动进程维护的可变状态（非协议态）：per-task 返工/失败计数。
type History struct {
	Rework  map[string]int // taskID -> 已观察到的 changes_requested 轮数
	Fails   map[string]int // taskID -> exec 后"未产生预期变迁"的连续失败数
	Iters   int            // 全局已执行动作数
}

// Thresholds 阈值（CLI flag 注入；默认见 §CLI）。
type Thresholds struct{ MaxRework, MaxFails, MaxIters int }

// nextAction 是纯函数：只读 state + history + 阈值，返回下一步动作。无 IO。
func nextAction(st projection.State, h History, th Thresholds) Action
```

```go
// internal/agent/agent.go — Adapter 扩展（headless runner）
type RunnerSpec struct {
	Command string   // 例 "opencode" / "claude" / "gemini"
	// Args 用 "{briefing}" 占位简报文本的位置；调用方替换为真实简报。
	Args    []string // 例 ["run","{briefing}"] / ["-p","{briefing}"]
}
// Runner 返回该 kind 的 headless 调起规格；ok=false 表示无 headless runner（GUI/desktop kind）。
func (s spec) Runner() (RunnerSpec, bool)
// Adapter 接口加: Runner() (RunnerSpec, bool)
```

```go
// internal/orchestrate/runner.go — exec 接口（生产 exec + 测试 fake 共用）
type Runner interface {
	// Run headless 拉起 seat 的 agent 喂 briefing，阻塞到这一轮结束。
	Run(ctx context.Context, seat projection.Seat, briefing string) error
}
// CmdRunner 是真实实现：用 agent.Runner(kind) 解析命令 + exec。
// fakeRunner（测试）：按简报内容跑预设的 pactify 动词，确定性。
```

```go
// internal/pact/engine.go — 新增导出（orchestrate 读 state 用，避免重复 log 解析）
func (p *Project) StateProjection() (projection.State, error) // 包装现有 state()，仅返回 State
```

---

### Task 1: nextAction 纯函数决策（decide.go）

**Files:**
- Create: `internal/orchestrate/decide.go`
- Test: `internal/orchestrate/decide_test.go`

- [ ] **Step 1: 失败测试**（覆盖每条规则与阈值分支）

```go
// 用 projection.State 字面量构造场景，断言 nextAction 返回的 Action。
// a) task assigned + deps 空 → RunOwner(task, seat=owner)
// b) task assigned + dep 未 accepted → 该 task 不可动（看下一可动作或 Stuck）
// c) task assigned + dep 已 accepted → RunOwner
// d) task awaiting_review → RunReviewer(task, seat=reviewer)
// e) feature 全 task accepted → Merge(feature)   （注：硬门在 loop 层，nextAction 只判全 accepted）
// f) 全 feature shipped → Done
// g) 有未完成 task 但无可动作 + Rework[t] >= MaxRework → Stuck(含 task)
// h) Fails[t] >= MaxFails → Stuck
// i) Iters >= MaxIters → Stuck
// 多 feature/多 task 时的动作优先级：RunReviewer > RunOwner > Merge（先消化在审的，再推进新活，最后合并）——测试固定该顺序。
```

- [ ] **Step 2:** `cd /Users/xtation/AgentWorks/Code_Claude/pactify/web 2>/dev/null; cd /Users/xtation/AgentWorks/Code_Claude/pactify && go test ./internal/orchestrate/ -run NextAction -v` → FAIL（包/函数不存在）
- [ ] **Step 3:** 实现 `ActionKind`/`Action`/`History`/`Thresholds`/`nextAction`（锚点签名）。deps 满足 = 所有 dep 在同 feature 内 status==accepted。优先级 RunReviewer>RunOwner>Merge。Stuck 判定用 history+阈值。
- [ ] **Step 4:** `go test ./internal/orchestrate/ -run NextAction -v` → PASS
- [ ] **Step 5:** Commit `feat(orchestrate): nextAction pure decision function`

### Task 2: 简报生成器（brief.go）

**Files:**
- Create: `internal/orchestrate/brief.go`
- Test: `internal/orchestrate/brief_test.go`

- [ ] **Step 1: 失败测试**

```go
// workerBrief(seat, task, changesReason) 文本应含：座席 id+roles、"pactify join"、
//   task.ID、task.Spec 路径、"pactify checkpoint"、"不要自标 accepted"；
//   changesReason 非空时含该 reason 文本。
// reviewerBrief(seat, task) 文本应含：座席 id、task.ID、task.Spec、"git diff"、
//   "pactify accept"、"pactify changes"、跑验收命令的指示。
// 断言关键子串存在（不锁全文）。
```

- [ ] **Step 2:** 运行 → FAIL
- [ ] **Step 3:** 实现 `workerBrief(seat projection.Seat, task projection.Task, changesReason string) string` 和 `reviewerBrief(seat projection.Seat, task projection.Task) string`。可复用 `internal/agent/briefing.go` 的措辞风格，但本函数放 orchestrate 包（避免 agent 包膨胀）。changesReason 由 loop 从 log 取最近 changes 事件 payload（loop 层传入）。
- [ ] **Step 4:** 运行 → PASS
- [ ] **Step 5:** Commit `feat(orchestrate): worker/reviewer briefing generators`

### Task 3: agent Adapter headless runner 扩展

**Files:**
- Modify: `internal/agent/agent.go`（Adapter 接口 + spec.Runner + registry 行为）
- Test: `internal/agent/agent_test.go`（追加）

- [ ] **Step 1: 失败测试**

```go
// agent.Get("opencode").Runner() → {Command:"opencode", Args:["run","{briefing}"]}, ok=true
// agent.Get("claude-code").Runner() → {Command:"claude", Args:["-p","{briefing}"]}, ok=true
// agent.Get("gemini-cli").Runner() → {Command:"gemini", Args:["-p","{briefing}"]}, ok=true
// agent.Get("antigravity").Runner() → ok=false（GUI 无 headless runner）
// agent.Get("claude-desktop").Runner() → ok=false
// codex-app/codex-cli：ok=false（codex headless 未核验，保守置 false；实现者若核验 codex exec 可改）
```

- [ ] **Step 2:** 运行 `go test ./internal/agent/ -run Runner -v` → FAIL
- [ ] **Step 3:** `Adapter` 接口加 `Runner() (RunnerSpec, bool)`；`spec` 加 `runnerCmd string; runnerArgs []string`（空=无 runner）；registry 三个 CLI kind 填入已核验值（opencode run / claude -p / gemini -p），其余空（ok=false）。`RunnerSpec` 类型新增。
- [ ] **Step 4:** 运行 → PASS；`go build ./...` 通过（接口加方法不破其它实现——确认无其它 Adapter 实现）
- [ ] **Step 5:** Commit `feat(agent): per-kind headless runner spec (opencode/claude/gemini)`

### Task 4: 硬测试门 + verify 提取（gate.go）

**Files:**
- Create: `internal/orchestrate/gate.go`
- Test: `internal/orchestrate/gate_test.go`

- [ ] **Step 1: 失败测试**

```go
// extractVerify(specMarkdown) → 提取 frontmatter 或 fenced 的 verify 命令字符串。
//   - spec 含 `verify: "go test ./internal/serve/ -run Relay"` → 返回该命令
//   - 无 verify 字段 → 返回 "" + ok=false（loop 层退化为全量 go test）
// gate 用注入的 cmdExec 接口（不真 exec）：
//   - 命令退出 0 → gate PASS
//   - 命令非 0 → gate FAIL（含 stderr 摘要）
```

- [ ] **Step 2:** 运行 → FAIL
- [ ] **Step 3:** 实现 `extractVerify(spec string) (string, bool)`（读 task 规格文本，支持 `verify:` frontmatter 行 或 ```verify fenced 块——择一约定，测试里固定 frontmatter 行形式）；`type cmdExec interface { Run(ctx, dir, command string) (exitCode int, output string, err error) }`；`runGate(ctx, exec cmdExec, dir, command string) (ok bool, detail string)`。缺命令时调用方传全量回退命令。
- [ ] **Step 4:** 运行 → PASS
- [ ] **Step 5:** Commit `feat(orchestrate): hard test gate + verify extraction` → **Wave 1 收束，开 PR（orchestrate 纯核心）**

### Task 5: Runner 接口 + 真实 exec（runner.go）

**Files:**
- Create: `internal/orchestrate/runner.go`
- Test: `internal/orchestrate/runner_test.go`

- [ ] **Step 1: 失败测试**

```go
// resolveRunner(seat) 用 agent.Get(seat.Kind).Runner() 解析；GUI kind/未知 kind → error。
// CmdRunner.Run 用一个可注入的"exec 函数"（os/exec 包一层），测试断言：
//   - 解析出的命令 = agent runner，{briefing} 被替换为真实简报文本
//   - GUI 座席 → Run 返回明确 error（fail-closed，提示换 CLI 座席/人工）
// 不真起 opencode/claude（测试注入 fake exec 记录被调用的 argv）。
```

- [ ] **Step 2:** 运行 → FAIL
- [ ] **Step 3:** 实现 `Runner` 接口（锚点）；`CmdRunner{ exec func(ctx, name string, args []string, dir string) error }`（生产用 os/exec，测试注入 fake）；`Run` 解析 `agent.Get(seat.Kind).Runner()`，ok=false → error；把 Args 里的 `{briefing}` 替换为简报；`PACT_AGENT_ID=seat.ID` 注入子进程环境（agent 据此 join 正确座席）；在 repo dir 执行。
- [ ] **Step 4:** 运行 → PASS
- [ ] **Step 5:** Commit `feat(orchestrate): Runner interface + real headless exec`

### Task 6: 升级记录 + 通知（escalate.go）

**Files:**
- Create: `internal/orchestrate/escalate.go`
- Test: `internal/orchestrate/escalate_test.go`

- [ ] **Step 1: 失败测试**

```go
// writeEscalation(dir, task, reason, evidence, suggestion) 在 dir/.pact/orchestrate/ 下
//   写 escalation-<n>.md（n 用注入的序号或传入的时间戳字符串，避免 Date.now）；
//   文件含 task/reason/evidence/suggestion 字段。断言文件存在且含这些字段。
// notify(message) 调注入的 notifier（测试 fake 记录消息）；无可用通知器时静默不报错。
```

- [ ] **Step 2:** 运行 → FAIL
- [ ] **Step 3:** 实现 `writeEscalation(dir, ts, task, reason, evidence, suggestion string) (path string, err error)`（ts 由 loop 传入，prep 时拼，避免包内取时间破坏可测）；`notify` 用注入的 `Notifier` 接口（生产可接桌面通知或 stdout，测试 fake）。
- [ ] **Step 4:** 运行 → PASS
- [ ] **Step 5:** Commit `feat(orchestrate): escalation record + notifier`

### Task 7: 主循环（loop.go）

**Files:**
- Create: `internal/orchestrate/loop.go`
- Modify: `internal/pact/engine.go`（加导出 `StateProjection()`）
- Test: `internal/orchestrate/loop_test.go`

- [ ] **Step 1: 失败测试**（集成测试，注入 fake Runner + fake cmdExec，在临时 .pact 项目上端到端）

```go
// 在 t.TempDir() 建一个真 .pact 项目（init 一个 orchestrator + 一个 worker 座席 + 一个 reviewer，
//   assign 一个 feature 含 1-2 个 task，写 spec 含 verify 命令）。
// 注入 fakeRunner：收到 worker 简报 → 在该 repo 跑 pact Checkpoint(task,"ok")；
//   收到 reviewer 简报 → 跑 pact Accept(task)（或前 N 次 Changes 测返工）。
// 注入 fake cmdExec：verify 命令恒 exit 0（gate PASS）。
// 用例：
//  (1) happy path：loop 跑到 feature shipped（断言 STATE feature status==shipped，merge 被调）。
//  (2) 返工回路：fakeRunner reviewer 第一次 Changes、第二次 Accept → loop 正确重拉 worker → 最终 shipped。
//  (3) 升级：fakeRunner reviewer 每次都 Changes，超过 MaxRework → loop 写升级文件 + 暂停返回（断言 escalation 文件存在、未 merge）。
//  (4) 硬门拦截：fake cmdExec verify 恒非 0 → 即便全 accepted 也不 merge → Stuck/升级。
//  (5) dry-run：只产出 Action 序列不调 Runner（断言 fakeRunner 零调用）。
```

- [ ] **Step 2:** 运行 `go test ./internal/orchestrate/ -run Loop -v` → FAIL
- [ ] **Step 3:** 实现：
  - `engine.go` 加 `func (p *Project) StateProjection() (projection.State, error)`（包装 `state()` 取第一返回值）。
  - `loop.go`：`type Options struct{ Dir, Feature string; Th Thresholds; DryRun bool; Run Runner; Exec cmdExec; Notify Notifier; Now func() string }`（依赖全注入，可测）。`Run(ctx, opts) error` 主循环：
    1. 读 state（`pact.At(opts.Dir).StateProjection()`）。
    2. `act := nextAction(state, history, opts.Th)`。
    3. switch：
       - RunOwner：取 task 的最近 changes reason（从 log），`workerBrief` → `opts.Run.Run`（dry-run 跳过）→ 重投影；若 task 状态未进 awaiting_review，Fails[t]++。
       - RunReviewer：`reviewerBrief` → Run → 重投影；若未进 accepted/changes_requested，Fails[t]++；进 changes_requested 则 Rework[t]++。
       - Merge：先硬门——取该 feature 各 task 的 verify（extractVerify(读 spec 文件)，缺则全量回退）→ `runGate`；PASS 则 `pact.At(dir).Merge(feature)`；FAIL 则转升级。
       - Stuck：`writeEscalation` + `notify` + 返回（暂停；`--resume` 即重新 Run，history 从零但 state 已前进，自然续）。
       - Done：返回 nil。
    4. Iters++；循环。
  - changes reason 取法：读 log.jsonl 找该 task 最后一个 changes 事件的 payload reason（小工具函数，可放 loop.go 或复用 projection 的 event 加载）。
- [ ] **Step 4:** 运行 → PASS；`go test ./internal/orchestrate/ ./internal/pact/` 全绿
- [ ] **Step 5:** `npm`/构建无关；Commit `feat(orchestrate): serial driver loop with gate + rework + escalation` → **Wave 2 收束**

### Task 8: CLI 子命令（cmd_orchestrate.go）

**Files:**
- Create: `cmd/pactify/cmd_orchestrate.go`
- Modify: `cmd/pactify/commands.go`（注册 `newOrchestrateCmd()`）
- Test: `cmd/pactify/cli_test.go`（追加 smoke）

- [ ] **Step 1: 失败测试**

```go
// orchestrate --help 含 --feature/--resume/--max-rework/--max-iters/--dry-run。
// 在临时 .pact 项目上 orchestrate --dry-run 不报错且打印将执行的动作（不真拉 agent）。
```

- [ ] **Step 2:** 运行 → FAIL
- [ ] **Step 3:** 实现 `newOrchestrateCmd()`：flags `--feature string`、`--resume bool`、`--max-rework int`（默认 3）、`--max-iters int`（默认 50）、`--dry-run bool`；构造 `orchestrate.Options`（生产 Runner=CmdRunner、Exec=真实 exec、Notify=stdout/桌面、Now=真实时间字符串）调 `orchestrate.Run`。`--resume` 语义=直接重新 Run（state 已前进）。注册进 commands.go 的命令列表。
- [ ] **Step 4:** 运行 → PASS；`go build -o /tmp/pactify ./cmd/pactify && /tmp/pactify orchestrate --help`
- [ ] **Step 5:** Commit `feat(cli): pactify orchestrate command`

### Task 9: 文档 + 终审

**Files:**
- Modify: `docs/architecture.md`、`docs/operations.md`
- Modify: `.agent/CURRENT.md`、`.agent/sprints/sprint-003.md`

- [ ] **Step 1:** architecture.md 加 "orchestrate 驱动器" 小节（中心化串行、nextAction 纯函数、per-kind runner、硬测试门、升级暂停/resume）。operations.md 加用法（写任务图 → `pactify orchestrate --feature X` → 卡住看 `.pact/orchestrate/escalation-*.md` → 修后 `--resume`；GUI 座席不可驱动需换 CLI）。task 规格 `verify:` 字段约定写进 operations。
- [ ] **Step 2:** 全量验证 `go build ./... && go test ./...` 绿；`pactify orchestrate --dry-run` 真机 smoke（在 dogfood 的 relay 任务图或一个新玩具 feature 上）。
- [ ] **Step 3:** sprint T12 条目 + CURRENT.md 状态行（#9 编排驱动 ✅）。
- [ ] **Step 4:** Commit + **开 PR（Wave 3）**，CI 绿合并，重建二进制。

---

## Self-Review 结论

- **spec 覆盖**：§2.1 nextAction→T1；§2.3 简报→T2；§2.2 runner→T3(adapter)+T5(exec)；§2.4 硬门→T4+T7(merge 前调用)；§2.5 升级/resume→T6+T7；§3 文件结构→各 task 落点一致；§4 verify 字段→T4 extractVerify+T9 文档；§6 测试（纯单测+fake runner 集成）→T1-T7 各测 + T7 端到端；§7 工艺（exec 接口化）→T5 Runner/T4 cmdExec 注入。无缺口。
- **占位符扫描**：codex headless 标"保守 false，核验后可改"（非占位，是有据的保守默认）；其余无 TBD。
- **类型一致性**：`Action`/`History`/`Thresholds`/`nextAction`（T1）被 T7 引用一致；`RunnerSpec`/`Adapter.Runner`（T3）被 T5 引用一致；`Runner`/`cmdExec`/`Notifier`（T4/T5/T6）被 T7 Options 注入一致；`StateProjection`（T7 加）被 loop 调用一致；`projection.State/Task/Seat` 字段对照真实代码（project.go:11-30）。
- **有意留白**：reviewer 简报里"跑验收命令"与硬门是两道（agent 自跑 + orchestrate 独立复跑）——冗余是故意的质量底线，非重复劳动。
