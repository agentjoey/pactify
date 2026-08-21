# exec-tiering-evidence — 迭代帽升级记录必须带 per-task 证据

tier: L1
verify: go test ./internal/orchestrate/
dimension: correctness

## 目标 / Goal

全局迭代帽触发时，升级记录当前丢弃了驱动**已经握在手里**的 per-task 上下文，
只写一句 `"(global cap)"`。这逼着 orchestrator 重读整个项目状态才能诊断
（实测 tradelinks：连续 9 次升级全是同一句无信息量的话，而 `history` 里明明
存着 `legacy-backfill → "worker run failed: exit status 1"`）。

本任务让迭代帽的升级记录**枚举未完结任务及其已记录的失败原因**。

## 背景（不要重新调查，这些已核实）

- 检查顺序**已经是对的**：`tripped()`（per-task 失败/返工，`loop.go:437`）
  运行在全局 `MaxIters` 帽（`loop.go:479`）**之前**。**不要去调换顺序。**
- 真实缺陷：`Fails` 是**连续**失败计数，任何进展都会清零
  （`h.Fails[act.Task] = 0 // the review ran (progress)`），所以"半推进"的 run
  永远累积不到 `MaxFails`，`tripped()` 从不触发，最后撞全局帽——而那条路径
  把 `h` 里的 `LastFail` / `LastClass` / 返工数 / 未完结任务全丢了。

## 改文件 / Files

只允许改这两个文件：

- `internal/orchestrate/loop.go` — 迭代帽调用点 + 新增一个纯函数 helper
- `internal/orchestrate/exec_tiering_evidence_test.go` —（新建）测试

**不要**改 `escalate()` 的签名、`writeEscalation` 的模板、`History` 结构、
或任何其它文件。

## 契约 / Contract

新增一个**纯函数**（无 IO，可单测）：

```go
// capEvidence renders the per-task context the driver already holds when the
// global iteration cap fires: every unfinished task with its recorded last
// failure and class, so an operator can diagnose without re-reading the project.
// Returns "(global cap)" unchanged when there is nothing more to say.
func capEvidence(view projection.State, h History) string
```

要求：

1. 遍历 `view` 里所有**未完结**的 task（状态不是 `accepted`；跳过 `shipped` feature）。
2. 每条输出：task id、status、`h.LastFail[task]`（有则带上）、
   `h.LastClass[task]`（有则带上）、`h.Rework[task]`（>0 则带上）。
3. 没有任何未完结 task 时，返回原来的 `"(global cap)"`（保持现状）。
4. **输出必须有界**：最多列 20 条 task，超出追加 `… (+N more)`；
   单条 `LastFail` 文本截断到 200 字符。理由：升级记录不能变成新的账本膨胀源。

调用点（`loop.go` 迭代帽处）把第 4 个实参从字面量 `"(global cap)"`
换成 `capEvidence(view, h)`。`reason` 与 `suggestion` 两个实参保持不变。

## 验收 / Acceptance

评审维度：**correctness**。

- `capEvidence` 在有未完结 task + 有 `LastFail` 记录时，输出包含该 task id
  和该失败原因原文。
- **回归（tradelinks 形状）**：构造 `h.Fails` 全为 0（被进展清零）但
  `h.LastFail` 有值的 History，断言输出仍能诊断——即包含真实失败原因，
  而不是只有 `"(global cap)"`。
- 无未完结 task 时返回 `"(global cap)"`。
- 有界性：>20 条 task 时输出被截断且带 `(+N more)`；超长 `LastFail` 被截断。
- 全包测试绿：`go test ./internal/orchestrate/`（不得让任何既有测试变红）。
