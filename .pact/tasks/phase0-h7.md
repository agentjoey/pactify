---
id: phase0-h7
feature: phase0-sec
owner: kimi-worker
reviewer: claude
---

# phase0-h7 — 修复 H7：cmd 传输进程组在 ctx-cancel 时不收组 → 孤儿 agent 泄漏（high）

## 目标 / Goal
默认 cmd 传输 `osExecIdle` 把子进程放进独立进程组（Setpgid）却**从不设 `cmd.Cancel`**。ctx cancel（RunTimeout 到期 / Ctrl-C）时，`exec.CommandContext` 默认只对**组 leader** 发 SIGKILL，孙进程（MCP server、npx node 子）孤儿存活、继续烧 token。ACP 传输已修（`acp.go:186 cmd.Cancel = killGroup`），cmd 传输漏了。

## 改文件 / Files（只碰这些）
- `internal/orchestrate/runner_idle.go`（`osExecIdle`）
- `internal/orchestrate/runner.go`（`osExec`，一致性，见 ⚠️）

## 契约 / Contract
1. **【必做·测试门控】** `runner_idle.go` 的 `osExecIdle`：在 `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` **之后**加：
   ```go
   cmd.Cancel = func() error { killGroup(cmd); return nil }
   ```
   （对齐 `internal/acp/acp.go:186`。可选加 `cmd.WaitDelay = 2 * time.Second` 兜底 pipe 未关。）
2. **【一致性·谨慎】** `runner.go` 的 `osExec` 当前**没有 Setpgid**。若要它也在 cancel 收组，**必须同时**加 `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` 和 `cmd.Cancel = func() error { killGroup(cmd); return nil }`。
   > ⚠️ **绝不能只给 osExec 加 cmd.Cancel 而不加 Setpgid**——`killGroup` 对没有独立进程组的子进程会 `Kill(-parentPgid)`，**误杀父进程组（含本测试进程）**。若不确定就只做第 1 步，osExec 留 TODO 注释。
3. 不改其他行为（idle 巡逻、组杀逻辑、stdout tee 都不动）。

## 验收 / Acceptance（dimension: security）
- `go test ./internal/orchestrate/ -run 'TestSEC_H7'` **绿**（现红：孙进程存活）。
- 现有 `TestCmdRunner*`/`TestRunner*`/idle 相关测试**不破**。
- `go build ./...` 通过；`go vet ./internal/orchestrate/` 干净。

verify: go test ./internal/orchestrate/ -run 'TestSEC_H7|TestCmdRunner|TestRunner|Idle' && go build ./...
