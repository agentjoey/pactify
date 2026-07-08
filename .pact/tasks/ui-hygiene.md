# Task ui-hygiene (P3#21/#22/#23/#24) — 死代码 / 无障碍 / 硬编码色 / 常量收敛

## 1. 死代码(视图整合遗留)
- 删 `CanvasSkeleton`/`OpsSkeleton`(web/src/components/Skeleton.tsx:47,61)+ Skeleton.test.tsx 对应用例。
- 删 `getSessionsList`(web/src/lib/api.ts:444 附近,全仓无调用;若真有调用先确认再动)。
- 删 App.tsx 的 `dispatchGoal` 残留 state(App.tsx:72 注释自述 canvas NL dock 已退役;确认 DispatchPanel
  的 prop 是否仍需要——若 DispatchPanel 接口需要,则传常量 "" 并删 state)。
- RightRail.tsx:298 的 `TODO(W3) replay` 注释:改成一行说明(replay 已随 ?at= 移除)或删。

## 2. 无障碍
- 找出无 accessible name 的 icon 按钮/链接(已知:header 里 live badge 旁按钮、event stream 折叠钮、
  footer link——grep `<button` 无 aria-label 的交互元素,补 aria-label)。
- Toolbar 的 seat avatar span(Toolbar.tsx:98-106 附近)改 button + aria-label(seat 名)。
- CockpitPanel 消息容器加 `role="log" aria-live="polite" aria-label="Conversation"`。

## 3. 硬编码色 → token
- Setup.tsx:208/216/241 的 `#ffd479`/`rgba(255,212,121,...)` → `var(--color-role-product)` 系;
  AgentConfig.tsx:250 的 `#8ab4ff` → `var(--color-role-design)`。目视等价即可(同族 token)。

## 4. 常量收敛
- App.tsx:36-44 的 STALE_MS/EVENTS_CAP/FETCH_FAIL_THRESHOLD 移到新 `web/src/lib/constants.ts`
  统一导出(值不变),App import。CockpitPanel 的轮询兜底 5s 也挪进去(若已是常量)。

## 改文件
上述各文件 + 对应 test(删死代码的测试同步删)。

## 测试
- 全量 vitest 绿(删除项无引用残留,tsc 会兜底)。
- a11y:新加 aria-label 的元素在相应组件测试里可按 role/name 查到(抽查 1-2 个)。

## 验收 / Acceptance(视角: maintainability — 零行为变化(除 a11y 属性)、tsc/vitest/build 全绿)
verify: cd web && npx tsc --noEmit && npx vitest run
