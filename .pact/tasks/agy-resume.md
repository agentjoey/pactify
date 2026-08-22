# agy-resume — agy 会话续接

tier: L2
verify: go test ./internal/orchestrate/
dimension: correctness

## 目标 / Goal

同一 (seat, task) 重试时续接上一次 agy 会话，而非冷启动——省掉重复的上下文重建。

依赖：`agy-tokens` 已 accepted（同一段 JSON 解析里就能拿到 conversation_id）。
计划：`.agent/plans/antigravity-cli-integration-2026-08-22.md` §4 A3。

## 已核实事实（勿重新调查）

- agy headless JSON 返回 **`conversation_id`**（实测样本：`"conversation_id":"9940825b-…"`）
- agy 提供 **`--conversation <id>`** 续接，以及 `--continue` 续接最近一次
- 形态与 codex 的 `thread_id` + `exec resume <id>` **同构**
- pactify 已有 `.pact/orchestrate/sessions.json` 会话 store 与
  `codexResumeArgsIfAny`（`runner.go` 约 151 行）的既有模式

⚠️ **复用既有模式，不要另造机制。** ACP 路径的 `LookupSession`/`RecordSession`/`ClearSession`
也已存在，优先复用。

## 改文件 / Files

- `internal/orchestrate/runner.go`（+ 必要的 helper）
- 对应测试

**不要**改 sessions store 的格式、不要动 codex/ACP 的续接逻辑。

## 契约 / Contract

- stint 成功后把 `conversation_id` 记入既有 session store，键为 (repoDir, seat, task)
- 下次同 (seat, task) 启动时，argv 追加 `--conversation <id>`
- **无记录 → 冷启动**（不追加该 flag），行为与今天一致
- 续接失败（agy 拒绝该 id）→ **清除记录并回落冷启动**，不得让 stint 直接失败
  （参考 ACP 路径 `LoadSession` 失败即 `ClearSession` 再 `NewSession` 的处理）

## 验收 / Acceptance

评审维度：**correctness**。

- 首次 stint 的 argv **不含** `--conversation`；成功后 store 有该 (seat,task) 的记录。
- 第二次同 (seat,task) 的 argv **含** `--conversation <上次的 id>`。
- 不同 task（或不同 seat）**不串用**同一 conversation。
- 续接被拒时清记录并冷启动，stint 不失败。
- 其它 kind 的续接（codex/ACP）无回归。
- 门绿：`go test ./internal/orchestrate/`
