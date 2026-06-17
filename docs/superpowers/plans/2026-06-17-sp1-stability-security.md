# SP1 稳定性内核 + 安全横切 Implementation Plan

> **For agentic workers:** 本 plan 用 **orchestrate（pact 任务图）** 执行，非 subagent-driven。
> 步骤用 checkbox（`- [ ]`）跟踪。worker 遵守 TDD：先写失败测试 → 跑红 → 最小实现 → 跑绿 → checkpoint。

**Goal:** 补齐 v1 稳定性与安全地基——错误处理智能分类恢复、plan apply 事务化、merge-STATE 滞后修复、acting-seat 授权基线。

**Architecture:** 四块互不依赖。①③ 改 orchestrate 驱动器 + pact 引擎（claude 直接实现）；②④ 改 serve 后端 + planner（opencode 经 orchestrate）。每块自带 Go 测试，块级 `go test ./...` 绿。

**Tech Stack:** Go 1.22（cobra CLI、net/http ServeMux、内部包 orchestrate/pact/planner/serve/event/projection）。

**Spec:** `docs/superpowers/specs/2026-06-17-sp1-stability-security-design.md`

---

## 文件结构

| Task | 文件 | 职责 |
|---|---|---|
| T1 ① | `internal/orchestrate/loop.go`（改 runOwner）+ 新 `recover.go` + `recover_test.go` | 软失败后 verify 分类：过→补 checkpoint，不过→重试 |
| T2 ③ | `internal/pact/engine.go`（改 Merge）+ `internal/pact/merge_state_test.go` | merge 末尾提交 .pact 变更，HEAD STATE 与工作树一致 |
| T3 ② | `internal/planner/apply.go`（加 ApplyTx）+ `apply_tx_test.go`；`internal/serve/plan.go`（加 handler）+ `plan_apply_test.go` | plan 批量 assign 事务化 + HTTP endpoint |
| T4 ④ | `internal/serve/author.go`（加 requireSeat）+ `internal/serve/agents.go`/`manifests.go`/`registry.go`/`sessions.go`/`recipes.go`/`wire.go`（加闸）+ `acting_seat_test.go` | machine-scoped 端点 seat 闸 + handleWire roster 校验 |

执行映射：**T1/T2 = claude 直接实现**；**T3/T4 = opencode owner + claude reviewer（orchestrate）**。

---

## Task 1 ①：错误处理智能分类恢复（claude）

**pact 元数据**：`owner=claude reviewer=opencode`（若进任务图）/ 实际 **claude 直接实现**。
`verify: go test ./internal/orchestrate/`，`deps: 无`。

**Files:**
- Create: `internal/orchestrate/recover.go`
- Create: `internal/orchestrate/recover_test.go`
- Modify: `internal/orchestrate/loop.go`（`runOwner`，约 209-228）

- [ ] **Step 1: 写失败测试** — `recover_test.go`

```go
package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// soft-fail 后 verify 通过 → 自动补 checkpoint，task 转 awaiting_review，不重烧 worker。
func TestClassifyAndCheckpoint_VerifyPasses(t *testing.T) {
	dir := seedAssignedTask(t) // helper：init+assign 一个 owner=w1 reviewer=r1 的 task t1，spec 带 "verify: true"
	opts := Options{Dir: dir, Exec: okExec{}} // okExec 让 gate 返回 ok=true
	ok := opts.classifyAndCheckpoint(context.Background(), Action{Feature: "F", Task: "t1"}, "w1")
	if !ok {
		t.Fatal("verify 通过应返回 true（已补 checkpoint）")
	}
	st, _ := pactStateProjection(dir) // helper 包装 pact.At(dir).StateProjection()
	if status := taskStatus(st, "F", "t1"); status != "awaiting_review" {
		t.Fatalf("task status=%q, want awaiting_review", status)
	}
}

// soft-fail 后 verify 不过 → 返回 false（驱动继续重试 worker）。
func TestClassifyAndCheckpoint_VerifyFails(t *testing.T) {
	dir := seedAssignedTask(t)
	opts := Options{Dir: dir, Exec: failExec{}} // gate 返回 ok=false
	if opts.classifyAndCheckpoint(context.Background(), Action{Feature: "F", Task: "t1"}, "w1") {
		t.Fatal("verify 不过应返回 false")
	}
}
```

