# tierui-fixes — 独立实现复核的 findings 1–5

tier: L2
verify: go test ./internal/serve/ && cd web && npx vitest run && npx tsc -b --noEmit
dimension: correctness

## 目标 / Goal

修掉独立 Review/Verification Agent 在**真实 build 实测**后提出的 5 项 should-fix。
无 blocking 项——方向已通过，这些是发布前的收口。

上下文：`.agent/frontend-design/exec-tiering-ui.md`（rev 3）。
**不要重做设计**，只按下列各条修。

## 五项修复（逐条，都很小）

### 1. 性能回归：3 秒轮询上跑了两次全量账本 fold

`internal/serve/orchestrate.go:175` 的 `runtimeTierEffort` 调用
`ProjectStateAt(dir, -1)`（`dto.go:152` —— `event.ReadAll` + `projection.Project`，**无缓存**），
随后 `s.resolveSeatKinds(dir)` 又做了**第二次**全量读+fold。
`RunRail.tsx:193` 每 **3000ms** 轮询一次该端点。本仓账本 587 事件 / **296KB**。
本改动之前该 handler 只是一次 `status.json` 的小 `os.ReadFile`。

**仓库早有 memo 版且正是为此而加**（v0.9.0 "serve 全量 fold 加 memo"）：
`(*Server).projectStateFull(id, projectRoot)`（`dto.go:106`，按 log 路径+size+mtime 记忆化）。
新代码绕过了它。handler 作用域内**已有** project id（`orchestrate.go:129`）。

**修**：`ProjectStateAt(dir,-1)` → `s.projectStateFull(id, p.Path)`；
`resolveSeatKinds` 要么一并记忆化，要么直接从 `projectStateFull` 已返回的事件里推导座席 kind。
**不要**新造缓存机制。

### 2. rev 3 矩阵有一行未实现：无法识别的 tier 原始值被丢弃

矩阵 A 要求：*"spec 有 `tier:` 但值无法识别 → 显示 L1 + **原始值入 title**"*。
但 `plan.go:103-104` 算完 `ParseTier(raw)` 就把 `raw` 丢了，DTO 也**没有字段能承载**，
`TierBadge.tsx:53` 于是回落到 `TIER_TITLE[tier]`。

后果落在本功能存在的**核心场景**上：人手改 spec 纠正 tier 时打错字（`tier: L9` / `tier: high`），
界面与显式 `tier: L1` **byte 级无差别**，零反馈，而引擎静默按 L1 跑。

**修**：DTO 加 `TierRaw string \`json:"tier_raw,omitempty"\``，
**仅当** `ParseTier(raw)==L1 && !strings.EqualFold(strings.TrimSpace(raw), "L1")` 时填充；
`TierBadge` 在有 `tierRaw` 时把它编进 title（例：`spec 写的是 "L9"，无法识别 —— 引擎将按 L1 运行`）。
补测试：`tier: L9` 的 spec → `TierRaw="L9"` 且徽标 title 含该原始值。

### 3. a11y：补救措施自己低于 AA（实测数字，不必复测）

| 元素 | 实测 | 要求 |
|---|---|---|
| `DispatchPanel.tsx:169` 图例 | **3.46:1** / 10px | ❌ |
| `DispatchPanel.tsx:184` `verify` 行 | **3.46:1** / 11px | ❌ |
| `RunRail.tsx:597` effort 说明 | **3.36:1** / 9px | ❌ |

图例正是 rev 3 为"`title` 是鼠标专属，键盘/触屏用户看不到"而引入的**非鼠标通路**，
它自己却不达标；`verify` 行是决策①的 scope 证据，3.46:1 下读起来只是又一行灰字。

**修**：图例与 `verify` 行改用 `--color-text-2`（同表面实测 **5.77:1** ✅）；
RunRail 的 effort 说明改 `text-2` 且字号 **≥10px**。
`owner → reviewer` 与 `dimension · role` 保持 `text-3` 不动（既有色调、最低优先级元数据）。

### 4. 冲突警告只能鼠标看到

`TierBadge.tsx:53` 把 `conflict` 传成了 `title` 但**没传 `ariaLabel`**（只有 `missing` 分支传了），
而所有徽标实测 `tabIndex === -1` → *"manifest says L3, spec file says L2…"* 这条
**本功能最重要的诊断**只有鼠标悬停可达，键盘/读屏用户只看到一个普通 `L2` 徽标。

**修**：`conflict` 非空时一并传 `ariaLabel={conflict}`（`role="img"` 的管道已存在且已测）。
补测试。

### 5. 重新生成截图证据

现有截图早于最后一次 dist 重建（截图 01:33 / dist 01:38），违反工作流铁律 2
"最终截图必须来自修复后的最终 build"。

**修**：本轮全部改完、门全绿之后，重新跑
`cd web && npm run build && node scripts/shots.mjs`，并针对 plan review 的 tier 出一张图。
把截图路径写进 evidence。

## 顺带：把 6–9 登记进 backlog（仓库"已知妥协必登记"约定）

在 `docs/backlog.md` **追加**（禁止整文件重写）四行，各一句话即可：

- **PlanDock 是死代码**：`grep -rn "PlanDock" web/src` 只剩注释，dark-UI 重构（`1c3b676`）已从
  `App.tsx` 下掉；本次给它加的 tier 渲染与测试都作用于未挂载组件。要么重新挂载，要么删除。
- **`dimension · role` 行无 `truncate`**（`DispatchPanel.tsx:190`，`white-space: normal`），
  `role` 无后端校验、长值会折成两行（309px 处临界）。加 `truncate` + `title` 即可对齐 `verify` 行。
- **"manifest 有 tier、spec 没有"文案不准**：`plan.go:105` 只在两侧都有时报冲突，
  半完成的 dual-write 会显示 NO TIER 的"planner 未标注"，而实际是 planner 标了 manifest 漏了 spec。
- **`TierSlot` 的宽度依赖父级是 flex**（`TierBadge.tsx:75`），非 flex 父级下 `w-[34px]`/`shrink-0`
  静默失效且无测试会红；改 `inline-block` 可无条件健壮。

## 改文件 / Files

- `internal/serve/orchestrate.go`、`internal/serve/plan.go`
- `web/src/components/ui/TierBadge.tsx`、`web/src/components/shell/DispatchPanel.tsx`、
  `web/src/components/board/RunRail.tsx`、`web/src/lib/types.ts`
- 对应测试；`docs/backlog.md`（**只追加**）

**不要**改设计方向、不要动 `Badge.tsx`、不要重构无关代码、不要处理 6–9 的代码（只登记）。

## 验收 / Acceptance

评审维度：**correctness**。

- **1**：`runtimeTierEffort` 不再调用未缓存的 `ProjectStateAt`；同一 handler 内不再出现两次全量 fold。
- **2**：`tier: L9` 的 spec → DTO `TierRaw="L9"`，徽标 title 含原始值；
  显式 `tier: L1` → `TierRaw` 为空（不得对正常值也填）。
- **3**：图例、`verify` 行、effort 说明均改为 `text-2`（effort 字号 ≥10px）。
- **4**：`conflict` 非空 → 徽标同时有 `role="img"` 与 `aria-label`（内容为冲突文案）；
  无冲突时不得平白加 role。
- **5**：截图来自本轮修复后的最终 build，路径写进 evidence。
- `docs/backlog.md` 追加了 4 行（未重写全文）。
- 门全绿：`go test ./internal/serve/` + `cd web && npx vitest run && npx tsc -b --noEmit`
  + `npx playwright test`。
