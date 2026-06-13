# Task p-apply · manifest → assign

**Feature:** planner · **Owner:** opencode-worker · **Reviewer:** claude · **Deps:** p-manifest

## 目标
把校验通过的 manifest 落成 pact assign。

## 改文件
新建 `internal/planner/apply.go` + `apply_test.go`。

## 契约
```go
package planner
// Apply 校验 plan（Validate + 每个 task 的 Spec 文件在 dir 下存在）后，对每个 task 调
// pact.At(dir).Assign(task.ID, plan.Feature, plan.Branch, task.Owner, task.Reviewer, task.Spec, task.Deps)。
// 先全校验再逐个 assign（任一校验失败则不 assign 任何）。
func Apply(dir string, plan Plan, roster []string) (assigned int, err error)
```
- 校验 = plan.Validate(roster) + 每个 task.Spec 在 dir 下存在（filepath.Join(dir, task.Spec) os.Stat）。
- 任一不过 → 返回 error，不 assign。
- 全过 → 逐个 Assign；中途 Assign 失败 → 返回已 assign 数 + error。
- 引擎入口：`pact.At(dir).As("claude").Assign(...)` 或确认 assign 需要的 PACT_AGENT_ID/As 设置（看 internal/pact/engine.go Assign 的 agentID 要求；测试里 t.Setenv("PACT_AGENT_ID","claude") 或 As）。

## 验收（apply_test.go，临时 .pact 项目，参考 internal/pact 测试建 project + roster）
合法 plan + spec 文件存在 → Apply 成功，StateProjection 显示各 task assigned 且 owner/reviewer/deps 正确；spec 缺失 → error 不 assign；校验失败（owner==reviewer 等）→ error 不 assign。

## verify
```
go test ./internal/planner/ -run Apply
```

## 完成方式
TDD。座席 opencode-worker。`pactify checkpoint p-apply` 附 verify 输出。不自接受。
