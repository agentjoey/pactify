# t-sp3-backend：setup apply endpoint（SP3 后端）

> 完整 TDD 要点见 `docs/superpowers/plans/2026-06-17-sp3-onboarding.md` 的
> **Task t-sp3-backend**；设计见 `docs/superpowers/specs/2026-06-17-sp3-onboarding-design.md`。先读它们。

## 目标
新建 `POST /api/setup/apply`：一键 init 新项目 + batch wire 各座席的 agent。

- `s.requireSeat(w)`（machine-scoped，新项目尚无 roster）→ 无 seat 422。
- body `{path:string, project:string, seats:[{id,roles,entry,kind}]}`。
- 校验 `path` 存在且**无 `.pact`** → 已存在 409（不 re-init）。
- 组 seat spec 串 `id:roles:entry:kind`（roles 逗号连接），`pact.At(path).As(seat).Init(project, specs)`。
- 逐个带 kind 的 seat：`agent.WireAt(path, kind, id, roles, path)`，收集 `{kind,seat,wrote,path,docOnly,snippet}`。
- 返回 `{inited:true, wired:[...], notes}`。Init 失败→不 wire 返回错误；wire 失败→per-kind 报告（best-effort）。

## 文件
- 改 `internal/serve/setup.go`（加路由 + `handleSetupApply`；现有 GET suggest 在此）
- 建 `internal/serve/setup_apply_test.go`
- 复用：`pact.Init`(engine.go:61)、`seat.ParseSeat`、`agent.WireAt`(briefing.go:90)、`author.go` requireSeat

## 纪律
- **TDD**：先写失败测试→红→实现→绿。测试：无 seat→422、path 有 .pact→409、合法→init+wired、TOML→docOnly。
- 复用现有函数，勿重造。完成跑 verify 全绿再 checkpoint。

verify: go test ./internal/serve/
