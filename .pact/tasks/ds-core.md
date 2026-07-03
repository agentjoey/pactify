# ds-core：DataSource 抽象接口 + LocalServeSource + Context/hook（P1 地基）

> 设计见 `.agent/plans/product-form-and-frontend-2026-07-04.md` §2.1 与 `.agent/plans/sprint-p1p2-foundation-2026-07-04.md`。
> 先读现有 `web/src/lib/api.ts`（~40 函数，全打本地 serve `/api/*`）与 `web/src/lib/types.ts`（State/Feature/Task/Seat）。

## 目标
给 `web/` 引入数据源抽象层，**不改任何组件**（下一棒 ds-wire 才接线）。纯新增。

1. **`web/src/lib/datasource.ts`（新）** — 定义并导出：
   - `interface DataSourceCapabilities { canWrite: boolean; canOrchestrate: boolean; multiMachine: boolean }`
   - `interface DataSource`，方法覆盖现有 api 的读+写两类（读用统一 State 形状 = `types.ts::State`）：
     - 读：`listProjects(): Promise<ProjectMeta[]>`、`getState(id, wt?): Promise<State>`、`getStats(id): Promise<ProjectStats>`、`subscribe(id, onState: (s: State)=>void): () => void`（返回取消订阅函数）
     - 写/控制（可选方法，托管侧 U3 前不实现）：`verb?(id, ...)`、`runOrchestrate?(id, ...)`、`shipFeature?(...)` 等 —— 覆盖 api.ts 现有 postVerb/runOrchestrate/resumeOrchestrate/shipFeature/generatePlan/applyPlan/postTask 的签名（照搬其入参类型）
     - `capabilities: DataSourceCapabilities`
   - `class LocalServeSource implements DataSource`：每个方法**直接委托** api.ts 现有函数（`getState`=`fetchState`，`subscribe`=复用现有 SSE 订阅逻辑/EventSource），`capabilities = {canWrite:true, canOrchestrate:true, multiMachine:false}`。
2. **Context + hook**：`DataSourceContext`（React.createContext）+ `useDataSource(): DataSource` hook + `DataSourceProvider`（默认注入 `new LocalServeSource()`）。放 `datasource.ts` 或 `datasource.tsx`（含 JSX 则 .tsx）。

## 文件
- 建 `web/src/lib/datasource.tsx`（+ 若拆 `datasource.test.tsx`）
- **只读** `web/src/lib/api.ts` / `types.ts`（照搬签名，不改它们）
- 不碰任何 `web/src/components/**` 与 `App.tsx`

## 纪律
- **TDD**：先写失败 vitest（LocalServeSource 委托到 api 的 mock、capabilities 值、subscribe 取消），红→实现→绿。
- LocalServeSource 是**薄委托**，不重实现逻辑；subscribe 复用现有 SSE 机制（查 App.tsx/现有 EventSource 用法，抽成可复用）。
- 严格 TS，无 `any` 逃逸（写方法入参类型照搬 api.ts）。不引入行为变化。
- 完成跑 verify 绿再 checkpoint。

verify: cd web && npx tsc --noEmit && npx vitest run src/lib
