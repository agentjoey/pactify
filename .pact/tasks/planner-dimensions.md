# Task planner-dimensions (C-8) — planner 给每个任务分配评审视角(review dimension)

## 目标
让 planner 在拆任务时为**每个任务**指定一个评审视角(lens),引导 reviewer 聚焦。
纯加性:字段可选,不破坏现有 plan/prompt 契约。

## 改文件(限这两 + 各自测试)
- `internal/planner/prompt.go`
- `internal/planner/manifest.go`
- `internal/planner/prompt_test.go`
- `internal/planner/manifest_test.go`

## 契约
1. `manifest.go` 的 `PlanTask` 加字段:
   `Dimension string \`json:"dimension,omitempty"\``
   — 评审视角。可选(可为空)。若非空,必须是以下集合之一(小写):
   `correctness` `security` `performance` `maintainability` `ux`。
   在 `PlanTask` 校验里加:dimension 非空且不在集合 → 追加一条 error
   `task <id>: dimension %q not one of correctness|security|performance|maintainability|ux`。
   为空则跳过(向后兼容:旧 plan 无此字段仍合法)。把合法集合定义为包级
   `var validDimensions = map[string]bool{...}` 或等价,便于测试引用。
2. `prompt.go` 的 `BuildPrompt`:加一节 `## Review dimensions`,指示 planner
   为每个任务选一个评审视角(上述集合),写进 manifest 的 `dimension` 字段,
   并在该任务 spec 的「验收 / Acceptance」里点明这个视角。措辞简洁。
   同时在 `manifestSchemaExample` 的示例任务里给一个 `"dimension": "correctness"`
   字段做示范(至少一个任务带上)。

## 验收 / Acceptance(视角: maintainability — 契约清晰、加性不破旧)
- reviewer 独立跑 verify 门通过。
- 阅读 diff 确认:Dimension 可选、空值不报错、非法值报错、prompt 含 dimensions 指引。

## verify
verify: go build ./... && go test ./internal/planner/
