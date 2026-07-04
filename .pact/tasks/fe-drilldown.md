# fe-drilldown：托管模式任务详情（点卡片→解密事件历史）（B / FE-10）

> 托管 web（RelaySource）现在能渲染 board，但点任务卡片没有详情。目标：托管模式下点卡片，展示该任务**解密后的事件历史**（spec 路径 / evidence / changes 原因 / 各事件 payload）。先读 `web/src/lib/relaysource.ts`、`@pactify-apps/relay-client`（`getProjectEvents`/`decrypt`）、`web/src/components/RightRail.tsx`（现有本地详情侧栏样式）、`web/src/lib/datasource.ts`（DataSource 接口）。

## 目标
1. **RelaySource 暴露解密事件**：给 `RelaySource` 加 `getEvents(project): Promise<PactEventDetail[]>`——`client.getProjectEvents(id)` → 每条 `client.decrypt(id, bodyEnc)` 还原完整 pact 事件 + 保留明文头（seq/eventType/task/feature/ts）。返回 `{ seq, eventType, task, feature, ts, body }[]`（body = 解密后的对象，含 evidence/reason/spec/payload 视事件类型）。
   - 可选:在 `DataSource` 接口加**可选** `getEvents?`（LocalServeSource 可不实现或走 `/api` events），组件按能力用。
2. **详情面板组件**（`web/src/components/TaskDetail.tsx` 新）：给定 project + 选中 taskId，调 `src.getEvents?.(project)`（若有）过滤该 task 的事件，按时间倒序渲染:每条 = 事件类型徽标 + 时间 + 关键字段（checkpoint→evidence、changes→reason、assign→owner/reviewer/spec、accept/merge）。空/加载/无 getEvents（本地）时优雅降级（本地就沿用现有 RightRail,不强接）。
3. **接线**：托管模式（`capabilities.multiMachine` 或 `!canWrite` 作判据，或加个 capability 标记）下,点卡片（已有 `onSelectTask`/`selected`）时展示 `TaskDetail`。不破坏本地模式现有 RightRail。

## 文件
- 改 `web/src/lib/relaysource.ts`（+getEvents）、`web/src/lib/datasource.ts`（可选 getEvents? 到接口）
- 建 `web/src/components/TaskDetail.tsx` + `TaskDetail.test.tsx`
- 接线处（App/Board/RightRail 之一，按现有 selected 流）
- `web/src/lib/relaysource.test.ts` 补 getEvents 测试（mock client）

## 纪律
- **本地模式零回归**：本地不实现 getEvents 就走现有 RightRail,不报错。
- 复用现有侧栏/卡片样式(dark UI),别造新风格。
- 测试:RelaySource.getEvents（mock client 返回构造事件→断言解密+过滤）、TaskDetail 渲染（mock 事件）。
- 零回归:tsc + 全量 vitest + playwright 全绿。

verify: cd web && npx tsc -b && npx vitest run && npx playwright test
