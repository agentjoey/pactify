# Canvas P0 交互地基重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复画布四个基础交互缺陷（box 乱跳/连线不可用/Office 无操作面/Office 无缩放），把位置管理重构为 Dify 式"创建时物化"模型，并建立 Playwright 真浏览器验收门。

**Architecture:** spec = `docs/superpowers/specs/2026-06-12-canvas-p0-interaction-rework.md`（LOCKED，含两份研究报告结论）。核心：layout 成为位置唯一事实来源（schema v2，子节点父相对坐标）；`deriveFlow` 拆为 `deriveGraph`/`placeNew`/`mergeNodes` 三个纯函数；RF 节点数组只走 merge-by-id；生产代码移除 jsdom 几何桩；连线改条形 handle + v12 真实 CSS 类；Toolbar/Hud 双模式共享；e2e 用 mock server + Playwright。

**Tech Stack:** React 19 + @xyflow/react 12.11 + Tailwind 4 + vitest（既有）；新增 @playwright/test + chromium（仅 devDep）。

**约定（沿用既有 pipeline）：** TDD；`web/src` 任何变更必须同 commit 重建 `internal/serve/dist`（`cd web && npm run build`）；每波一个 PR，CI 绿即合并（已授权）；引擎错误文案逐字保留。

**Wave 划分：** W1 = T1→T2→T3（顺序，同文件区）；W2 = T4；W3 = T5→T6。

---

## 共享锚点（所有任务以此为准）

```ts
// web/src/lib/canvas.ts —— v2 增改
export type LayoutJSON = {
  v?: number;                                          // 2 = 当前 schema
  positions?: Record<string, { x: number; y: number }>; // 顶层=绝对；子节点=父相对（RF 原生坐标系）
  office?: Record<string, { x: number; y: number }>;
};
export const LAYOUT_V = 2;
// 旧 layout（无 v 字段）→ 丢弃 positions/office 全部重新物化（spec §1.3）
export function normalizeLayout(raw: unknown): LayoutJSON;

// 无 position 的图节点 —— deriveFlow 的替代者只产出身份+data+边
export type GraphNode = {
  id: string;
  type: "task" | "seat" | "feature" | "draft";
  parentId?: string;
  data: Record<string, unknown>;
};
export function deriveGraph(
  state: State, drafts: Draft[], draftFeatures?: DraftFeature[],
): { nodes: GraphNode[]; edges: FlowEdge[] };

// 只为 layout.positions 中【没有条目】的节点计算一次初始位置；返回值=新增条目。
// 幂等：对已有条目的 id 绝不返回。子节点返回父相对坐标。
// 网格规则沿用现行常量（COL_W/ROW_H/SEAT_*/PAD/HEADER/TASK_REL_Y0）；
// 碰撞避让对象 = 已有 layout 条目 + 本批已分配的新条目。
export function placeNew(
  layout: LayoutJSON, graph: { nodes: GraphNode[] },
): Record<string, { x: number; y: number }>;

// merge-by-id：prev 中已存在的节点保留 prev 的 position/measured/selected/
// dragging/width/height（RF 回写字段），仅替换 data/className/style 等派生字段；
// 新节点位置取自 layout（缺条目时兜底 {x:0,y:0}，正常流程 placeNew 先行）；
// 消失的节点移除。feature 节点 style 尺寸 = max(featureStyle(默认), 子节点bbox+PAD)。
// 子节点带 expandParent: true；task 带 extent:"parent"（draft 不带，维持拖出派发）。
export function mergeNodes(
  prev: Node[], graph: { nodes: GraphNode[] }, layout: LayoutJSON,
): Node[];
```

**删除项（T2 落地）：** `toRFNodes` 的 `measured`/`handles` 种子与 `handlesFor`、`toParentRelative`、`childToAbsolute`、`deriveFlow` 的位置逻辑（`placeFeature`/`placeChild` 碰撞避让挪进 `placeNew`）。jsdom 中边的 DOM 级断言降级为单测（`deriveGraph` 返回的 edges）或升级为 e2e（T5），**不得**为让 jsdom 渲染边而往生产节点塞几何。

---

### Task 1: lib 纯函数层（normalizeLayout / deriveGraph / placeNew / mergeNodes）

**Files:**
- Modify: `web/src/lib/canvas.ts`
- Test: `web/src/lib/canvas.test.ts`（扩展既有）

- [ ] **Step 1: 写失败测试**（关键用例，全部先写）

