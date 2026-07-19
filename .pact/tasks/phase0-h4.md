---
id: phase0-h4
feature: phase0-sec
owner: kimi-worker
reviewer: claude
---

# phase0-h4 — 修复 H4：remote stint 的 Branch 未校验 → git push --mirror 参数注入（high）

## 目标 / Goal
`pact.stint` 的 dispatcher 只校验 Seat/AgentKind/Task，**不校验 Branch**。option 形态的 Branch（`--mirror`/`--all`/`--upload-pack=`）经 `StintRequest` 流到 serve 的 `runLifecycle` → `gitx.Push(dir,"origin",branch)` → `git push origin --mirror`（毁灭性：删远端不存在于本地的 ref；`--upload-pack=` 更可远端执行）。在 dispatcher 边界拒掉非法 branch。

## 改文件 / Files（只碰这些）
- `internal/remoteexec/dispatch.go`（`pact.stint` 分支，`RunStint` 调用前）
- `internal/gitx/gitx.go`（`Push`/`Fetch`/`AddWorktree` 位置参数前加 `--`，纵深防御）

## 契约 / Contract
1. **主修（回归测试所门控）**：`dispatch.go` 的 `pact.stint` 分支，在调用 `d.Stint.RunStint(...)` 前加：
   ```go
   if rpc.Branch != "" && !gitx.ValidBranchName(rpc.Branch) {
       return fail("pact.stint: invalid branch")
   }
   ```
   （import `internal/gitx`。空 Branch 仍放行——本地解析场景，见既有 TestHandle_Stint。）
2. **纵深防御**：`gitx.Push`/`Fetch`/`AddWorktree` 在位置参数（remote 之后的 ref）前插 `--`，使万一漏网的 ref 不被当选项解析（如 `run(dir,"push",remote,"--",branch)`）。
3. 不改其他行为。

## 验收 / Acceptance（dimension: security）
- `go test ./internal/remoteexec/ -run TestSEC_H4` **全绿**（现红：四个 option 形态 branch 都被 stinter 收到）。
- 既有 `TestHandle_Stint` 不破（空 Branch 仍放行、合法 branch 仍到达 stinter）。
- `go build ./...` 通过；`go vet ./internal/remoteexec/ ./internal/gitx/` 干净。

verify: go test ./internal/remoteexec/ -run 'TestSEC_H4|TestHandle_Stint' && go build ./...
