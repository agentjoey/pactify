# exec-tiering-parse — tier 成为可解析的任务属性（纯管线，零行为变化）

tier: L1
verify: go test ./internal/orchestrate/ ./internal/planner/
dimension: correctness

## 目标 / Goal

让任务规格能声明复杂度分级 `tier: L0|L1|L2|L3`，并让 plan 清单承载同一字段。

**本任务不改变任何运行时行为** —— 只铺解析与校验管线。真正的模型/预算路由
在后续任务（`exec-tiering-route` / `exec-tiering-budget`）里接线。

设计依据：`docs/specs/execution-tiering.md` §4.1–§4.4。

## 背景（已核实，不要重新调查）

- `tier` **走 spec 文件通道，不走账本**。理由：pact 协议 v1 已冻结，
  `Assign(taskID, feature, branch, owner, reviewer, spec, deps)` 装不下新字段，
  加事件字段就是改协议。而 `verify:` / `qa:` 已经证明了 spec 文件这条路
  （`taskGateCommand` → `extractVerify(readSpec(...))`）。
- `internal/orchestrate/gate.go` 已有共享解析器 `extractField(md, prefix)`：
  首行命中、去空白、去一对引号、空值视为缺失。**直接复用它，不要另写解析。**
- `orchestrate` 与 `planner` **互不 import**（都由 cmd/ 与 serve/ 消费）。
  不要为了共享常量在两者之间建立 import —— 见下方「契约」的刻意取舍。

## 改文件 / Files

- `internal/orchestrate/gate.go` — 新增 tier 常量与解析
- `internal/planner/manifest.go` — `PlanTask.Tier` 字段 + 校验
- `internal/planner/prompt.go` — 要求 planner 输出 tier
- 对应测试文件（新建或就近扩展）

**不要**改 `Assign` 签名、账本事件、`escalate`、运行时调度逻辑。
**不要**在本任务里接线任何 model/effort/预算行为。

## 契约 / Contract

### 1. orchestrate 侧（行为方，拥有类型）

```go
const tierPrefix = "tier:"

type Tier string

const (
    TierL0 Tier = "L0"
    TierL1 Tier = "L1" // 默认
    TierL2 Tier = "L2"
    TierL3 Tier = "L3"
)

// ParseTier normalizes a raw tier string. Case-insensitive, whitespace-trimmed.
// Any absent, empty, or unrecognized value resolves to TierL1 so every existing
// spec keeps today's behavior byte-for-byte.
func ParseTier(raw string) Tier

// extractTier pulls `tier:` out of a task spec markdown, defaulting to TierL1.
func extractTier(specMarkdown string) Tier
```

`extractTier` 必须走既有的 `extractField(specMarkdown, tierPrefix)`。

### 2. planner 侧（序列化方，只校验字符串）

```go
// PlanTask
Tier string `json:"tier,omitempty"` // L0|L1|L2|L3; empty = L1
```

清单校验：`Tier` 非空且不是这四个字面量之一时 → 返回校验错误
（在 plan review 阶段失败，而不是留到运行时）。校验大小写不敏感。

**刻意取舍**：四个字面量在 orchestrate 与 planner 各出现一次。这是为了
不让两个本来独立的包互相 import；请在 planner 侧加一行注释说明这层重复
是有意的，并指向 `orchestrate.Tier` 为行为事实源。

### 3. planner 提示词

`BuildPrompt` 增加一节，要求 planner：

- 给**每个** task 标一个 tier（L0 简单明确 / L1 普通开发 / L2 跨模块复杂 /
  L3 高不确定性·高风险）；
- 把 tier 同时写进 **manifest 的 `tier` 字段** 和 **spec 文件的 `tier:` 行**
  （与 `verify` 现有的双写约定一致）；
- 并**优先**把 L0/L1 派给便宜的 role-bound 座席、L2/L3 派给更强的座席。

同时更新 `manifestSchemaExample`，让示例里带上 `"tier"`。

## 验收 / Acceptance

评审维度：**correctness**。

- `ParseTier`：`"L2"` / `"l2"` / `" L2 "` 都得到 `TierL2`；`""`、`"L9"`、
  `"garbage"` 都得到 `TierL1`。
- `extractTier`：spec 里有 `tier: L3` → `TierL3`；**没有 tier 行 → `TierL1`**；
  裸 `tier:`（无值）→ `TierL1`。
- **向后兼容（硬要求）**：不带 `tier:` 的既有 spec，其 `taskGateCommand`
  / QA / 现有流程行为**逐字节不变**。请加一个测试钉住这点。
- 清单：带合法 `tier` 能 round-trip；带 `"L9"` 被校验拒绝；不带 `tier`
  的老清单仍然合法。
- `BuildPrompt` 输出包含 tier 指示，且 schema 示例含 `"tier"` 字段。
- 门绿：`go test ./internal/orchestrate/ ./internal/planner/`