```ts
// normalizeLayout
it("v2 layout 原样通过", ...);
it("无 v 字段的旧 layout → 丢弃 positions/office，返回 {v:2}", ...);
it("非对象/null → {v:2}", ...);

// deriveGraph
it("产出 seat/feature/task/draft 节点，无 position 字段", ...);
it("dep 边与 v1 deriveFlow 一致（id/source/target/kind）", ...);
it("draft 的 dep 源可指向 task 或 draft（前缀正确）", ...);

// placeNew
it("空 layout：feature 列、task 行、seat 轨与 v1 网格一致；子节点为父相对坐标", ...);
it("已有条目的 id 不出现在返回值中（幂等）", ...);
it("新 feature 列避让已保存 feature 位置（碰撞规则同 v1）", ...);
it("新 task 行避让同 feature 下已保存兄弟（含本批新分配）", ...);
it("两次连续调用（第二次把第一次结果合入 layout）返回空对象", ...);

// mergeNodes
it("已存在节点保留 prev 的 position/measured/selected/dragging，data 更新", ...);
it("新节点位置取 layout；子节点 position=layout 原值（父相对）+ parentId + expandParent", ...);
it("task 带 extent:'parent'，draft 不带", ...);
it("消失节点移除；feature 先于子节点输出（RF 顺序要求）", ...);
it("feature style 尺寸 ≥ featureStyle 默认，且包住被拖远的子节点 bbox", ...);
it("生产节点不含 measured/handles 字段（防回归）", ...);
```

- [ ] **Step 2:** `cd /Users/xtation/AgentWorks/Code_Claude/pactify/web && npx vitest run src/lib/canvas.test.ts` → 新用例全 FAIL
- [ ] **Step 3:** 实现四个函数（保留 `deriveFlow` 暂不删——T2 切换调用方后删除；网格常量复用文件内既有定义）
- [ ] **Step 4:** vitest 该文件全绿
- [ ] **Step 5:** Commit `feat(canvas): layout v2 + deriveGraph/placeNew/mergeNodes (position materialization core)`

### Task 2: Canvas.tsx 切换到物化管线

**Files:**
- Modify: `web/src/components/Canvas.tsx`、`web/src/lib/api.ts`（getLayout 过 normalizeLayout）
- Test: `web/src/components/Canvas.test.tsx`（适配）

- [ ] **Step 1: 失败测试**

```ts
it("加载旧 schema layout 时全部节点重新物化且界面可渲染", ...);
it("拖动后仅被拖节点的 layout 条目变化（PUT body 断言）", ...);
it("snapshot 更新（SSE 模拟）后未拖动节点的 position 不变", ...);
it("新 draft 出现 → 自动获得位置且写入 layout（author 时 PUT）", ...);
it("observer（author=false）：物化只进本地 state，不发 PUT", ...);
it("传给 ReactFlow 的节点不含手写 measured/handles", ...);
```

- [ ] **Step 2:** 跑测试确认 FAIL
- [ ] **Step 3:** 实现：
  - 加载：`getLayout(project).then(l => setLayout(normalizeLayout(l)))`。
  - 单一图效应：`graph = useMemo(deriveGraph)`；effect 内 `const add = placeNew(layoutRef.current, graph)`；若非空 → `next = {…layout, v:2, positions:{…,…add}}`，`setLayout(next)`，author && !replaying 时走既有 debounced PUT；随后 `setNodes(prev => 注入回调(mergeNodes(prev, graph, next)))`（onDispatch/onFocus 注入维持现状语义）。
  - `onNodeDragStop`：用 **v12 第 3 参（被拖节点集合）** 逐个写 `positions[n.id] = n.position`（坐标即 RF 原生值，无换算）；draft-over-seat 派发手势保留（nodeBounds 的父偏移逻辑不变）。
  - 删除：rebuild 整图 effect、`toRFNodes` 几何种子、`handlesFor`、`toParentRelative`/`childToAbsolute` 调用。
  - 受影响 jsdom 测试：边 DOM 断言改走 lib 单测（已在 T1），其余按行为适配，**不得**回塞几何桩。
- [ ] **Step 4:** `npx vitest run` 全绿
- [ ] **Step 5:** `npm run build`（dist 同 commit）→ Commit `refactor(canvas): merge-by-id node pipeline, positions materialized once`

### Task 3: 连线交互重做（条形 handle + v12 类名 + 连接线）

**Files:**
- Modify: `web/src/components/nodes/TaskNode.tsx`、`web/src/index.css:247-273`、`web/src/components/Canvas.tsx`（connectionLineComponent + connecting 类包装）
- Create: `web/src/components/canvas/ConnectionLine.tsx`
- Test: `web/src/components/nodes/TaskNode.test.tsx`、`web/src/components/canvas/ConnectionLine.test.tsx`

