# t-sp2-frontend：dashboard 驱动 UI（SP2 前端）

> 完整要点见 `docs/superpowers/plans/2026-06-17-sp2-dashboard-drive.md` 的 **Task t-sp2-frontend**；
> 设计见 `docs/superpowers/specs/2026-06-17-sp2-dashboard-drive-design.md`。先读它们 + 现有组件。

## 目标
6 处 UI，调用已 ship 的后端 endpoint（`/orchestrate/{run,resume,ship,diff}` + `/plan/{feature}/apply`）：

1. `PlanReview.tsx`：加「Apply」按钮 → `POST .../plan/{feature}/apply` → 成功 toast/刷新、失败显错误。
2. `LiveOrchestrate.tsx`：非运行态+roster 完整 → 「Run」→ `POST .../orchestrate/run`。
3. `LiveOrchestrate.tsx`：escalated 态 → escalation 详情 + 「View diff」（`GET .../orchestrate/diff` 渲染 `<pre>`）+「Resume」→ `POST .../orchestrate/resume`。
4. `LiveOrchestrate.tsx`：shipped 态 → 「Ship」→ PR title/body 输入 → `POST .../orchestrate/ship` → 显示 PR URL。
5. `ops/OpsView.tsx`：每 agent「Prune sessions」→ 已有 `POST /api/agents/{kind}/sessions/prune`。
6. `lib/api.ts`：加 `applyPlan/runOrchestrate/resumeOrchestrate/shipFeature/getDiff/pruneSessions`（复用现有 `writeJSON` 模式）。

## 文件
- 改 `web/src/lib/api.ts`、`PlanReview.tsx`、`LiveOrchestrate.tsx`、`components/ops/OpsView.tsx`
- 改/建对应 `*.test.tsx`

## 纪律
- **TDD**：先写失败 vitest → 红 → 实现 → 绿。每按钮测 POST URL/body + 成功/失败态。
- **画布工艺规约**（spec 2026-06-12 §5）：节点位置只两写入者、RF 节点走 merge-by-id、禁伪造 RF 几何。
- 复用现有组件/样式模式，勿重造。完成跑 verify 双绿再 checkpoint。

verify: cd web && npx tsc --noEmit && npx vitest run && npx playwright test
