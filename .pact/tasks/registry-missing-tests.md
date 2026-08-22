# registry-missing-tests — 给「失效项目可区分」补回归覆盖

tier: L2
verify: go test ./internal/registry/ ./internal/serve/ ./cmd/pactify/ && bats tests/
dimension: correctness

## 背景 / Context

commit `eaef206` 加了「失效项目可区分」，**但没有任何测试**。本任务只补测试，不改生产代码。

已实现的三处（读代码确认，不要改）：

1. `registry.Missing(path) bool`（`internal/registry/registry.go`）——
   `filepath.Join(path, ".pact")` 不存在或不是目录即为 true；空 path 也为 true。
2. `pactify list`（`cmd/pactify/cmd_register.go`）—— 失效项目行尾追加
   `\t(missing — no .pact/ at this path; \`pactify unregister <name>\` to remove)`。
3. `GET /api/projects`（`internal/serve/api.go`）—— item 新增
   `Missing bool \`json:"missing,omitempty"\``。

**这个缺陷之所以要钉死**：失效登记与「健康但为空」的项目在此前的 API 里**逐字节相同**
（都是 `feature_count: 0`），dashboard 因此渲染成一个正常的空看板。回归会静默复发。

## 要补的测试

### 1. `internal/registry/registry_test.go`

- `Missing("")` → true
- 目录存在且含 `.pact/` 目录 → false
- 目录存在但**无** `.pact/` → true
- 目录整体不存在 → true
- `.pact` 是**文件**而非目录 → true（用 `os.WriteFile` 造一个同名文件）

### 2. `internal/serve/`（就近的既有 API 测试文件）

- 两个项目：一个健康（有 `.pact/`）、一个路径已删除 →
  `GET /api/projects` 返回的 JSON 中，健康项目**不含** `missing` 键（`omitempty` 生效），
  失效项目 `missing: true`。
- **这条是本任务的核心断言**：必须直接断言原始 JSON 里健康项目没有该键
  （例如 unmarshal 成 `map[string]any` 后检查 key 是否存在），而不是断言解码后的
  struct 字段为 false —— 后者无法区分「键不存在」和「键为 false」，会让
  `omitempty` 回归时静默通过。

### 3. `cmd/pactify/`（或 `tests/*.bats`，二选一，按既有惯例就近）

- `pactify list` 对失效项目输出含 `missing`，对健康项目**不含**。
- 提示里的项目名必须是该项目自己的名字（防止拼接错行）。

## 不要做 / Out of scope

- **不要**改 `internal/registry/registry.go`、`cmd_register.go`、`internal/serve/api.go`
  —— 生产代码已定稿，本任务只补测试。
- **不要**改既有测试的断言。
- **不要**去动自动注册（`EnsureRegistered`）的行为——它刻意保持原样。
- **不要**新增 `unregister` 命令——它已经存在（`newUnregisterCmd`）。

## 验收 / Acceptance

评审维度：**correctness**。

- 上述三组测试齐备，且**每一条都必须在对应行为被破坏时变红**。自检方法：
  临时把 `Missing` 改成恒 `return false`，你的新测试应当变红；改回后恢复绿。
  **把这次自检的结果写进 evidence**（哪些测试红了）。
- `omitempty` 那条按上面要求断言原始 JSON 的键存在性。
- 门绿：`go test ./internal/registry/ ./internal/serve/ ./cmd/pactify/ && bats tests/`
- 全仓不回归：`go build ./... && go vet ./internal/...`