- [ ] **Step 1: 失败测试**：TaskNode 渲染上/下条形 handle（class `task-port task-port-in|out`，含 `.port-mark` 子元素）；ConnectionLine 渲染 bezier path + 终点标记；连接中 stage wrapper 获得 `connecting` 类（useConnection mock）。
- [ ] **Step 2:** FAIL 确认
- [ ] **Step 3:** 实现：

```tsx
// TaskNode：替换两个 12px 圆点
<Handle type="target" position={Position.Top} className="task-port task-port-in"
        isConnectableStart={false}><span className="port-mark" /></Handle>
…卡片…
<Handle type="source" position={Position.Bottom} className="task-port task-port-out">
  <span className="port-mark" /></Handle>
```

```css
/* index.css —— 替换 247-273 死规则区 */
.react-flow__handle.task-port {            /* 贴边整宽 16px 透明热区 */
  width: 100%; height: 16px; left: 0; transform: none; border: none;
  border-radius: 0; background: transparent; min-width: 0; min-height: 0; }
.task-port-in  { top: -8px; }
.task-port-out { bottom: -8px; }
.task-port .port-mark {                    /* Dify 式中央小标记 */
  position: absolute; left: 50%; top: 50%; transform: translate(-50%,-50%);
  width: 14px; height: 4px; border-radius: 2px;
  background: var(--color-role-design); opacity: .45;
  transition: opacity .12s var(--motion-ease), transform .12s var(--motion-ease); }
.task-port:hover .port-mark { opacity: 1; transform: translate(-50%,-50%) scale(1.3); }
.connecting .task-port-in .port-mark { opacity: .9; }            /* 拉线中亮出所有入口 */
.react-flow__handle.task-port.connectingfrom .port-mark,
.react-flow__handle.task-port.valid .port-mark { opacity: 1; background: var(--color-role-dev); }
```

```tsx
// ConnectionLine.tsx：getBezierPath({sourceX,…,curvature:0.16})，stroke=var(--color-role-design)，
// 终点 14×4 圆角矩形标记；ConnectionLineComponentProps 来自 @xyflow/react。
// Canvas.tsx：<ReactFlow connectionLineComponent={ConnectionLine} …>，
// RF 子组件 ConnectingFlag：useConnection().inProgress → stage wrapper toggle "connecting" 类。
```

- [ ] **Step 4:** vitest 全绿
- [ ] **Step 5:** `npm run build` → Commit `feat(canvas): strip handles + v12 connection classes + custom connection line` → **开 PR（W1）**

### Task 4: Office 操作面 + 双模式共享 chrome

**Files:**
- Modify: `web/src/components/Canvas.tsx`（Toolbar 移出 plan 条件块；office 右键状态）、`web/src/components/canvas/OfficeView.tsx`（Hud/右键/dock 空态/desk 物化）、`web/src/components/canvas/Hud.tsx`（minimap nodeColor 支持 desk）、`web/src/components/canvas/ContextMenu.tsx`（office pane/desk 目标）、`web/src/index.css`
- Test: `web/src/components/canvas/OfficeView.test.tsx`、`Toolbar.test.tsx`

- [ ] **Step 1: 失败测试**：office 模式下可见 New Feature/New Task 按钮且点击打开既有表单/编辑器；office 模式渲染 `canvas-hud` 与 minimap；HUD ± 改变 viewport zoom（useReactFlow mock 断言）；零 draft 时 dock 显示"＋ 新建任务"并触发 TaskEditor；office pane 右键出现菜单（新建 Task/Feature）；desk 右键在 drafts≥1 时列出 draft 派发项；新 seat 加入不改变已有 desk 的 office 条目（物化幂等）；comms pill 仅 plan 模式渲染。
- [ ] **Step 2:** FAIL 确认
- [ ] **Step 3:** 实现：Toolbar 移到 mode 条件块外（`comms` 相关 props 仅 plan 传入）；OfficeView 的 ReactFlow 子节点加 `<Hud />`（minimapNodeColor 加 `desk` 分支：按 `desk.status` → busy=success/review_due|waiting=warn/idle=text-3）；office desk 物化走 layout.office（首现 gridPos 一次写入，复用 placeNew 的避让思路，author&&!replaying 才 PUT）；dock `author && !replaying` 常显 + 空态按钮 props 上穿 `onOpenNewTask`；ContextMenu 增加 office 目标类型（pane: 新建 Task/Feature/fit view；desk: 每个 draft 一项"派发 <id> →"）。
- [ ] **Step 4:** vitest 全绿
- [ ] **Step 5:** `npm run build` → Commit `feat(office): authoring toolbar + zoom hud + context menu + dock empty state` → **开 PR（W2）**

