# agy-kind — 把 `antigravity` 从 IDE 条目改造为 headless CLI kind

tier: L2
verify: go test ./internal/agent/ ./internal/agentcfg/ ./internal/doctor/ ./internal/wizard/
dimension: correctness

## 目标 / Goal

`antigravity` 当前注册的是 **IDE（GUI kind）**——`desktop:true`、无 headless runner、command 为空。
**Human Owner 决定：去除 IDE 接入，`antigravity` 即 CLI**（不新增 `-cli` 后缀条目、不并存）。

依赖：`agy-repodir` 已 accepted（提供 `{repoDir}` 占位符）。
计划：`.agent/plans/antigravity-cli-integration-2026-08-22.md` §4 A1。

## 已核实事实（实机实测，勿重新调查）

1. CLI 二进制是 **`agy`**（不是 `antigravity`），`~/.local/bin/agy`，v1.1.17，已认证可用。
2. ⚠️ **agy 读 `GEMINI.md`，不读 `AGENTS.md`** —— 实测：同一仓库同时放两个文件问同一问题，
   它回答 GEMINI.md 的内容（gemini 血统）。**按直觉填 AGENTS.md 会导致 pactify 写的座席指令
   agy 根本读不到**，表现为"worker 不知道自己是谁"。
3. ⚠️ **不传 `--add-dir` 时 agy 把文件写进 `~/.gemini/antigravity-cli/scratch/`**，
   且照常返回 `SUCCESS` —— **静默失败**。
4. ⚠️ **`--print-timeout` 默认仅 5 分钟**，pactify 的 stint 常超时；不抬高会表现为
   "无 checkpoint 的干净退出"（v0.8.1 escalation 归因要解决的那类误诊）。
5. auth 是 **OAuth 令牌文件** `~/.gemini/antigravity-cli/antigravity-oauth-token`（实测存在），
   **不是** env key。
6. **`agy mcp add` 实测写入 `~/.gemini/config/mcp_config.json`** —— registry 现有路径正确。
7. `agy models` 实测可用，返回 `gemini-3.7-flash-{high,medium,low}` /
   `gemini-3.6-flash-*` / `gemini-3.5-flash-*` / `gemini-3.1-pro-{high,low}`。

## 改文件 / Files

- `internal/agent/agent.go` — 改造既有 registry 条目
- `internal/agent/launch.go` — 新增 `RunnerProfile`
- `internal/doctor/vendor.go` — `vendorAuth` 条目
- `internal/orchestrate/env.go` — `crossVendorStrip` 的自有键集合
- 上述各自测试 + §「既有测试更新」列出的 5 处

## 契约 / Contract

### 1. registry 条目改造（`agent.go`）

```go
// 旧: {"antigravity", "", "~/.gemini/config/mcp_config.json", Global, JSONMcpServers, true, false, ""}
"antigravity": {"antigravity", "GEMINI.md", "~/.gemini/config/mcp_config.json",
                Global, JSONMcpServers, false /*desktop*/, false, "agy"},
```
MCP 路径保持不变——**已实测**：`agy mcp add` 正是写入 `~/.gemini/config/mcp_config.json`。
IDE 已去除，故该路径纯属 agy 自己的，不存在共用问题。

### 2. RunnerProfile（`launch.go`）

```go
"antigravity": {
    Command:      "agy",
    DefaultModel: "gemini-3.7-flash-medium",   // Human Owner 指定轻量档
    Models: []string{
        "gemini-3.7-flash-high", "gemini-3.7-flash-medium", "gemini-3.7-flash-low",
        "gemini-3.1-pro-high", "gemini-3.1-pro-low"},
    BuildArgs: func(model string, perm PermPosture, briefing string) []string {
        return []string{"-p", briefing, "--model", model,
            "--add-dir", "{repoDir}",            // ⚠️ 事实3,不传就写进 scratch
            "--output-format", "json",           // token 捕获依赖(见 agy-tokens)
            "--dangerously-skip-permissions",    // blanket posture
            "--print-timeout", "30m"}            // ⚠️ 事实4
    },
    EffortArgs: func(e string) []string { return []string{"--effort", e} },
}
```

### 3. doctor（`vendor.go`）
binary `agy`；auth 探针 `~/.gemini/antigravity-cli/antigravity-oauth-token`；
installHint 指向 antigravity 安装；loginHint 说明需 OAuth 登录。

### 4. crossVendorStrip（`env.go`）
⚠️ agy 用 OAuth 令牌**文件**认证。**不要**把 `GOOGLE_API_KEY`/`GEMINI_API_KEY` 当作它的自有键
（那是 `gemini-cli` 的）。**先实测 agy 是否读任何 env key**；拿不准就归入"无自有 env key"
（最安全：其余厂商键全部 strip）。

## ⚠️ 必须实测确认的一点

`agy models` 的模型名**自带** `-high/-medium/-low`，同时又有独立 `--effort` flag。
**二者同时给出时谁生效、是否冲突，未验。** 若模型名会静默覆盖 `--effort`，
则 tier→effort 路由对 agy 无效，需改为在模型名里编码档位。**实施时必须实测并把结论写进 evidence。**

## 既有测试更新（改造 GUI→drivable 会触发，逐条更新，不要绕过）

- `internal/wizard/wizard_test.go:46` 断言 "antigravity should be marked non-drivable" → 反转
- `internal/agent/agent_test.go:148` 把它列入 GUI kinds → 移出
- `internal/doctor/vendor_test.go:243` 把它排除在厂商预检外 → 改为**应当**纳入
- `internal/agent/briefing_test.go:42` onboarding 块渲染 → 按新 entryFile 更新
- `internal/agent/agent_test.go:10` Kinds() 排序清单 → 名字不变，通常不受影响（确认即可）

## 验收 / Acceptance

评审维度：**correctness**。

- **agy 可被指派为 reviewer**（Human Owner 决策）：kind 不得对角色做限制——
  worker/reviewer 两种 briefing 都应能正常启动它（本棒只需不阻断；行为由 `agy-e2e` 验）。
- `agent.Get("antigravity").Runner()` 返回 ok=true，command `agy`，argv 含
  `--add-dir {repoDir}` 与 `--print-timeout`。
- `pactify doctor` 报出 agy 的 binary 与 auth 两项检查（不再被当作 GUI 跳过）。
- `EffortArgs("low")` → `["--effort","low"]`。
- 上述 5 处既有测试全部更新且绿。
- **实测结论已写进 evidence**：`--effort` 与模型名内嵌档位的关系。
- 门绿：`go test ./internal/agent/ ./internal/agentcfg/ ./internal/doctor/ ./internal/wizard/`