- [ ] **Step 2: 跑测试确认失败** — `go test ./internal/orchestrate/ -run Classify` → FAIL（`classifyAndCheckpoint` 未定义）。注：`seedAssignedTask`/`pactStateProjection`/`taskStatus` helper 若缺则在 `recover_test.go` 内补（复用 `runner_test.go`/`loop_test.go` 既有 seed 风格）。

- [ ] **Step 3: 实现** — `recover.go`

```go
package orchestrate

import (
	"context"

	"github.com/agentjoey/pactify/internal/pact"
)

// classifyAndCheckpoint 在 worker 软失败后、计入失败前尝试分类恢复：跑该 task 的
// verify 命令，通过则代表「活已干完、只是 checkpoint 没打」——以 owner 身份补一个
// checkpoint（带 gate 输出作 evidence），task 进 awaiting_review，返回 true。
// 不过/出错返回 false，调用方继续重试 worker（活没干完）。
func (opts Options) classifyAndCheckpoint(ctx context.Context, act Action, ownerSeat string) bool {
	cmds := opts.gateCommands(act.Feature) // 既有：读 spec → extractVerify，无则 fallbackGate
	cmd := cmds[act.Task]
	if cmd == "" {
		return false // 无可判定的 verify → 不短路，照常重试
	}
	ok, detail := runGate(ctx, opts.Exec, opts.Dir, cmd)
	if !ok {
		return false
	}
	ev := "verify passed on recovery:\n" + summarize(detail)
	if err := pact.At(opts.Dir).As(ownerSeat).Checkpoint(act.Task, ev); err != nil {
		return false
	}
	return true
}
```

> 复用既有：`gateCommands`（loop.go:348）、`runGate`（gate.go:71）、`summarize`（gate.go:84）、`pact.Checkpoint`（engine.go:337）。若 `gateCommands` 签名是 per-feature map，按其实际签名取 `act.Task` 项。

- [ ] **Step 4: 接入 runOwner** — `loop.go` 软失败分支（约 213-217），在 `h.Fails[act.Task]++` 前：

```go
		// 软失败：先分类恢复——verify 已过则补 checkpoint，不重烧 worker。
		if opts.classifyAndCheckpoint(ctx, act, ownerSeat); /* 见下 */ true {
			after, _ := pact.At(opts.Dir).StateProjection()
			if _, t, ok := find(after, act.Feature, act.Task); ok && t.Status == "awaiting_review" {
				h.Fails[act.Task] = 0
				return nil
			}
		}
		h.Fails[act.Task]++
		return nil
```

> 用 `ownerSeat`（runOwner 已有的 owner 座席变量名，按实际改）。若 classify 补了 checkpoint，重投影即见 awaiting_review → 清零失败、return；否则落到 `h.Fails++` 原路径。

- [ ] **Step 5: 跑测试确认通过** — `go test ./internal/orchestrate/ -run Classify` → PASS。

- [ ] **Step 6: 全包回归** — `go test ./internal/orchestrate/` → 全绿（现有 loop/escalate 测试不破）。

- [ ] **Step 7: checkpoint/commit**

```bash
git add internal/orchestrate/recover.go internal/orchestrate/recover_test.go internal/orchestrate/loop.go
git commit -m "feat(orchestrate): smart-classify worker soft-fail — verify-pass auto-checkpoints instead of re-burning worker"
```

---

## Task 2 ③：merge-STATE 滞后修复（claude）

**pact 元数据**：`owner=claude reviewer=opencode`/ 实际 **claude 直接实现**。
`verify: go test ./internal/pact/`，`deps: 无`。

**Files:**
- Modify: `internal/pact/engine.go`（`Merge`，231-270）
- Create: `internal/pact/merge_state_test.go`

