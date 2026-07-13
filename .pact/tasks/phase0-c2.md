---
id: phase0-c2
feature: phase0-sec
owner: kimi-worker
reviewer: claude
---

# phase0-c2 — 修复 C2：cmd 传输 + verify 门泄露密钥给第三方 agent（critical）

## 目标 / Goal
默认 cmd 传输（`osExec`/`osExecIdle`）与 fix-until-green 的 verify 门（`shellExec`）当前把**完整父进程环境**交给 spawned agent，泄露 `PACT_RELAY_TOKEN` / `PACTIFY_MASTER_SECRET`。改为**过滤后的环境**。ACP/cockpit 路径已有 `filteredEnviron`，本任务把同款纪律补到 cmd 路径与 verify 门。

## 改文件 / Files（只碰这些）
- `internal/orchestrate/runner.go`（`osExec`，约 :108 `cmd.Env = append(os.Environ(), env...)`）
- `internal/orchestrate/runner_idle.go`（`osExecIdle`，约 :107 同上）
- `internal/orchestrate/defaults.go`（`shellExec.Run` 的 `cmd.Env` base）
- 新增包内辅助：`internal/orchestrate/env.go`（`filteredEnviron`）

## 契约 / Contract
1. 新增 `func filteredEnviron() []string`，**复刻 `internal/acp/acp.go` 的实现**：从 `os.Environ()` 剔除 `key == "PACT_RELAY_TOKEN"` 或 `strings.HasPrefix(key, "PACTIFY_")` 的项，其余原样保留。
2. `osExec` / `osExecIdle`：`cmd.Env = append(filteredEnviron(), env...)`（把 `os.Environ()` 换成 `filteredEnviron()`）。
3. `shellExec.Run`：base 从 `os.Environ()` 改为 `filteredEnviron()`——**无论 `env` map 是否为空都要过滤**（即 `cmd.Env = filteredEnviron()`，再 append map 里的 `k=v`）。
4. **不改其他行为**：`PACT_AGENT_ID`/`PACT_TASK_ID`/`PACT_DIR` 等注入照旧；`PATH`/`HOME`/`ANTHROPIC_*` 等非密钥变量必须仍透传（本任务只做 denylist；allowlist 是后续 M11，不在此）。

## 验收 / Acceptance（dimension: security）
- `go test ./internal/orchestrate/ -run TestSEC_C2` **全绿**（现红）。
- `go build ./...` 通过；`go vet ./internal/orchestrate/` 干净。
- 现有 `runner_test.go`（TestCmdRunner*/TestRunner*）不被破坏。

verify: go test ./internal/orchestrate/ -run 'TestSEC_C2|TestCmdRunner|TestRunner' && go build ./...
