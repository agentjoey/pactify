# Canvas P0 — 交互地基重构设计（位置物化 / 连线 / Office 操作面 / e2e 验收门）

日期：2026-06-12 · 状态：LOCKED（用户已批准 P0 范围："不光是手感，整体商业级产品标准也借鉴 dify 等工具"）
前置：Dashboard v2（PR #16-#19）。本 spec 修复其验收中暴露的四个基础交互缺陷，并把画布的状态模型与质量门槛提升到商业产品标准。

## 0. 问题与根因（已实锤）

| # | 用户报告 | 根因 |
|---|---------|------|
| 1 | 移动一个 task box，其他未 dispatch 的 box 随机移动 | `deriveFlow` 每次渲染重算未保存节点的位置，`placeChild`/`placeFeature` 的碰撞避让会被已保存位置级联挤压；SSE 快照整图重建还会把拖拽中的节点传送回去 |
| 2 | box 之间拉线完全不可交互 | handle `opacity:0` 仅 hover 显示且只有 12px；`toRFNodes` 每次重建塞入假 `measured:{200,80}`/`handles`（jsdom 测试桩混入生产），v12 源码证实 user node 上的 `handles` 会**完全覆盖** DOM 测得的锚点；CSS `.react-flow.connecting`/`.task-handle.connecting` 是死规则（v12 真实类名为 `connectingfrom`/`connectingto`/`valid`） |
| 3 | Office 页面无任何可操作内容 | Toolbar 只在 plan 模式渲染；dock 仅有 draft 时出现；右键菜单只接在 plan 的 ReactFlow；而 Office 是**默认落地模式** |
| 4 | Office 不能放大缩小 | 缩放 HUD/minimap 只挂在 plan 的 ReactFlow；Office 无任何可见缩放控件 |

研究输入（两份调研报告结论，已逐条对源码/官方文档核实）：
- **Dify**（reactflow 11 + zustand）：position 是**创建时一次性计算的持久化数据**，存进 draft JSON，从不在渲染时重derive；拖拽中不保存、dragStop 才 debounced 全量保存；handle = 16px 透明热区 + 伪元素小标记（未连接隐藏、hover 放大、点击弹下游选择器）；快捷键集中声明表；右键菜单走 store 按 target 类型渲染；无 e2e（vitest + 整模块 mock）。
- **React Flow v12**：`measured`/`handles` 是 SSR 输入 + RF 回写字段，应用**永远不要手写**；服务端数据合并必须 `setNodes(prev => merge)` 保留 prev 的 `position/measured/selected/dragging`；连接态真实类名 `connectingfrom`/`connectingto`/`valid`，画布级"连接中"状态用 `useConnection().inProgress` 自己加；官方 Easy Connect 模式支持大条形/全节点 handle；节点上滚轮默认仍缩放画布（可滚动内容加 `nowheel`）；`onNodeDragStop` 第 3 参 = 本次被拖动的节点集合（多选整组）；父节点无官方"自动包住子节点"，`expandParent` 仅拖到边缘时单向扩张；xyflow 官方仓库的 Playwright e2e 模式可直接照抄（hover→mouse.down→move→up，断言 transform / connectionline / edge 计数，`toHaveCSS('visibility','visible')` 等待测量完成）。

## 1. 状态模型重构 — 位置物化（修 #1，架构级）

### 1.1 原则

**layout 是所有节点位置的唯一事实来源。** 位置在节点首次出现时计算一次并立刻写入 layout；此后只有用户拖拽能改它。派生层只负责"哪些节点/边存在 + 节点 data"，不再负责位置。

### 1.2 拆分 `deriveFlow` → 三个纯函数（lib/canvas.ts）

```
deriveGraph(state, drafts, draftFeatures) → { nodes: GraphNode[], edges: FlowEdge[] }
  // 只产出节点身份 + data + parentId + 边。GraphNode 无 position。

placeNew(layout, graph) → Record<id, {x,y}>
  // 对 layout.positions 中没有条目的节点，按现有网格规则（feature 列 / 子行 / seat 轨）
  // 计算一次初始位置；碰撞避让只对"已存在的 layout 条目 + 本批新节点"做。
  // 幂等：已有条目的节点绝不重算。返回值是"新增条目"，由调用方合入 layout。

mergeNodes(prev: Node[], graph, layout) → Node[]
  // 按 id 合并：已存在的节点保留 prev 对象的 position/measured/selected/dragging/
  // width/height（RF 回写字段），只替换 data/className 等派生字段；新增节点从
  // layout 取位置；消失的节点移除。绝不整图重建。
```

三个函数全部 TDD。`toRFNodes` 中的 `measured`/`handles` 种子**从生产路径删除**（v12 自己测量；现有依赖种子的 jsdom 测试改用测试侧 helper 注入）。

### 1.3 layout schema v2

```jsonc
{ "v": 2,
  "positions": { "feature:f1": {"x":320,"y":0},     // 顶层节点：绝对坐标
                 "task:t1":    {"x":16,"y":44} },    // 子节点：父相对坐标（关键变更）
  "office":    { "claude-opus": {"x":60,"y":40} } }
```

