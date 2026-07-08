# Task board-blocked-visibility (P2#17) — 卡片显示"卡在哪":依赖阻塞 + stale 原因

## 问题
看板说不出任务为什么不动:任务有 `deps`(Task.deps: string[],types.ts:2 已有,DTO 已传)但
无任何呈现;stale 橙点只有 "in_progress > 30min" 一句 tooltip,无原因。

## 修法
1. **阻塞徽标**:ASSIGNED/WORKING 列的卡片,若其 `deps` 中存在**未 accepted/shipped** 的任务,
   卡片显示一个小徽标 `⧗ awaiting <depId>`(多个取第一个 + `+N`;title 列全里所有未完成 dep)。
   data-testid="blocked-badge"。计算放 Board(有全量 tasks 映射)传给 TaskCard,或 TaskCard 收
   一个 `blockedOn?: string[]` prop——选后者(TaskCard 保持展示组件)。
2. **stale tooltip 加因**:stale 点的 title 从固定文案改为组合:`in progress >30min` + 若同时
   blockedOn 非空,附 `; awaiting <ids>`。
3. 徽标样式:小号、amber 系(与 stale 点同族),不挤压现有布局(放 owner 行右侧或 stat 行)。

## 改文件
web/src/components/Board.tsx · web/src/components/TaskCard.tsx · 各自 test

## 测试
- dep 未完成 → blocked-badge 出现且文案含 dep id;dep accepted/shipped → 无徽标。
- 多 dep 未完成 → `+N` 计数;title 列全。
- 无 deps 任务零变化。

## 验收 / Acceptance(视角: ux — 一眼看出等谁、无布局挤压、零误报)
verify: cd web && npx tsc --noEmit && npx vitest run src/components/Board.test.tsx src/components/TaskCard.test.tsx
