# exec-tiering-budget — 验证预算按 tier 派生（L1 等于今天）

tier: L2
verify: go test ./internal/orchestrate/
dimension: correctness

## 目标 / Goal

今天 `MaxFixRounds` / `MaxRework` / `MaxFails` / critic / QA 全是**全局**的：
一个改文案的 L0 任务和一次架构迁移吃同一套预算——L0 白白付一次 critic 棒
和一次 QA 棒。本任务让这些预算**按 task 的 tier 派生**。

依赖：`exec-tiering-parse`（提供 `Tier` / `extractTier`）。
设计依据：`docs/specs/execution-tiering.md` §5。

## 改文件 / Files

- `internal/orchestrate/` — 预算解析 + 消费点（`tripped`、fix 环、critic/QA 门）
- `cmd/pactify/cmd_orchestrate.go` — 记录哪些 flag 是**显式**设置的
- 对应测试

**不要**改账本、协议、reviewer 流程。

## 契约 / Contract

### 1. tier → 预算表

| Tier | fix rounds | MaxRework | MaxFails | critic | QA |
|---|---:|---:|---:|---|---|
| L0 | 1 | 2 | 2 | 关 | 关 |
| L1 | **2** | **3** | **2** | **关** | 按 `qa:` 行 |
| L2 | 2 | 3 | 2 | 开（若已配置） | 按 `qa:` 行 |
| L3 | 3 | 4 | 3 | 开（若已配置） | 按 `qa:` 行 |

**L1 必须逐字节等于今天的默认值**（`MaxFixRounds`=2、`MaxRework`=3、
`MaxFails`=2、critic 仅在显式配置时开）。

### 2. 解析优先级（从高到低）

1. **显式** CLI flag（`--max-fix-rounds` / `--max-rework` / `--max-fails` / `--critic`）
2. tier 派生值
3. 现有默认值

关键实现点：Go 的 flag 默认值与"用户显式设成同一个值"不可区分。**必须**用
cobra 的 `cmd.Flags().Changed("<name>")` 在 CLI 层记录显式性，再传进
Options；不要用哨兵值（0 在这里是合法语义：`MaxFixRounds=0` 表示禁用自修）。

### 3. 消费点

```go
// budgetFor resolves this task's effective budget from its tier, with explicit
// operator flags taking precedence. Pure function — no IO.
func (opts Options) budgetFor(t Tier) Budget
```

`Budget` 至少含 `FixRounds, MaxRework, MaxFails int` 与 `Critic, QA bool`。
`tripped()` 与 fix-until-green 环、critic/QA 门改为消费该 task 的 Budget，
而不是全局 `opts.Th` / `opts.MaxFixRounds`。

### 4. reviewer 永不被 tier 门控

L0 跳过的只有**可选的** critic 棒与 QA 棒。**reviewer 棒必须照跑**——
"worker 不能自接受"是协议不变量，不是预算项。任何让 L0 跳过 reviewer 的
实现都是错的。

## 验收 / Acceptance

评审维度：**correctness**。

- `budgetFor(L1)` 的每个字段等于今天的默认值（钉死回归）。
- `budgetFor(L0)`：FixRounds=1、Critic=false、QA=false。
- `budgetFor(L3)`：FixRounds=3、MaxRework=4、MaxFails=3。
- **显式 flag 覆盖 tier**：显式 `--max-fix-rounds=5` + L0 任务 → 5（不是 1）。
- **显式 0 要被尊重**：显式 `--max-fix-rounds=0` → 0（禁用自修），
  不能被 tier 派生值顶掉。
- L0 任务的一轮驱动**不启动** critic 棒与 QA 棒；**仍然启动** reviewer 棒。
- 不带 `tier:` 的既有 spec 走 L1 路径，现有 orchestrate 测试全绿。
- 门绿：`go test ./internal/orchestrate/`
