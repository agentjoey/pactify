# agy-repodir — 新增 `{repoDir}` 启动占位符

tier: L1
verify: go test ./internal/orchestrate/ ./internal/agent/ ./internal/agentcfg/
dimension: correctness

## 目标 / Goal

让 `RunnerProfile.BuildArgs` 生成的 argv 能携带 `{repoDir}` 占位符，由 runner 层在启动时
替换成本次 stint 的 `lc.RepoDir`。这是接入 antigravity CLI（`agy`）的**结构性前提**——
agy 必须通过 `--add-dir <RepoDir>` 才会在目标仓库干活。

计划：`.agent/plans/antigravity-cli-integration-2026-08-22.md` §4 A0。

## 已核实事实（勿重新调查）

- `runner.go` 已有占位符替换 switch（约 163-169 行）：
  `case briefingPlaceholder: args[i] = lc.Briefing` / `case "{seat}": args[i] = lc.Seat`
- `BuildArgs(model, perm, briefing)` **没有** repoDir 参数；给它加参数要动**所有** kind 的签名，
  而各 kind 的默认输出被 `launch_test`/`agent_test` **逐字节锁定** → 风险高。
  **Human Owner 已决定采用占位符方案。不要改 BuildArgs 签名。**
- `LaunchContext` 已有 `RepoDir` 字段。

## 改文件 / Files

- `internal/orchestrate/runner.go`（仅在既有 switch 加一个 case）
- 对应测试

**不要**改 `BuildArgs` 签名、不要动任何 kind 的 profile（那是 agy-kind 那棒）。

## 契约 / Contract

在 `runner.go` 既有占位符 switch 中新增：

```go
case "{repoDir}":
    // A kind whose CLI needs the workspace passed explicitly (antigravity/agy
    // uses --add-dir; without it the agent writes into its own scratch dir)
    // carries this token in its argv; substitute the stint's repo at exec.
    args[i] = lc.RepoDir
```

## 验收 / Acceptance

评审维度：**correctness**。

- argv 含 `{repoDir}` 时被替换为 `lc.RepoDir` 的真实值。
- **回归（硬要求）**：不含该占位符的 kind，启动参数**逐字节不变**——
  既有 `launch_test`/`agent_test` 保持绿。
- `lc.RepoDir` 为空时替换为空字符串，不 panic。
- 门绿：`go test ./internal/orchestrate/ ./internal/agent/ ./internal/agentcfg/`
