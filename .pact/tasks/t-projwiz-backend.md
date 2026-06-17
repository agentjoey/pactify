# t-projwiz-backend：fs browse + group（项目向导后端）

> 完整 TDD 要点见 `docs/superpowers/plans/2026-06-17-project-wizard.md` 的 **Task t-projwiz-backend**；
> 设计见 `docs/superpowers/specs/2026-06-17-project-wizard-design.md`。先读它们。

## 目标
后端支撑「添加项目向导」的「选文件夹不手输」+ 分组：

1. **`GET /api/fs/browse?path=<abs>`**（新建 `internal/serve/fsbrowse.go`）：`requireSeat`(422)；空 path→`os.UserHomeDir`；
   `os.ReadDir` 只列**子目录**（跳过文件 + `.git`/`node_modules`/`.`-前缀隐藏目录）；每项 `{name, path, isGit, hasPact}`；
   返回 `{path, parent, entries}`；非法/不可读 path→400。仅读不写。
2. **`registry.Project` 加 `Group string`**（`json:"group,omitempty"`），`Register` 透传、持久化、GET registry DTO 带 group。
3. **`registerReq` + `setupApplyReq` 各加 `Group`**；setup-apply 在 init+wire **成功后自注册进 registry**（`registry.Register(project, path, group)`），让向导一次调用即完成新建+注册。

## 文件
- 建 `internal/serve/fsbrowse.go` + `fsbrowse_test.go`
- 改 `internal/registry/registry.go`、`internal/serve/registry.go`、`internal/serve/setup.go`
- 复用：`author.go` requireSeat、现有 registry/setup 逻辑

## 纪律
- **TDD**：先写失败测试→红→实现→绿。fs browse 用 `t.TempDir()` 造目录树（含 .git 子目录 + .pact 子目录 + 普通文件）测 isGit/hasPact/跳过。
- 复用现有函数，勿重造。完成跑 verify 全绿再 checkpoint。

verify: go test ./internal/serve/ ./internal/registry/
