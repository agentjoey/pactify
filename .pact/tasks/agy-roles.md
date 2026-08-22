# agy-roles — 把 agy 绑定到轻量类 role

tier: L1
verify: go test ./internal/roles/ ./internal/agentcfg/ ./internal/planner/
dimension: correctness

## 目标 / Goal

按 Human Owner 指定的定位，把 antigravity 座席绑定到**轻量类工作**的 role，
让 planner 的 role routing 自动把这类任务派给它。

依赖：`agy-kind` 已 accepted。
计划：`.agent/plans/antigravity-cli-integration-2026-08-22.md` §4 A5。

## Human Owner 决策（已定，不要自行更改）

- **成本不作为考量**（走 Google AI 订阅）——原计划的"成本压测定档"已取消。
- **定位**：按当前模型能力（`gemini-3.7-flash`），agy 适合
  **轻量任务 / 前端开发 / 测试 / 运维 / 文档维护**。
- ⚠️ **不要**把 agy 设为 planner / orchestrator 的默认，也不要绑架构类任务——它是轻量档。
- 落点是**机器级 role catalog**，**不是** tier 档（tier 由任务复杂度决定，与座席能力正交）。

## 已核实事实

- `internal/roles` 已有 `Profile{Name, Kind, Model, Fallback, Effort}` 结构
- planner 提示词已有 **role routing** 段落：会把 task 的 `role` 与 role catalog 对照，
  **优先**把该类工作派给绑定了该 role 的座席——这正是 role 字段被设计出来的用途
- agy 可用模型（实测 `agy models`）：`gemini-3.7-flash-{high,medium,low}` 等

## 改文件 / Files

- `internal/roles/`（若需内置默认 profile）或文档 + 配置示例
- 对应测试

**不要**改 planner 的 routing 逻辑（它已经能用）、不要改 tier 语义。

## 契约 / Contract

提供 antigravity 的推荐 role profile（内置默认或文档化配置皆可，实施时选一并说明理由）：

| role | Kind | Model |
|---|---|---|
| `frontend` | `antigravity` | `gemini-3.7-flash-medium` |
| `test` | `antigravity` | `gemini-3.7-flash-medium` |
| `ops` | `antigravity` | `gemini-3.7-flash-low` |
| `docs` | `antigravity` | `gemini-3.7-flash-low` |

⚠️ 若 `agy-kind` 实测发现**模型名内嵌档位会覆盖 `--effort`**，
则此处的 `Effort` 字段需留空并在模型名里编码档位——**以 agy-kind 的实测结论为准**。

## 验收 / Acceptance

评审维度：**correctness**。

- role profile 可被 `roles.Load()` 正确读出，`Kind` 为 `antigravity`。
- 绑定后 planner 的 prompt 中出现该 role 与其 kind/model（role catalog 渲染路径既有测试可复用）。
- 未绑定 role 的项目行为**逐字节不变**（回归钉死）。
- 与 `agy-kind` 的 effort 实测结论一致（若冲突，以实测为准并在 evidence 说明）。
- 门绿：`go test ./internal/roles/ ./internal/agentcfg/ ./internal/planner/`