- [ ] **Step 1: 写失败测试** — `merge_state_test.go`

```go
package pact

import (
	"os/exec"
	"strings"
	"testing"
)

// merge 后 HEAD 提交里的 STATE.yml 必须与工作树一致（feature=shipped），
// 不能滞后在 in_progress（根因：merge 事件 append 在 git merge commit 之后且未提交）。
func TestMerge_HEADStateMatchesWorktree(t *testing.T) {
	dir := seedMergeableFeature(t) // helper：init+assign+checkpoint+accept 一个 feature F 到可 merge 态（独立分支）
	if err := At(dir).As("claude").Merge("F"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// 工作树 STATE
	wt := readFile(t, dir+"/.pact/STATE.yml")
	// HEAD 提交的 STATE
	out, err := exec.Command("git", "-C", dir, "show", "HEAD:.pact/STATE.yml").Output()
	if err != nil {
		t.Fatalf("git show: %v", err)
	}
	head := string(out)
	if !strings.Contains(head, "shipped") {
		t.Fatalf("HEAD STATE 未含 shipped（滞后）:\n%s", head)
	}
	if strings.TrimSpace(head) != strings.TrimSpace(wt) {
		t.Fatalf("HEAD STATE 与工作树不一致\nHEAD:\n%s\nWT:\n%s", head, wt)
	}
}
```

- [ ] **Step 2: 跑测试确认失败** — `go test ./internal/pact/ -run TestMerge_HEADState` → FAIL（HEAD STATE 滞后）。helper `seedMergeableFeature`/`readFile` 缺则按 `engine` 既有测试 seed 风格补。

- [ ] **Step 3: 实现** — `engine.go` `Merge`，把 `appendAndRender`（266-269）改为 append 后再提交 .pact：

```go
	if err := p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("merge"), EventType: "merge",
		Feature: feature, Payload: map[string]any{},
	}); err != nil {
		return err
	}
	// 把 merge 事件 + 重算后的 STATE.yml（shipped）落进 HEAD，使提交与工作树一致。
	// （此前 appendAndRender 只写工作树不提交，导致 merge commit 的 STATE 滞后。）
	if ch, _ := gitx.HasChanges(p.dir); ch {
		return gitx.CommitAll(p.dir, "pact "+feature+": merge (state shipped)")
	}
	return nil
```

> 复用 `gitx.HasChanges`/`gitx.CommitAll`（Merge 内已用于 ledger）。该提交是 Merge 最后一步。

- [ ] **Step 4: 跑测试确认通过** — `go test ./internal/pact/ -run TestMerge_HEADState` → PASS。

- [ ] **Step 5: 全包回归** — `go test ./internal/pact/` → 全绿（serial in-place no-op merge、并行 worktree merge 测试不破）。

- [ ] **Step 6: commit**

```bash
git add internal/pact/engine.go internal/pact/merge_state_test.go
git commit -m "fix(pact): commit merge event + shipped STATE so HEAD matches worktree (post-merge STATE lag)"
```

---

## Task 3 ②：plan apply 事务化（opencode）

**pact 元数据**：`owner=opencode reviewer=claude`，`branch=feat-sp1-plan-apply`，
`spec=.pact/tasks/t-plan-apply.md`，`verify: go test ./internal/planner/ ./internal/serve/`，`deps: 无`。

**Files:**
- Modify: `internal/planner/apply.go`（加 `ApplyTx`）
- Create: `internal/planner/apply_tx_test.go`
- Modify: `internal/serve/plan.go`（`registerPlanRoutes` 加路由 + `handlePlanApply`）
- Create: `internal/serve/plan_apply_test.go`

- [ ] **Step 1: 写 ApplyTx 失败测试** — `apply_tx_test.go`