- **子节点存父相对坐标**（v1 存绝对坐标导致拖 feature 后子节点行为分裂：已保存的留在原地、未保存的跟着走）。父相对后，拖 feature 只写 feature 一个条目，子节点天然跟随。
- 读到无 `v` 字段的旧 layout：**丢弃 positions/office，全部重新物化**（当前只有开发期 scratch 项目，迁移不值得写）。serve 端 PUT 仍存 opaque JSON，零改动。
- 物化时机：每次 snapshot/drafts 变化后，对新出现的节点跑 `placeNew`，合入本地 layout state；**author && !replaying 时**走既有 debounced PUT；observer/replay 只物化到本地（算法确定性保证各端默认布局一致）。
- 拖拽保存：dragStop 只写被拖节点集合（v12 第 3 参）对应的条目；子节点写父相对坐标（`childToAbsolute` 反向废除）。
- feature 容器尺寸：由子节点 bbox 计算（min = 现行 featureStyle），子节点 `expandParent: true` 兜底拖边扩张。

### 1.4 SSE/replay 合流

snapshot 到达 → `deriveGraph` → `placeNew`（新节点）→ `mergeNodes`。拖拽中的节点因 merge 保留 prev position/dragging 而**不被打断**。replay 同路径（只是不 PUT）。

## 2. 连线交互重做（修 #2，对标 Easy Connect / Dify）

- **条形 handle**：task/draft 节点的 target（上边）与 source（下边）改为**贴边整宽、16px 高的透明热区**，中央伪元素小标记（Dify 式）：默认半透明可见（author 模式），hover 热区时标记放大 + 着色。废除"opacity:0 直到 hover"——可发现性优先。
- **连接中状态**：包一层读 `useConnection().inProgress` 的组件，往 stage wrapper 加 `.connecting` 类 → CSS 显示所有节点的 target 标记；现有 `connect-target` 高亮环保留。
- **CSS 类名修正**：死规则 `.react-flow.connecting`/`.task-handle.connecting` 删除，改用 v12 真实类名 `connectingfrom`/`connectingto`/`valid`。
- **自定义 connectionLineComponent**：bezier（曲率 ~0.16）+ 角色色，终点画目标标记（Dify 式）。
- 保留：`connectionRadius={30}`、`isValidConnection`（isValidDep）、无效落点 not-allowed 光标、toast 解释。
- 节点拖拽与连线不冲突：热区只占上下边 16px，卡片主体仍是拖拽区，无需 dragHandle。

## 3. Office 操作面 + 双模式共享 chrome（修 #3 #4）

- **Toolbar 提升为双模式共享**：新建 Feature / 新建 Task 在 Office 与 Plan 都渲染（comms lens pill 仍 plan 专属）。TaskEditor/DispatchModal 本就挂在 Canvas 层，零搬动。
- **Hud（缩放±/百分比/fit/minimap）挂进 Office 的 ReactFlow**；minimap 桌卡按 desk status 着色。
- **Office 右键菜单**：pane 菜单（新建 Task / 新建 Feature / fit view）；desk 菜单（派发到此坐席——有 draft 时）。复用 ContextMenu 外壳。
- **dock 空态**：author 模式下 dock 常显；零 draft 时显示"＋ 新建任务"入口（打开 TaskEditor）。
- **desk 位置物化**：office key 走与 §1 相同的机制（首现写入，gridPos 不再每次按 index 重算——坐席加入不再挤动已有桌子）。
- click-dispatch 维持"恰好 1 个 draft"规则；>1 时点击 idle 桌让 dock 短暂高亮（引导去拖）。
- desk 内容不可滚动（现状），无需 `nowheel`；若未来加滚动区必须加。

## 4. Playwright e2e 验收门（流程修复）

- **形态**：Playwright + chromium，`web/e2e/`；被测物 = `vite build` 产物由**轻量 mock server**（`web/e2e/mock-server.mjs`，~100 行 node http）服务：静态 dist + `/api/*` fixture（projects/state/layout PUT 回显/SSE 可控推送）。不依赖 Go 后端，hermetic、CI 快。
- **五条回归用例**（与四个用户报告一一对应 + 创建闭环）：
  1. plan：拖动 box A 松手 → 断言其他所有 box transform 不变（含 PUT layout 后）。
  2. plan：从 task 下边缘 handle 拉线到另一 draft → connectionline 出现 → 松手 → edge 计数 +1（无效目标 → 不产生 edge + notice 出现）。
  3. office：滚轮缩放 + HUD ± 按钮 → viewport zoom 变化；fit 复位。
  4. office：新建 Feature → 新建 Task → dock 出现 → 拖到 desk → DispatchModal 打开。
  5. plan：SSE 推送新 snapshot 时正在拖拽的节点不被传送（merge 保位置）。
- 模式照抄 xyflow 官方：`node.hover()→mouse.down()→mouse.move()→mouse.up()`、先 `toHaveCSS('visibility','visible')` 等待测量。
- **CI**：web.yml 增加 e2e job（`npx playwright install chromium --with-deps`）；画布相关 PR 的合并门 = vitest + e2e 双绿。jsdom 测试不再作为交互正确性依据。

## 5. 工艺规约（防复发）

- 生产代码不得包含 jsdom/测试专用桩（`measured`/`handles` 种子事件为戒）；测试几何注入只能住在 `setupTests.ts` / 测试 helper。
- RF 节点数组的任何更新必须走 merge-by-id，禁止 `setNodes(整图重建)`。
- 位置只有两个写入者：`placeNew`（首现一次）与用户拖拽。任何"渲染时计算位置"的 PR 直接打回。

## 6. 范围外（P1 记录）

视口持久化（Dify 存 viewport 进 draft）、undo/redo（zundo）、自动整理（elkjs）、指针/手型双模式、快捷键声明表化、office parcel 跨桌拖拽语义、连接线磁吸强化、性能（display 层每帧克隆全图）。
