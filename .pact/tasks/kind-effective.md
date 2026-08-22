# kind-effective — 按 kind 分派的行为改用「实际执行的 kind」

tier: L2
verify: go test ./internal/agentcfg/ ./internal/orchestrate/ ./internal/serve/
dimension: correctness

## 问题（**已实测发作，不是假设**）

`orchestrate/runner.go` 里若干行为按 kind 分派，用的是 `lc.Kind`（调用方声明的 kind）：

- `crossVendorStrip(lc.Kind)` —— 决定清掉哪些厂商 API key（M11 凭据隔离）
- `parseTokenUsage(lc.Kind, …)` —— 决定用哪个 token 解析器
- resume 的 `switch lc.Kind`（`case "codex-cli"` / `case "antigravity"`）

但**实际执行的命令**来自 `agentcfg.ResolveSeat(lc.Seat, lc.Kind, lc.Effort)`：
`ResolveSeat`（`agentcfg.go:102-124`）在座席绑定了 role profile 时，会用
**profile 的 `Kind` 覆盖传入的 kind**（`k := p.Kind; if k == "" { k = kind }`）。
两者不一致时，`lc.Kind` 与真正跑起来的程序就指向不同厂商。

### 实测记录（2026-08-22，本仓真实 orchestrate 跑）

座席 `claude` 的名册 Kind 曾被误写成 `antigravity`，而 `~/.pactify/roles.json` 把
`claude` 绑到 profile `claude-opus5-reviewer`（`kind: claude-code`）。于是：

- `lc.Kind = "antigravity"`，`eff.Command = "claude"`（profile 覆盖生效）
- → `crossVendorStrip("antigravity")` 把**所有**厂商 key 清空，**包括 `ANTHROPIC_API_KEY`**，
  claude reviewer 是在没有自己厂商 key 的环境下跑的（本次没炸，因为 claude-code 走自身凭据存储；
  方向恰好是「清多了」。反向组合就是「漏了」——M11 要防的那种）
- → `parseTokenUsage("antigravity", <claude 的输出>)` 用 agy 的解析器解 claude 的输出，返回 false
  → **该棒 token 全部丢失**。`.pact/orchestrate/tokens.json` 里该任务只记到 worker 的两棒
  （`runs: 2`），reviewer 两棒零记录。

## 目标 / Goal

让按 kind 分派的行为，一律使用**解析后实际执行的 kind**。

## 契约 / Contract

### 1. `agentcfg.Effective` 增加 `Kind` 字段

`Effective`（`agentcfg.go:37-43`）目前有 `Command/Args/Model/Scoped/Effort`，**没有 Kind**——
这正是 runner 无从得知有效 kind 的原因。加一个 `Kind string`，在 `ResolveWith` 里填入它收到的
`kind` 形参（`ResolveWith` 是两条路径的唯一出口，`ResolveSeat` 的两个分支都经它返回，所以
role-profile 覆盖后的 kind 会自动带出来，不要在 `ResolveSeat` 里另写一份）。

### 2. `runner.go` 改用 `eff.Kind`

把下列三处的 `lc.Kind` 换成 `eff.Kind`：
- `crossVendorStrip(...)`
- `parseTokenUsage(...)`（即 `recordTokens` 的取值来源；注意 `recordTokens(lc, …)` 当前从
  `lc.Kind` 取，需要把有效 kind 传进去，**不要**改 `LaunchContext` 结构）
- resume 的 `switch`，以及 antigravity 的会话生命周期块里那个 `lc.Kind == "antigravity"` 判断

**注意顺序**：`agyResumeArgsIfAny` 目前在 `eff` 解析**之后**调用，可直接用 `eff.Kind`；
若有任何一处在解析之前就分派，必须移到之后。

### 3. 不要改的

- **不要**改 `ResolveSeat` 的覆盖优先级（profile Kind 赢过传入 kind 是既定语义，本任务不动它）。
- **不要**改 `LaunchContext` 结构或它的字段含义。
- **不要**改 `loop.go` 里 `--seat-kind` > 名册 的优先级。
- **不要**动 `glmEnv(eff.Command, …)` / `geminiEnv(eff.Command)`——它们本来就按 `eff` 取值，是对的。
- 函数开头 `agent.Get(lc.Kind)` 的未知 kind 守卫**保持不变**（它是解析前的入参校验）。

## 验收 / Acceptance

评审维度：**correctness**。

- `Effective.Kind` 在两条路径下都正确：绑定了 profile 的座席 → profile 的 kind；
  未绑定的座席 → 传入的 kind。
- **回归测试（钉死本次实测场景）**：座席绑定 `kind=claude-code` 的 profile，但传入
  `lc.Kind="antigravity"` 时——
  - `crossVendorStrip` 按 **claude-code** 计算（即 `ANTHROPIC_API_KEY` **保留**，其它厂商 key 清空）
  - token 解析走 **claude-code** 的解析器（给一份 claude 形状的输出，断言 token **被记录**；
    这条必须在改动前会红）
  - agy 的 resume 分支**不触发**（argv 里不出现 `--conversation`）
- 反向：座席绑定 `kind=antigravity` 而传入 `lc.Kind="claude-code"` 时，行为按 antigravity 走。
- 未绑定 profile 的座席，行为**逐字节不变**（现有全部 runner 测试不得改断言）。
- 门绿：`go test ./internal/agentcfg/ ./internal/orchestrate/ ./internal/serve/`
- 全仓不回归：`go build ./... && go vet ./internal/...`
