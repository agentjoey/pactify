# exec-tiering-hermetic — 启动路径测试必须与本机 agent 配置隔离

tier: L0
verify: go test ./internal/orchestrate/
dimension: correctness

## 目标 / Goal

`internal/orchestrate` 里凡是走**座席启动解析**的测试，都会读**本机**的
agent 配置（`~/.pactify/agents.json`）。于是它们在干净的 CI 上绿、在任何配了
agent 覆盖的开发机上红——测试结果取决于跑它的人，而不是代码。

实测：`TestCmdRunner_Codex_ResumeRetry` 用 `Kind: "codex-cli"` 启动，却拿到
`[run --title [pact:w1] -m deepseek/deepseek-v4-pro do it]`——那是 **opencode**
的参数形状（`--title` 只有 `tagOpencodeSession` 会加）配 deepseek 模型，
因为本机 `agents.json` 里 `opencode → deepseek/deepseek-v4-pro`。

**这个红是有毒的**：它让整个包的门不可信，逼得每个 worker 都要靠 briefing
里的免责说明才不被带偏——换个人或换台机器就会有人花 token 去查一个假故障。

## 背景（已核实，不要重新调查）

- 仓库**已有 hermetic 先例**，照抄即可：
  `internal/orchestrate/approve_test.go:12`、`fallback_test.go:10`
  都用 `t.Setenv("PACTIFY_HOME", t.TempDir())` 把机器配置隔离掉。
- 污染源是 `agentcfg` 在解析座席启动配置时读取机器级 agent 配置。
- 这是**一类问题**，不是一个测试：请找出 `internal/orchestrate` 里所有
  会走启动解析（`CmdRunner.Run` / `ResolveSeat` / `agentcfg` 解析）
  且**未**隔离 `PACTIFY_HOME` 的测试，一并修掉。

## 改文件 / Files

- `internal/orchestrate/` 下的**测试文件**（`*_test.go`）

**只改测试，不改任何生产代码。** 如果你认为必须改生产代码才能隔离，
那就停下，在 evidence 里说明原因，不要擅自改。

## 契约 / Contract

给每个受影响的测试加上（沿用既有先例的写法）：

```go
t.Setenv("PACTIFY_HOME", t.TempDir())
```

要求：

1. 覆盖**所有**受影响测试，不是只修 `TestCmdRunner_Codex_ResumeRetry`。
2. 不要改动这些测试的断言语义——它们断言的行为是对的，
   坏的只是环境隔离。
3. 优先复用已有 helper；若同一段 setup 在多个测试里重复出现，
   可以抽一个小 helper（例如 `hermeticHome(t)`），但**不要**顺手重构
   无关测试结构。

## 验收 / Acceptance

评审维度：**correctness**。

- **关键验收（必须亲测）**：在**本机真实**（被污染的）配置下，
  `go test ./internal/orchestrate/` 全绿——不需要任何 `PACTIFY_HOME=` 前缀。
- 在隔离环境下同样绿：`PACTIFY_HOME=$(mktemp -d) go test ./internal/orchestrate/`
  （两种环境结果一致，正是"与本机配置无关"的证明）
- 未改动任何生产代码：`git diff --stat` 只含 `*_test.go`。
- 未改动任何测试的断言语义。