### Task 5: Playwright e2e 验收门

**Files:**
- Create: `web/playwright.config.ts`、`web/e2e/mock-server.mjs`、`web/e2e/fixtures.mjs`、`web/e2e/canvas.spec.ts`、`web/e2e/office.spec.ts`
- Modify: `web/package.json`（devDep `@playwright/test`；scripts `e2e`/`e2e:server`）、`.github/workflows/`（既有 web job 所在 workflow 增加 e2e job）、`web/.gitignore`（playwright-report/test-results）

- [ ] **Step 1:** mock-server.mjs：node http，服务 `../internal/serve/dist` 静态文件 + fixtures：`GET /api/projects`、`GET /api/projects/p1/state`、`GET/PUT /api/projects/p1/layout`（PUT 回显存内存）、`GET /api/projects/p1/events`（SSE 长连）、**测试钩子 `POST /__test/snapshot`**（注入新 state 并向 SSE 推送）。fixture：2 seats（claude-opus[orchestrator,reviewer]/opencode[worker]）、1 feature、2 task、1 draft。
- [ ] **Step 2:** playwright.config：`webServer: { command: "npm run build && node e2e/mock-server.mjs", port: 4173 }`，project chromium。
- [ ] **Step 3:** 五条用例（xyflow 官方模式：先 `toHaveCSS('visibility','visible')` 等测量完成；拖拽 = hover→mouse.down→move→up 断言 transform）：
  1. **drag-isolation**（canvas.spec，plan 模式）：记录全部 node transform → 拖 box A → 其余 transform 逐一不变；等 debounce 后 PUT body 仅含 A 的条目变化。
  2. **connect**（canvas.spec）：从 draft 下边缘热区拉到另一 draft 上边缘 → `.react-flow__connectionline` 出现 → 松手 → `.react-flow__edge` 计数 +1；拉向跨 feature 目标 → 计数不变 + notice 文案出现。
  3. **office-zoom**（office.spec）：`mouse.wheel` 与 HUD ± → `.react-flow__viewport` transform scale 变化；fit 按钮复位。
  4. **office-author**（office.spec）：New Feature 表单建 f2 → New Task 建 t9 → dock 出现 t9 → HTML5 拖到 idle desk → DispatchModal 可见且 owner 预填。
  5. **drag-during-sse**（canvas.spec）：mouse.down 拖动中 → `POST /__test/snapshot` 推新快照 → 节点仍跟随鼠标、松手位置=鼠标处（不被传送）。
- [ ] **Step 4:** 本地 `npx playwright install chromium && npm run e2e` 全绿
- [ ] **Step 5:** CI：在既有 web workflow 增加 `e2e` job（checkout → setup-node → `npm ci` → `npx playwright install chromium --with-deps` → `npm run e2e`，上传 playwright-report artifact on failure）。Commit `test(e2e): playwright acceptance gate for canvas interactions` 

### Task 6: 终审 + 文档 + 收尾

- [ ] **Step 1:** 全量验证：`cd web && npx vitest run && npm run e2e`；`go build ./... && go test ./...`；真机 sanity：`pactify serve` + headless chrome 截图 plan/office 两模式确认 chrome 元素齐全。
- [ ] **Step 2:** 文档：`docs/architecture.md` 增"Canvas P0 状态模型（位置物化 + e2e 门）"小节；spec §5 工艺规约摘录进 `web/` 顶部 README 或 CLAUDE.md 适当位置。
- [ ] **Step 3:** `.agent/CURRENT.md` 状态行 + `.agent/sprints/sprint-003.md` 增 T10 条目。
- [ ] **Step 4:** Commit + **开 PR（W3）**，CI 绿合并；重建 `/opt/homebrew/bin/pactify`。

---

## Self-Review 结论

- spec §1（物化/schema v2/merge/SSE 合流）→ T1+T2；§2（条形 handle/类名/连接线）→ T3；§3（Toolbar/Hud/右键/dock/desk 物化/click-dispatch 维持）→ T4；§4（五用例/mock server/CI）→ T5；§5（工艺规约入文档）→ T6。无缺口。
- 类型一致性：`GraphNode`/`placeNew`/`mergeNodes` 签名在 T1 定义、T2/T4 引用一致；`normalizeLayout` 在 api.ts 调用点与 T1 签名一致。
- 已知留白（有意）：ContextMenu office 目标的具体 item 结构、Hud minimap desk 配色细节——实现者按既有组件模式落地，验收以测试用例为准。