```go
package planner

import (
	"os"
	"path/filepath"
	"testing"
)

// 合法 plan：全部 assign，返回数量 = task 数。
func TestApplyTx_AllOrNothing_Success(t *testing.T) {
	dir, plan, roster := seedPlan(t, 3) // helper：init+写 3 个 spec 文件 + 构造合法 Plan
	n, err := ApplyTx(dir, plan, roster, "claude")
	if err != nil || n != 3 {
		t.Fatalf("ApplyTx=(%d,%v), want (3,nil)", n, err)
	}
}

// 非法 plan（含不存在的 dep）：预检阶段整体拒绝，log.jsonl 零新增。
func TestApplyTx_RejectsAtomically(t *testing.T) {
	dir, plan, roster := seedPlan(t, 3)
	plan.Tasks[2].Deps = []string{"nonexistent"} // 制造非法
	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	before, _ := os.ReadFile(logPath)
	if _, err := ApplyTx(dir, plan, roster, "claude"); err == nil {
		t.Fatal("非法 plan 应整体失败")
	}
	after, _ := os.ReadFile(logPath)
	if len(after) != len(before) {
		t.Fatalf("log.jsonl 不应有新增：before=%d after=%d", len(before), len(after))
	}
}
```

- [ ] **Step 2: 跑测试确认失败** — `go test ./internal/planner/ -run ApplyTx` → FAIL（`ApplyTx` 未定义）。helper `seedPlan` 缺则在测试文件内补（参考 `apply.go` 既有测试）。

- [ ] **Step 3: 实现 ApplyTx** — `apply.go`

```go
// ApplyTx assigns 整个 plan 的任务图，事务化：先对全部 task 预检（checkAssign+checkDeps
// 基于累积投影），任一不过则零写入返回错误；预检全过后记录 log 字节大小逐个 assign，
// 中途 append 失败则截断回滚到原大小并重算 STATE。seat = 执行 assign 的座席。
func ApplyTx(dir string, plan Plan, roster []string, seat string) (assigned int, err error) {
	if err := plan.Validate(roster); err != nil {
		return 0, fmt.Errorf("applytx: %w", err)
	}
	for _, t := range plan.Tasks { // spec 文件存在性预检
		if _, err := os.Stat(filepath.Join(dir, t.Spec)); err != nil {
			return 0, fmt.Errorf("applytx: spec %q: %w", t.Spec, err)
		}
	}
	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	orig, _ := os.Stat(logPath)
	var origSize int64
	if orig != nil {
		origSize = orig.Size()
	}
	p := pact.At(dir).As(seat)
	for _, t := range plan.Tasks {
		if err := p.Assign(t.ID, plan.Feature, plan.Branch, t.Owner, t.Reviewer, t.Spec, t.Deps); err != nil {
			_ = os.Truncate(logPath, origSize)      // 回滚已 append
			_ = rerenderState(dir)                   // 重算 STATE 与回滚后的 log 一致
			return assigned, fmt.Errorf("applytx: assign %s rolled back: %w", t.ID, err)
		}
		assigned++
	}
	return assigned, nil
}
```

> `rerenderState(dir)`：读 `.pact/log.jsonl` → `projection.Project` → `projection.WriteState`（参考 engine.go:49-58 的 appendAndRender 末段；若 planner 不便 import 引擎私有，新增一个 `internal/pact` 导出 helper `RerenderState(dir)` 并在此调用）。imports 补 `fmt os path/filepath` + `internal/pact` + `internal/projection`/`internal/paths`。

- [ ] **Step 4: 跑 ApplyTx 测试确认通过** — `go test ./internal/planner/ -run ApplyTx` → PASS。

- [ ] **Step 5: 写 endpoint 失败测试** — `plan_apply_test.go`

```go
package serve

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 合法 plan apply：200 + {assigned:N}；无 acting-seat：422。
func TestHandlePlanApply(t *testing.T) {
	root := seedProjectWithPlan(t) // helper：init+roster+写 .pact/plan-F.json + spec 文件
	srv := New(projectsAt(root))
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, _ := http.Post(ts.URL+"/api/projects/p/plan/F/apply", "application/json", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	// 无 seat → 422
	srv2 := New(projectsAt(root)) // 未 SetSeat
	ts2 := httptest.NewServer(srv2.Handler())
	defer ts2.Close()
	r2, _ := http.Post(ts2.URL+"/api/projects/p/plan/F/apply", "application/json", nil)
	if r2.StatusCode != 422 {
		t.Fatalf("no-seat status=%d, want 422", r2.StatusCode)
	}
}
```

