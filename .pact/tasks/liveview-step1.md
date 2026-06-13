# liveview-step1 — orchestrate emits runtime status

verify: go test ./internal/orchestrate/

## 目标 / Goal
让 `pactify orchestrate` 在驱动 pact 状态机的过程中**吐出运行态**：把"当前在跑哪个 task、哪个座席、动作/阶段、进度、是否升级"持续写到一个机器可读的状态文件 `.pact/orchestrate/status.json`。这是整条 liveview 链的数据源 —— serve 端点 (step2) 和前端面板 (step3) 都消费它。

这是本链最复杂的一棒：要在现有 serial loop 里插入 status 投影点，且不能破坏 loop 的确定性可测性。

## 改文件 / Files
仅可触碰以下文件（bounded set）：
- `internal/orchestrate/status.go`（新建）—— Status 结构 + 原子写入器 + 进度计算 helper。
- `internal/orchestrate/status_test.go`（新建）—— 单元测试。
- `internal/orchestrate/loop.go`（改）—— 在 loop 的每个动作分支与终态（done/escalate）处调用 status 写入器。
- `internal/orchestrate/loop_test.go`（改，仅在需要时）—— 补一条断言：跑一轮后 `.pact/orchestrate/status.json` 存在且字段正确。

禁止改动 serve / web / cmd 任何文件。

## 契约 / Contract
在 `internal/orchestrate/status.go` 定义（JSON tag 必须与此处完全一致——step2/step3 据此对接）：

```go
// Status is the machine-readable runtime snapshot the orchestrate loop emits to
// .pact/orchestrate/status.json on every iteration and at terminal states.
type Status struct {
	Feature   string `json:"feature"`              // feature being driven ("" = all)
	Task      string `json:"task"`                 // current task id ("" if none)
	Seat      string `json:"seat"`                 // acting seat id for this action
	Action    string `json:"action"`               // run_owner|run_reviewer|merge|stuck|idle|done
	Phase     string `json:"phase"`                // human-readable, e.g. "owner working"
	Escalated bool   `json:"escalated"`            // true when paused for a human
	Reason    string `json:"reason,omitempty"`     // escalation/stuck reason when Escalated
	Done      bool   `json:"done"`                 // run finished — all targeted work shipped
	Total     int    `json:"total"`                // total tasks in scope
	Accepted  int    `json:"accepted"`             // tasks already accepted
	Iter      int    `json:"iter"`                 // loop iteration counter
	UpdatedAt string `json:"updated_at"`           // RFC3339 timestamp
}

// writeStatus atomically writes s to <dir>/.pact/orchestrate/status.json
// (temp file + rename), creating the orchestrate dir if absent.
func writeStatus(dir string, s Status) error
```

- 时间戳：用 loop 已有的注入式时钟习惯。若 `Options.Now != nil` 用它，否则 `time.Now().UTC().Format(time.RFC3339)`。保持包内纯净、可确定测试。
- 进度：新增 helper（如 `progress(view projection.State) (total, accepted int)`），从 `opts.filtered(st)` 后的 view 统计任务总数与 `status == "accepted"` 的数量。
- 写入点（在 `run(ctx)` 中，DryRun 时**不写**）：
  - 每轮在确定 `act` 之后、dispatch 之前，写一条对应该动作的 Status（Action/Task/Seat/Phase/Iter/Total/Accepted）。
  - `ActDone` 返回前写 `{Action:"done", Done:true}`。
  - 任一 `escalate(...)` 触发处，对应写 `{Escalated:true, Reason:..., Action:"stuck"}`（建议在 `escalate` 方法里集中写一次，保证所有升级路径都覆盖）。
- 原子写：先写 `status.json.tmp` 再 `os.Rename`，避免读者读到半截 JSON。
- 写 status 失败不得让整个 loop 崩溃——status 是观测投影，不是事务源；写失败记日志/忽略即可（与 `log.jsonl` 源、`STATE.yml` 投影的关系一致）。

## 验收 / Acceptance
Reviewer 确认：
1. `go test ./internal/orchestrate/` 全绿。
2. status.go 的 JSON tag 与上面契约逐字一致。
3. loop 跑一轮（fake runner）后 `.pact/orchestrate/status.json` 存在、可被 `json.Unmarshal` 解析，且 Action/Task/Total/Accepted 合理；DryRun 路径不写文件。
4. 升级路径（rework/fail/gate）写出的 status `escalated:true` 且带 `reason`。
5. 未引入对 serve/web 的依赖；loop 的既有测试不回归。
