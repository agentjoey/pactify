# agy-tokens — 捕获 agy 的 token 用量

tier: L1
verify: go test ./internal/orchestrate/ ./internal/tokens/
dimension: correctness

## 目标 / Goal

把 agy headless JSON 输出里的 token 用量记进 pactify 既有的 per-task token store。

依赖：`agy-kind` 已 accepted（argv 已含 `--output-format json`）。
计划：`.agent/plans/antigravity-cli-integration-2026-08-22.md` §4 A2。

## 已核实事实（实测样本，勿重新调查）

agy `--output-format json` 的真实输出：

```json
{"conversation_id":"9940825b-…","status":"SUCCESS","response":"…",
 "duration_seconds":4.72,"num_turns":1,
 "usage":{"input_tokens":20300,"output_tokens":31,"thinking_tokens":27,
          "cache_read_tokens":0,"total_tokens":20331}}
```

⚠️ **取 `usage.total_tokens`，不要自己算 input+output** —— `total_tokens` 已包含 thinking。
实测一次真实任务：`input 53,743 / output 2,248 / thinking 971 / total 55,991`。

pactify 侧已有 `internal/tokens` 与 `recordTaskTokens`（cmd 传输解析 headless JSON 的既有模式，
参考 codex/claude 的做法）。**复用它，不要另造 store。**

## 改文件 / Files

- `internal/orchestrate/`（token 解析接线）+ 测试

**不要**改 `internal/tokens` 的存储格式、不要动其它 kind 的解析。

## 契约 / Contract

- 从 stdout 捕获的 JSON 中取 `usage.total_tokens` → `recordTaskTokens`
- JSON 不可解析 / 无 `usage` 字段 → **记 0 并继续**，不得让 stint 失败
  （token 记录是遥测，绝不能影响交付）
- 与既有 kind 的解析互不干扰

## 验收 / Acceptance

评审维度：**correctness**。

- 给定上述真实样本 → 记录 20331（不是 20331 之外的任何算法结果）。
- 缺 `usage` / 畸形 JSON / 空输出 → 记 0，**不报错、不失败**。
- 一次真实 stint 后 `.pact/orchestrate/tokens.json` 有该 task 的非零计数（可在 evidence 里佐证）。
- 其它 kind 的 token 解析无回归。
- 门绿：`go test ./internal/orchestrate/ ./internal/tokens/`
