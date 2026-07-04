# obs-runduration：RUN 时长在 changes_requested（返工期）不再冻结（OBS-1）

> 来源 backlog OBS-1 / code-review-2026-07-02 §100。先读 `web/src/lib/derive.ts`（RUN 时长/task runtime 计算，含 `changes_requested`/`awaiting_review` 分支）与 `internal/serve/stats.go`（若有对应时长归因）。

## 问题
任务在 `changes_requested`（reviewer 打回、worker 返工）期间是**活跃工作期**，但当前 RUN/task 时长计算把这段算作停滞（时长冻结），低估了真实工作时长。

## 目标
让 RUN/task 时长把 `changes_requested`（返工）期计为**活跃**（end=now 或视同 in_progress），与 `in_progress`/`awaiting_review` 一致地累计，而不是冻结。

1. **定位**：`derive.ts` 里推导 task runtime / RUN 时长的函数（按 assign→checkpoint→accept 事件时间轴推导活跃窗口的逻辑）。确认 `changes_requested` 当前如何被处理（大概率被当作终止/停滞点）。
2. **修**：把 `changes_requested` 状态视为活跃工作态——即该状态下时长继续计到 now（或到下一个 checkpoint）。若 `internal/serve/stats.go` 有并行的时长归因逻辑，同步修正保持一致。
3. **测试**：加/改单测覆盖「任务经历 changes_requested 后时长继续增长、不冻结」；若改了 stats.go，加对应 Go 测试。

## 文件
- 改 `web/src/lib/derive.ts`（主）；如相关，`internal/serve/stats.go`
- 相应 `web/src/lib/derive.test.ts`（或新增）+ 若动 Go 则 `internal/serve/*_test.go`

## 纪律
- **不破坏现有时长语义**：in_progress/awaiting_review/accepted 的既有计算保持不变，只修 changes_requested 冻结。
- 纯推导逻辑，`nowMs` 显式传参（不隐式 `Date.now()`）便于测试（若 derive 已有此约定，遵守）。
- 完成跑 verify 绿再 checkpoint。

verify: cd web && npx tsc --noEmit && npx vitest run src/lib/derive
