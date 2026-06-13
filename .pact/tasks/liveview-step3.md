# liveview-step3 — dashboard live orchestration view panel

verify: npm run -C web test

## 目标 / Goal
前端 dashboard 加一个 **live 编排视图**面板：显示 orchestrate 当前在跑哪个 task、哪个 agent（座席）在干哪一棒、进度（accepted/total）、以及是否已升级（escalated）等待人工。数据来自 step2 的端点，轮询刷新。

## 改文件 / Files
仅可触碰以下文件（bounded set）：
- `web/src/lib/types.ts`（改）—— 新增 `OrchestrateStatus` 与 `OrchestrateStatusResponse` 类型。
- `web/src/lib/api.ts`（改）—— 新增 `getOrchestrateStatus(project)`。
- `web/src/components/TopBar.tsx`（改）—— `View` 联合类型加 `"live"`，`VIEWS` 列表加一项（label 例如 "Live"，分配一个未占用的快捷键）。
- `web/src/components/LiveOrchestrate.tsx`（新建）—— 面板组件。
- `web/src/components/LiveOrchestrate.test.tsx`（新建）—— 组件测试（vitest + RTL，参照 `Agents.test.tsx` 等既有写法）。
- `web/src/App.tsx`（改）—— `view === "live"` 时渲染 `<LiveOrchestrate>`。

禁止改动 orchestrate / serve / cmd 任何文件。Canvas 相关文件不要碰。

## 契约 / Contract
**类型（与 step1/step2 的 JSON 逐字对齐）：**
```ts
export interface OrchestrateStatus {
  feature: string;
  task: string;
  seat: string;
  action: "run_owner" | "run_reviewer" | "merge" | "stuck" | "idle" | "done";
  phase: string;
  escalated: boolean;
  reason?: string;
  done: boolean;
  total: number;
  accepted: number;
  iter: number;
  updated_at: string;
}
export interface OrchestrateStatusResponse {
  present: boolean;
  status?: OrchestrateStatus;
}
```

**api.ts：**
```ts
export const getOrchestrateStatus = (project: string) =>
  getJSON<OrchestrateStatusResponse>(`/api/projects/${project}/orchestrate/status`);
```

**LiveOrchestrate 组件：**
- props：`{ project: string; refreshTick: number }`（沿用 App 既有的 `refreshTick` 模式，在 SSE 更新时重取；并自带一个轻量定时轮询，例如每 3s，组件卸载时清理）。
- 渲染分支：
  - `present === false` → 友好空态："orchestrate 尚未运行"。
  - `escalated` → 醒目的升级横幅，显示 `reason`，提示需人工介入。
  - `done` → "已收工 / 全部交付"。
  - 否则 → 当前 **task**、**seat**（哪个 agent）、**action/phase**（在干哪一棒）、进度 `accepted/total`、iter。
- 用现有 design tokens / 既有组件样式风格；无障碍：面板有可识别的 label。

**App.tsx：**
- 在视图切换处增加 `view === "live"` 分支渲染 `<LiveOrchestrate project={current} refreshTick={refreshTick} />`（与 ops/canvas 分支并列）。

## 验收 / Acceptance
Reviewer 确认：
1. `npm run -C web test` 全绿。
2. `npm run -C web lint` 无新增报错（建议本地先跑）。
3. 组件测试覆盖：`present:false` 空态、`escalated` 横幅、正常运行态（显示 task/seat/phase/进度）三种渲染。
4. TopBar 的 `View` 类型与 VIEWS 列表新增 `"live"`，App 能切到该视图并渲染面板。
5. 类型与 step2 JSON 契约逐字一致；未触碰 Canvas/serve/orchestrate 文件。
