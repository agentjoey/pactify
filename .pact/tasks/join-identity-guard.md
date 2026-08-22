# join-identity-guard — `join <seat>` 与当前身份不一致时必须报错，不得静默改别的座席

tier: L1
verify: go test ./internal/pact/ && bats tests/join.bats tests/accept.bats
dimension: correctness

## 问题（**已实测发作，账本里有脏数据**）

`pactify join <seatID>` 的 argv 座席**只用于 gate 检查**，而写进账本的 join 事件用的是
**解析出的当前身份**（`joinWithClientLocked`，`internal/pact/engine.go:367-419`）：

```go
id, err := p.agentID()          // ← PACT_AGENT_ID / .pact/seat / .As()
...
if err := p.appendAndRender(event.Event{
    AgentID: id,                // ← argv 的 seatID 从未用于事件
    ...
    Payload: payload,           // ← 但 roles / kind 全来自本次命令行
}); err != nil {
```

`seatID` 只出现在 `checkJoinGate(preState, seatID)` / `checkJoinTarget(preState, seatID, taskID)` 里。

### 实测（2026-08-22，本仓）

```
PACT_AGENT_ID=claude pactify join agy-w --kind antigravity --roles worker
```

**退出码 0，无任何输出**，但账本里出现的是：

```json
{"agent_id":"claude","event_type":"join",
 "payload":{"kind":"antigravity","roles":["worker"],...}}
```

即：它没有创建 `agy-w`，而是**把 orchestrator/reviewer 座席 `claude` 重新登记成了
kind=antigravity**。投影按 `if jkind != "" { st.Agents[ai].Kind = jkind }`
（`projection/project.go:248-249`）覆写了 `claude` 的 Kind。roles 侥幸保住（仅在原本为空时才写），
Kind 没有。

后续 `orchestrate` 从名册解析 seat→kind 时因此把 `claude` 当成 antigravity 座席，
触发了 `[KIND]` 缺陷（见 `.pact/tasks/kind-effective.md` 的实测记录）。

**危害不在于「限制」而在于「无声」**：调用方拿到 exit 0，理所当然继续往下走。
被脚本/agent 驱动时尤其糟——agent 看到 0 就认为 join 成功了。

## 目标 / Goal

argv 的座席与当前解析出的身份不一致时，**报错并非零退出**，不写任何事件。

## 契约 / Contract

1. 在 `joinWithClientLocked` 里，解析出 `id` 之后、**追加任何事件之前**，
   若 `seatID != ""` 且 `seatID != id`，返回错误并终止（不 append、不 checkout）。
2. 错误信息必须可操作，点明三件事：当前解析到的身份、它的**来源**、以及怎么改。
   身份来源用既有的 `p.ResolveSeat()` 返回的 `source`（`"actor"` / `"env"` / `"file"`，
   见 `engine.go:50-59` 的文档），**不要**新造来源判定。例如：

       pactify join: 要 join 座席 "agy-w"，但当前身份是 "claude"（来自 env PACT_AGENT_ID）。
       join 只能登记自己：先 `export PACT_AGENT_ID=agy-w` 再重试。

   文案可自行措辞，但必须含：目标座席、当前身份、来源、修复动作。
3. `seatID == id` 时行为**逐字节不变**（现有全部 join 测试与 bats 不得改断言）。

## 不要做 / Out of scope

- **不要**改成「以 argv 为准」去登记别的座席——那会让任何进程冒充任意座席，
  破坏 seat-identity 契约（`docs/specs/` 下的 seat-identity §3.1）。修法是**拒绝**，不是顺从。
- **不要**动 `checkJoinGate` / `checkJoinTarget` 的既有语义。
- **不要**改 `projection` 的 join 折叠逻辑（Kind 覆写本身是 WS-K 动态座席的正常行为，
  问题出在事件被写成了错误的 agent_id）。
- **不要**给已有的脏数据写「补偿事件」——账本修正已由人工完成，不在本任务范围。

## 验收 / Acceptance

评审维度：**correctness**。

- 身份不一致 → 返回非 nil error，且**账本零新增事件**（比较调用前后 `log.jsonl` 的行数），
  分支也未被切换。
- 错误信息包含目标座席名、当前身份名、来源标识。
- 三种来源都覆盖：`.As()`（actor）、`PACT_AGENT_ID`（env）、`.pact/seat` 文件（file）——
  每种都断言不一致时报错且来源在文案里正确。
- 身份一致时：join 正常，事件 payload 与既有测试**逐字节一致**。
- `bats tests/join.bats` 全绿（现有用例不得改）。
- 门绿：`go test ./internal/pact/ && bats tests/join.bats tests/accept.bats`
