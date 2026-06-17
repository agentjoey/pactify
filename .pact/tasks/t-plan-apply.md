# t-plan-apply：plan apply 事务化（SP1 块②）

> 完整 TDD 步骤（含全部测试代码 + 实现骨架）见
> `docs/superpowers/plans/2026-06-17-sp1-stability-security.md` 的 **Task 3**。先读它。

## 目标
新建 `POST /api/projects/{id}/plan/{feature}/apply`：一次性 assign 一个 feature 的整张
任务图，**事务化**——预检全过才写入，中途失败截断回滚，绝不留半成品。

## 文件
- 改 `internal/planner/apply.go`：加 `ApplyTx(dir string, plan Plan, roster []string, seat string) (assigned int, err error)`
- 建 `internal/planner/apply_tx_test.go`
- 改 `internal/serve/plan.go`：`registerPlanRoutes` 加路由 + `handlePlanApply`
- 建 `internal/serve/plan_apply_test.go`

## 实现要点（细节见 plan Task 3）
- `ApplyTx`：`plan.Validate(roster)` → 每个 task spec 文件存在性预检 → 记录 `.pact/log.jsonl`
  字节大小 → 逐个 `pact.At(dir).As(seat).Assign(...)`；任一失败 `os.Truncate(logPath, origSize)`
  回滚 + 重算 STATE（projection.WriteState）后返回错误。
- `handlePlanApply`：`s.project(id)`(404) → `s.actingProject(dir)`(422) → 读 `.pact/plan-<feature>.json`
  → `planner.Parse`(400) → 持 `s.projectMu(id)` → `planner.ApplyTx(dir, plan, rosterOf(dir), s.seat)`
  → 200 `{assigned:N}`。复用 writeErr/writeJSON/rosterOf。

## 纪律
- **TDD**：先写失败测试 → 跑红 → 最小实现 → 跑绿 → checkpoint。
- 复用现有函数，勿重造。完成后跑 verify 命令必须全绿再 checkpoint。

verify: go test ./internal/planner/ ./internal/serve/
