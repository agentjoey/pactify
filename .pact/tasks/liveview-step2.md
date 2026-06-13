# liveview-step2 — serve /api/orchestrate/status endpoint

verify: go test ./internal/serve/

## 目标 / Goal
在 serve 后端暴露 orchestrate 运行态：新增一个只读端点，读取 step1 写出的 `.pact/orchestrate/status.json` 并以 JSON 返回，供前端 live 面板 (step3) 轮询。

## 改文件 / Files
仅可触碰以下文件（bounded set）：
- `internal/serve/orchestrate.go`（新建）—— DTO + 路由注册 + handler。
- `internal/serve/orchestrate_test.go`（新建）—— handler 测试。
- `internal/serve/api.go`（改，仅一行）—— 在 `Handler()` 里调用 `s.registerOrchestrateRoutes(mux)`，与现有 `registerTimelineRoutes` 等并列。

禁止改动 orchestrate / web / cmd 任何文件。

## 契约 / Contract
**路由（项目维度，沿用现有 `/api/projects/{id}/...` 形态）：**
```
GET /api/projects/{id}/orchestrate/status
```
> 说明：原始目标写作 `/api/orchestrate/status`，但 serve 是多项目服务、所有现有端点都以 `/api/projects/{id}/` 作用域解析 repo（见 `s.project(id)`）。因此采用项目作用域形式，是该端点的规范实现。请在 PR 描述里点明这一决策。

**handler 行为：**
- 用 `s.project(r.PathValue("id"))` 解析 dir；未知项目 → `writeErr(w, 404, "unknown project")`。
- 读取 `<dir>/.pact/orchestrate/status.json`：
  - 文件不存在（orchestrate 从未跑过）→ `200` 返回 `{"present": false}`。
  - 文件存在但解析失败 → `writeErr(w, 500, ...)`。
  - 文件存在且可解析 → `200` 返回 `{"present": true, "status": <原始 status 对象>}`。

**DTO：**
```go
// OrchestrateStatusDTO wraps the orchestrate runtime snapshot. Present is false
// when the orchestrate driver has never run for this project (no status file).
type OrchestrateStatusDTO struct {
	Present bool            `json:"present"`
	Status  json.RawMessage `json:"status,omitempty"`
}
```
- 用 `json.RawMessage` 透传 step1 的 status 字节，避免 serve 端重复声明 Status 结构、与 orchestrate 包字段漂移。
- 复用现有 helper：`writeJSON` / `writeErr`，以及 `logPath` 同款的 `filepath.Join(dir, ".pact", "orchestrate", "status.json")`（可在本文件内加一个小 helper `orchestrateStatusPath(dir)`）。

## 验收 / Acceptance
Reviewer 确认：
1. `go test ./internal/serve/` 全绿。
2. 路由已在 `Handler()` 注册，未知项目返回 404。
3. 三条路径覆盖到测试：无文件→`{present:false}`；有合法文件→`{present:true, status:{...}}` 且 status 内容原样透传；坏文件→500。
4. 未新增对 orchestrate 包的导入（仅读文件字节）；现有 serve 测试不回归。
