# tierui-backend — tier/effort 的服务端数据面（三态契约 + 路径加固）

tier: L2
verify: go test ./internal/orchestrate/ ./internal/serve/
dimension: correctness

## 目标 / Goal

让前端能拿到 tier（与引擎同源）、verify、dimension、role 与运行时 effort。
**本任务只做后端，不碰任何前端文件。**

设计事实源：`.agent/frontend-design/exec-tiering-ui.md`（rev 3，已过两轮独立设计复核）。
背景规格：`docs/specs/execution-tiering.md`。

## 已核实的事实（不要重新调查，直接用）

1. `serve` **已经** import `orchestrate`（`stream.go` / `stint.go` / `fallback_proposal.go`），
   无循环依赖。
2. ⚠️ **三态必须保留**：`ParseTier`（`gate.go:67`）把缺失/空/无法识别一律折叠成 `TierL1`，
   而 `extractTier`（`gate.go:81`）写的是 `raw, _ := extractField(...)` —— **丢弃了 ok 标志**。
   若直接导出 `extractTier`，"spec 没写 tier" 与 "spec 明确写了 L1" 将无法区分，
   产品决策（要能看出 planner 漏标）直接失效。**这是本任务最容易做错的地方。**
3. 引擎读 tier 的路径是 `readSpec`（`loop.go:1427`，未导出）+ `extractTier`。
   **不要让 serve 重新实现 readSpec** —— 重实现必然与引擎语义漂移。
4. `agentcfg.ResolveSeat(seat, kind, effort)` 已导出且是纯函数；
   serve 已有 `resolveSeatKinds`（`internal/serve/orchestrate.go:272`）。
5. `t.Spec` 由 LLM 生成且**无路径校验**（`manifest.go` 只查非空）。

## 改文件 / Files

- `internal/orchestrate/gate.go`（或就近新文件）—— 导出 `SpecTier`
- `internal/serve/plan.go` —— DTO 加字段 + 从 spec 读 tier + 冲突标记
- `internal/serve/orchestrate.go` —— 运行时 effort 解析
- 对应测试

**不要**改：任何 `web/` 下的文件、`orchestrate.Status` 结构、账本、协议、
`ParseTier`/`extractTier` 的现有语义（只新增导出，不改既有行为）。

## 契约 / Contract

### 1. 保留三态的导出（跨越「读文件 + 解析」，单一路径）

```go
// SpecTier reads specRel under dir and reports the raw `tier:` value and whether
// a tier line was present at all. present=false covers: empty specRel, a spec that
// cannot be read, no `tier:` line, and a bare `tier:` with no value — all of which
// the engine runs as L1, but which the UI must be able to distinguish from an
// explicit `tier: L1`.
func SpecTier(dir, specRel string) (raw string, present bool)
```

要求：
- 内部复用既有 `readSpec` + `extractField`（**不要**新写解析或读盘逻辑）
- 裸 `tier:`（无值）→ `present=false`（`extractField` 既有语义，需测试钉死）
- **路径加固**：`specRel` 为绝对路径、或 `filepath.Rel(dir, filepath.Join(dir, specRel))`
  以 `..` 开头时，**直接返回 `("", false)` 且不读盘**。（决策②把 `t.Spec` 变成了
  HTTP handler 内的 `os.ReadFile`，必须拒绝逃逸。）

### 2. `planTaskDTO` 扩展（`internal/serve/plan.go`）

新增字段：`Tier`（规范化后的 L0..L3）、`TierMissing bool`、`Dimension`、`Role`、
`TierConflict string`（可空）。`Verify` 已存在，保持透出。

填充逻辑：
- `raw, present := orchestrate.SpecTier(p.Path, t.Spec)`
- `TierMissing = !present`；`Tier = string(orchestrate.ParseTier(raw))`
- **冲突标记（免费）**：manifest 的 `t.Tier` 与 spec 值都存在且规范化后不一致时，
  `TierConflict` 写成人类可读的一句（例：`manifest says L3, spec file says L0 — the engine will use L0`）。
  引擎用 spec 值，所以 `Tier` 永远取 spec 侧。

### 3. 运行时 effort（**不改 `orchestrate.Status`**）

在 serve 侧解析，不要把值从 runner 回穿到 Status：

```go
eff, _ := agentcfg.ResolveSeat(status.Seat, kindOf(status.Seat), orchestrate.EffortForTier(tier))
// eff.Effort 即已解析值(含 per-seat 覆盖);无 EffortArgs 的 kind 返回 ""
```

透出 `Tier` 与 `Effort` 给运行时状态接口。`Effort == ""` 是**多数情况**
（仅 claude-code/codex-cli 声明 `EffortArgs`），属正常，不是错误。

## 验收 / Acceptance

评审维度：**correctness**。

- `SpecTier` 三态：`tier: L2` → `("L2", true)`；无 tier 行 → `("", false)`；
  **裸 `tier:` → `("", false)`**；spec 文件不存在 → `("", false)`；`specRel==""` → `("", false)`。
- **路径加固**：`specRel` 为 `../../etc/passwd` 或绝对路径 → `("", false)` 且**未读盘**
  （用一个真实存在的仓外文件做证：断言其内容绝不出现在结果里）。
- DTO：显式 `tier: L1` 的 spec → `Tier="L1", TierMissing=false`；
  无 tier 行 → `TierMissing=true`（**这是产品要的区分，必须有测试**）。
- 小写 `tier: l2` → `Tier="L2"`（`ParseTier` 归一化）。
- 冲突：manifest `L3` + spec `L0` → `Tier="L0"` 且 `TierConflict` 非空。
- effort：某 kind 无 `EffortArgs` → `Effort==""` 且不报错；
  per-seat 覆盖存在时显示覆盖值而非 tier 推导值。
- 门绿：`go test ./internal/orchestrate/ ./internal/serve/`（**必须全绿**——包门当前是干净的，
  任何红都是本次引入的）。
