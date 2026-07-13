---
id: phase0-h5
feature: phase0-sec
owner: kimi-worker
reviewer: claude
---

# phase0-h5 — 修复 H5：ApplyTx 回滚 truncate 删并发事件（high, data-integrity）

## 目标 / Goal
`internal/planner/apply.go` 的 `ApplyTx` 在**无锁**下捕获 `origSize = stat(log).Size()`，失败时 `os.Truncate(log, origSize)` 回滚——但每个 `Assign` 各自取/放 ledger 锁，锁有间隙。并发写者在 origSize 捕获后、truncate 前 append 的事件会被 truncate 删掉（"pact 落后于 git" 腐化）。

**核心洞见**：truncate 到 origSize 本身没错，错在**没持续持锁**。修法 = 让整个批量 apply 罩在**一把** `withLedgerLock` 下——持锁期间无并发写者能 append，于是 truncate 到 origSize 变安全。

## 改文件 / Files
- `internal/pact/engine.go`（新增导出的批量方法）
- `internal/planner/apply.go`（改为委托给它）

## 契约 / Contract
1. 在 `internal/pact` 加一个**导出**方法（如 `func (p *Project) ApplyBatch(joins []JoinSpec, assigns []AssignSpec) (int, error)`，具体类型自定），它在**一把** `withLedgerLock(func() error { ... })` 内：
   - 捕获 origSize；
   - 逐个 `joinWithClientLocked(...)`（新座席，参考现有 `joinNewSeats` 的调用）；
   - 逐个 `assignLocked(...)`（参考现有 `Assign` 如何把单 reviewer 映射成 `reviewers=[reviewer], quorum=...` 调 `assignLocked`）；
   - 任一失败 → `os.Truncate(logPath, origSize)` + rerender + 返回 err（**此处 truncate 安全**，因为全程持锁、无并发写）；
   - 全成功 → 返回 assigned 数。
   > 关键：全程只取一次锁、内部用 `*Locked` 变体（**不要**再调会自己取锁的 `Assign`/`Join`，否则同进程重入死锁）。
2. `planner.ApplyTx` 改为：做完 `plan.Validate` + spec 存在性预检后，构造 joins/assigns 委托给 `p.ApplyBatch`，**删掉**自己的 `os.Truncate` 回滚 loop。
3. 保持行为等价：成功 assign 数、失败原子回滚、动态座席 join——都不变，只是现在持锁原子。

## 验收 / Acceptance（dimension: correctness）
- `go test ./internal/planner/ -run TestSEC_H5 -count=1` **绿**（现红：并发事件被删）。连跑两次都绿（非 flaky）。
- 既有 `TestApplyTx_*` / `TestApply*` / pact engine 测试**全不破**。
- `go build ./...` 通过；`go vet ./internal/planner/ ./internal/pact/` 干净；`go test ./internal/pact/ ./internal/planner/ -race` 绿。

verify: go test ./internal/planner/ ./internal/pact/ -run 'TestSEC_H5|ApplyTx|Assign|Apply|Init' -count=1 && go build ./...