- [ ] **Step 6: 跑测试确认失败** — `go test ./internal/serve/ -run HandlePlanApply` → FAIL（404，路由不存在）。

- [ ] **Step 7: 实现 handler** — `plan.go`，`registerPlanRoutes` 加 `mux.HandleFunc("POST /api/projects/{id}/plan/{feature}/apply", s.handlePlanApply)`，新增：

```go
func (s *Server) handlePlanApply(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, dir, ok := s.project(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	if _, err := s.actingProject(dir); err != nil { // seat 配置 + ∈ roster
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	feature := r.PathValue("feature")
	b, err := os.ReadFile(filepath.Join(dir, ".pact", "plan-"+feature+".json"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "plan manifest not found")
		return
	}
	plan, err := planner.Parse(b)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	mu := s.projectMu(id)
	mu.Lock()
	defer mu.Unlock()
	n, err := planner.ApplyTx(dir, plan, rosterOf(dir), s.seat)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"assigned": n})
}
```

> 复用 `s.project`/`s.actingProject`/`s.projectMu`/`rosterOf`/`writeErr`/`writeJSON`/`planner.Parse`。imports 补 `os path/filepath`。

- [ ] **Step 8: 跑测试确认通过** — `go test ./internal/serve/ -run HandlePlanApply` → PASS。

- [ ] **Step 9: 全包回归 + checkpoint** — `go test ./internal/planner/ ./internal/serve/` → 全绿。

```bash
git add internal/planner/apply.go internal/planner/apply_tx_test.go internal/serve/plan.go internal/serve/plan_apply_test.go
git commit -m "feat(serve): transactional plan apply endpoint (POST .../plan/{feature}/apply, all-or-nothing)"
```

---

## Task 4 ④：acting-seat 授权基线（opencode）

**pact 元数据**：`owner=opencode reviewer=claude`，`branch=feat-sp1-acting-seat`，
`spec=.pact/tasks/t-acting-seat.md`，`verify: go test ./internal/serve/`，`deps: 无`。

**Files:**
- Modify: `internal/serve/author.go`（加 `requireSeat` helper）
- Modify: `internal/serve/agents.go` `manifests.go` `registry.go` `sessions.go` `recipes.go` `wire.go`（machine 端点加闸；wire 加 actingProject）
- Create: `internal/serve/acting_seat_test.go`

- [ ] **Step 1: 写失败测试** — `acting_seat_test.go`

```go
package serve

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// machine-scoped 端点在无 acting-seat 时 422 fail-closed。
func TestMachineEndpoints_RequireSeat(t *testing.T) {
	srv := New(nil) // 未 SetSeat
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	for _, path := range []string{
		"/api/agents/opencode/sessions/prune",
		"/api/manifests",
		"/api/recipes/add-tests/expand",
	} {
		resp, _ := http.Post(ts.URL+path, "application/json", nil)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s no-seat status=%d, want 422", path, resp.StatusCode)
		}
	}
}

// 配置 seat（无需 roster）→ machine 端点放行（不再因 seat 闸 422）。
func TestMachineEndpoints_PassWithSeat(t *testing.T) {
	srv := New(nil)
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, _ := http.Post(ts.URL+"/api/recipes/add-tests/expand", "application/json", nil)
	if resp.StatusCode == http.StatusUnprocessableEntity {
		t.Fatal("配置 seat 后不应再因 seat 闸 422")
	}
}
```

- [ ] **Step 2: 跑测试确认失败** — `go test ./internal/serve/ -run MachineEndpoints` → FAIL（无 seat 也 200/其他，未 fail-closed）。

