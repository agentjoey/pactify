# exec-tiering-route — tier 驱动 reasoning effort（纯增量，未声明的 kind 零变化）

tier: L2
verify: go test ./internal/agent/ ./internal/agentcfg/ ./internal/orchestrate/
dimension: correctness

## 目标 / Goal

让任务的 tier 在**启动座席时**转成该 CLI 的 reasoning-effort 参数：
L0 低、L1/L2 中、L3 高。实现 DEP 的核心成本规则——**强模型是升级路径，
不是默认路径**。

依赖：`exec-tiering-parse`（提供 `Tier` 与 `extractTier`）。
设计依据：`docs/specs/execution-tiering.md` §4.5。

## 背景（已核实，不要重新调查）

- `agent.RunnerProfile.BuildArgs(model, perm, briefing)` 的**默认输出被
  `launch_test` / `agent_test` 逐字节锁定**（它等于历史硬编码的 Runner() args）。
  **绝对不要改 `BuildArgs` 的签名或默认输出。**
- tier 只改「怎么启动这个座席」，**绝不改「派给谁」**。owner/reviewer 留在
  账本里不动，两大不变量不受影响。
- 各厂商 effort 表达方式不同，且部分 kind 根本没有这个概念——所以必须做成
  **可选**能力，未声明的 kind 启动方式逐字节不变。

## 改文件 / Files

- `internal/agent/launch.go` — `RunnerProfile` 增加可选 effort 构造器
- `internal/agentcfg/agentcfg.go` — `Override` / `Effective` 增加 Effort
- `internal/orchestrate/` — 启动路径按 task tier 传入 effort
- 对应测试

**不要**改 `BuildArgs` 签名/默认输出、不要改账本、不要改 owner/reviewer 选择。

## 契约 / Contract

### 1. 可选的 per-kind effort 构造器（纯增量）

```go
// RunnerProfile
// EffortArgs renders this CLI's reasoning-effort flag(s). nil means the kind has
// no effort control — its launch stays byte-for-byte unchanged.
EffortArgs func(effort string) []string
```

参考映射（未列出的 kind 一律 nil）：

| kind | 形状 |
|---|---|
| `codex-cli` | `-c model_reasoning_effort=<effort>` |
| `claude-code` | `--effort <effort>` |
| `kimi-cli` | 低档用 `--no-thinking`，其余给 thinking effort |

若某 kind 的真实 flag 与上表不符，**以该 CLI 实际支持的为准**并在注释里说明；
拿不准就置 nil（安全默认：不变）。

### 2. 解析与传递

```go
// Override
Effort string // "", "low", "medium", "high"; "" = 不注入

// Effective
Effort string // 已解析的档位，供 display/telemetry
```

`ResolveWith` 在 `EffortArgs != nil` 且 `Effort != ""` 时，把
`EffortArgs(effort)` **追加**到 Args；否则 Args 逐字节不变。

### 3. tier → effort 阶梯

```go
func EffortForTier(t Tier) string
```

| Tier | Effort |
|---|---|
| L0 | `low` |
| L1 | `medium` |
| L2 | `medium` |
| L3 | `high` |

**L2 刻意不升档**：tier 定的是**起始**预算，只有真实失败才买更多推理
（failure-driven escalation）。不要"顺手"把 L2 提到 high。

### 4. 接线

启动一棒时，用该 task 的 `extractTier(spec)` 求出 tier → `EffortForTier` →
经 `Override.Effort` 传进 `ResolveSeat`。显式的 per-seat 配置覆盖优先于 tier
（操作者显式设定必须绝对生效）。

## 验收 / Acceptance

评审维度：**correctness**。

- `EffortForTier`：L0→low、L1→medium、L2→**medium**、L3→high。
- 声明了 `EffortArgs` 的 kind：L0 启动参数里出现该 CLI 的低档 flag；
  L3 出现高档 flag。
- **未声明 `EffortArgs` 的 kind：启动参数逐字节等于今天**（钉死回归测试）。
- `Effort == ""` 时 Args 逐字节不变。
- 显式 per-seat Override 优先于 tier 推导值。
- `BuildArgs` 的默认输出未被改动（既有 `launch_test` / `agent_test` 保持绿）。
- 门绿：`go test ./internal/agent/ ./internal/agentcfg/ ./internal/orchestrate/`
