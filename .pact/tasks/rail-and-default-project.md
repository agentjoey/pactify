# Task rail-and-default-project (P0#4/#6) — RightRail 过期 affordance + 默认项目选择

## 1. RightRail:已 shipped 的任务不再给过期操作(根因已定位)
现状(RightRail.tsx):
- :378 Merge 块条件只有 `author && project`——已 shipped 的 feature 仍显示 "Merge <feature>" 按钮。
- 任务状态徽标显示 ACCEPTED(task.status 停在 accepted;SHIPPED 列其实由 feature 状态派生)。
- :222 `{inFlight} in flight` 对已完结任务仍显示("9h in flight")。
修(RightRail 能拿到 state/feature 数据,找到该 feature 的 status):
- feature.status === "shipped" 时:**隐藏整个 Merge 块**;
- 任务徽标:feature shipped 时显示 SHIPPED(样式沿现有徽标,用 SHIPPED 列同色);
- `in flight` 仅当任务未 accepted/未 shipped 时显示(完结任务不显示该字样;⏱ duration 保留)。

## 2. 默认项目:记住上次选择 + 避开死项目
现状(App.tsx:131):恒选 `ps[0]`——落在死项目(pact-dogfood-squad,.pact 已不存在;
API 对这类项目回 `project:"unknown"`)。修:
- 选中项目变化时写 `localStorage["pactify:lastProject"] = id`(在 setCurrent 的调用点集中做,
  或一个 useEffect(current))。
- 初始选择顺序:①localStorage 里的 id 且仍在列表 → 用它;②否则列表里第一个
  `p.project !== "unknown"` 的;③否则 ps[0]。(ProjectMeta 若无 project 字段,给 api.ts 的
  类型补上——后端 /api/projects 已经返回它。)

## 改文件
web/src/components/RightRail.tsx(+test) · web/src/App.tsx · web/src/lib/api.ts 或 types.ts(类型补 project 字段,若缺)· App 相关 test(若有初始选择测试)

## 测试
- RightRail:shipped feature → 无 Merge 按钮、徽标 SHIPPED、无 "in flight";active feature 回归不变(Merge 照旧)。
- 初始选择:localStorage 命中用它;无命中跳过 project==="unknown" 的首项。(App 级测试若重,
  可把选择逻辑抽成纯函数 pickInitialProject(ps, stored) 单测。)

## 验收 / Acceptance(视角: correctness — 不给已完结 feature 危险按钮、默认项目可用、回归零)
verify: cd web && npx tsc --noEmit && npx vitest run