- [ ] **Step 3: 实现 requireSeat** — `author.go`

```go
// requireSeat 是 machine-scoped 副作用端点的最低授权闸：要求已配置 acting seat
// （不要求 ∈ roster——machine 操作非 project 概念）。未配置 → 写 422 并返回 false。
func (s *Server) requireSeat(w http.ResponseWriter) bool {
	if s.seat == "" {
		writeErr(w, http.StatusUnprocessableEntity, "no acting seat configured (set --seat or PACT_AGENT_ID)")
		return false
	}
	return true
}
```

- [ ] **Step 4: machine 端点加闸** — 在每个 machine-scoped handler 开头加：

```go
	if !s.requireSeat(w) {
		return
	}
```

应用到：`handleAgentRegister`/`handleAgentUnregister`/`handleAgentConfigSet`（agents.go）、`handleManifestCreate`/`handleManifestDelete`（manifests.go）、`handleRegistryAdd`/`handleRegistryDelete`（registry.go）、`handleSessionsPrune`（sessions.go）、`handleRecipeExpand`（recipes.go）。

- [ ] **Step 5: handleWire 加 project 校验** — `wire.go` `handleWire` 写 `.pact`，改用 `actingProject`：

```go
	if _, err := s.actingProject(dir); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
```

> 放在解析 `dir` 之后、执行 wire 之前。`handleWire` 是 project-scoped（写该 project 的 .pact），故用 `actingProject`（含 roster 校验）而非 `requireSeat`。

- [ ] **Step 6: 跑测试确认通过** — `go test ./internal/serve/ -run MachineEndpoints` → PASS。

- [ ] **Step 7: 全包回归 + checkpoint** — `go test ./internal/serve/` → 全绿（现有 author/agents/manifests 测试若因新增 seat 闸需 SetSeat，相应补齐）。

```bash
git add internal/serve/author.go internal/serve/agents.go internal/serve/manifests.go internal/serve/registry.go internal/serve/sessions.go internal/serve/recipes.go internal/serve/wire.go internal/serve/acting_seat_test.go
git commit -m "feat(serve): acting-seat baseline — requireSeat gate on machine endpoints, actingProject on wire"
```

---

## Pact 任务图（orchestrate 用）

T3/T4 转 pact 任务图（claude orchestrator+reviewer，opencode owner）：

```bash
# spec 文件 .pact/tasks/t-plan-apply.md / t-acting-seat.md = 上面 Task 3/4 的内容 + 末行 verify:
#   t-plan-apply.md  末行: verify: go test ./internal/planner/ ./internal/serve/
#   t-acting-seat.md 末行: verify: go test ./internal/serve/
pactify assign t-plan-apply  --feature sp1-backend --branch feat-sp1-plan-apply --owner opencode --reviewer claude --spec .pact/tasks/t-plan-apply.md
pactify assign t-acting-seat --feature sp1-backend --branch feat-sp1-plan-apply --owner opencode --reviewer claude --spec .pact/tasks/t-acting-seat.md
pactify orchestrate --feature sp1-backend   # claude 驱动 + 每棒 reviewer 独立重跑 verify
```

T1/T2（claude 直接实现）不入任务图，claude 在 orchestrate 期间/前后自做自测。两块独立、无 deps，可与 sp1-backend 并行。

---

## Self-Review

**Spec 覆盖**：① recover.go+runOwner（T1）✓；② ApplyTx+endpoint（T3）✓；③ Merge commit（T2）✓；④ requireSeat+wire（T4）✓。无遗漏。
**Placeholder**：每步有实测代码 + 实现骨架 + verify 命令，无 TBD。helper（seed*）明确「缺则按既有风格补」。
**类型一致**：`classifyAndCheckpoint(ctx, Action, seat) bool`、`ApplyTx(dir, Plan, roster, seat)(int,error)`、`requireSeat(w) bool` 在测试与实现中一致；`PlanTask` 字段名（ID/Owner/Reviewer/Spec/Verify/Deps）与 spec 一致。
