# ds-wire：组件接 useDataSource + 写控件 capability 门控（P1 收尾）

> 依赖 ds-core（DataSource 接口 + LocalServeSource + Context/hook 已 merged/可用）。
> 设计见 `.agent/plans/product-form-and-frontend-2026-07-04.md` §2.1。

## 目标
把 `web/` 顶层数据装载与主写操作从「直连 api.ts」改走 `useDataSource()`，并按 `capabilities.canWrite` 门控写控件。**本地 capabilities 全 true → 行为零变化**（全绿 = 验收）。范围克制。

1. **Provider 挂载**：App 根裹 `DataSourceProvider`（注入 `LocalServeSource`）。
2. **顶层数据装载改走 hook**：App.tsx（及承载 board 数据的容器组件）读 projects/state/stats/subscribe 改用 `useDataSource()` 的对应方法，替换直接 import 的 `fetchProjects/fetchState/getStats/SSE`。
3. **主写操作改走 hook**：Board 的 Accept/Changes、orchestrate run/resume、ship、dispatch/plan 等**顶层写动作**改走 `src.verb?/runOrchestrate?/...`。
4. **capability 门控**：写控件渲染前查 `src.capabilities.canWrite`——false 时隐藏或禁用并给可访问提示（如 title「远程控制需 U3」）。本 sprint 本地恒 true，但门控逻辑要就位（为 P3 RelaySource 铺路）。
5. 叶子展示组件**继续吃 props**，不逐个改造。

## 文件
- 改 `web/src/App.tsx` + 直接做数据装载/主写的容器组件（如 Board 容器、LiveOrchestrate、DispatchPanel 等——按实际 import api.ts 的位置定位）
- 相应更新受影响组件的 vitest（mock 从 api → DataSource/Provider）
- 不改 api.ts 的实现（LocalServeSource 仍委托它）

## 纪律
- **零回归**：现有 vitest + playwright 必须全绿。改的是「数据从哪来」的接线，不是行为。
- **范围克制**：只接顶层数据 + 主写路径；不追求把每个组件都改造成 source-aware（那是 P3+ 增量）。
- capability 门控要有对应测试（canWrite=false 时写控件不可用——可注入一个 capabilities.canWrite=false 的 fake source 测）。
- 完成跑 verify 三绿再 checkpoint。

verify: cd web && npx tsc --noEmit && npx vitest run && npx playwright test
