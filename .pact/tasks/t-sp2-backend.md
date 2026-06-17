# t-sp2-backend：orchestrate run/resume/ship/diff endpoints（SP2 后端）

> 完整 TDD 要点见 `docs/superpowers/plans/2026-06-17-sp2-dashboard-drive.md` 的
> **Task t-sp2-backend**；设计见 `docs/superpowers/specs/2026-06-17-sp2-dashboard-drive-design.md`。先读它们。

## 目标
新建 4 个 HTTP endpoint，让 dashboard 能驱动 orchestrate：

- `POST /api/projects/{id}/orchestrate/run`：body `{feature?, max_concurrency?}`；`actingProject`(422)；
  防重入（运行中→409）；从 roster seats 推断 `--seat-kind`；经**注入的 runner seam**（便于测试）
  后台 `exec.Command(pactify, "orchestrate", ...)` `Start()`（不 Wait）→ **202** + status URL。
- `POST .../orchestrate/resume`：同 run + `--resume`。
- `POST .../orchestrate/ship`：body `{remote?,branch?,pr?,title?,body?}`；调 `internal/finish`（参考 cmd_finish.go）；**同步** `{pushed, pr_url?}`。
- `GET .../orchestrate/diff`：`git -C dir diff`（`?staged`→`--staged`）→ `{diff}`。

## 文件
- 改 `internal/serve/orchestrate.go`（加 4 路由 + handler；现有 status/parallel GET 在此）
- 建 `internal/serve/orchestrate_drive_test.go`
- 复用：`author.go` actingProject、`internal/finish`、`cmd_orchestrate.go` 命令形态

## 纪律
- **TDD**：先写失败测试→红→实现→绿。exec 用注入 seam（生产 os/exec，测试 fake），断言 argv + 不阻塞。
- 复用现有函数，勿重造。完成跑 verify 全绿再 checkpoint。

verify: go test ./internal/serve/
