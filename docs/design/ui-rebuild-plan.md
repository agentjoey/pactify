# Pactify UI 重构计划（2026-06-14，Phase 0–5）

承接：D1 浅色、D4 蚂蚁动效语言（signature）、参考 dify（卡片/配置/TOOLS）+ React Flow（动效）+ Make Grid（office/通信）。
原则：**自底向上**——先锁基准与基础元素，再一页一页做。每阶段闭环 = 我截图自审 → 你审 → ship。

---

## Phase 0 · 基准 + 基础元素库（foundation，一次性）
目标：产出全局共用的「设计系统」，后续所有页面从这里拼。
- **0.1 浅色 token 终版**：page/card/inset/border/text/role/semantic/shadow/radius/type/spacing/motion —— **你拍板锁定**。
- **0.2 元素画廊页**：`?gallery` 路由（访问 `…/?gallery` 渲染画廊，不影响主应用）= 活的设计规范，代码即规范。
- **0.3 设计 + 锁定基础元素**（每个：浅色 + 全状态 + 截图审）：
  - **Foundations**：色板 / 字阶 / 间距 / 阴影 / 圆角 / 动效 swatch。
  - **Card**：基础卡 · **节点卡**（任务/座席：图标+标题+状态 pill+配置摘要，对标 dify agent 卡）· **配置分组卡** · 列表行。
  - **Button**：primary/ghost/danger · sm/md · loading · icon · disabled。
  - **StatusPill（pact 状态语言，新原语）**：assigned/in_progress/awaiting_review/changes_requested/accepted/shipped/escalated/idle —— 各配色 + 可选「运行中发光」。这是贯穿卡片/节点/Live 的状态语言。
  - **Badge**：role 徽章 · drivable/manual · 计数。
  - **Input**：文本 / 文本域（字数·展开·变量）/ select / **toggle** / **分段控件** / role·tool chip —— focus/error/disabled。
  - **Section**：dify 式分组（UPPERCASE label + 必填* + 软卡体）。
  - **反馈**：Alert（4 色）/ EmptyState / Spinner / Toast / Skeleton。
  - **Ant caste**：载物/信使剪影 + 状态色（静态；动效引擎在 Phase 3）。
- **产出**：锁定的元素库 + 画廊（活规范）。**Phase 0 不碰具体业务页面。**

## Phase 1 · 结构页逐个（简单页，全量转浅 + 套元素 + 收尾）
每页：浅色化 + 用锁定元素重拼 + 页面级收尾（empty/loading/error）+ 截图审 + ship。
1.1 Setup · 1.2 Recipes · 1.3 Ops + **AgentConfig（dify TOOLS 模式重做）** · 1.4 Plan 复审 · 1.5 Kanban · 1.6 **Live（首次用状态 pill 卡片）**

## Phase 2 · 画布 Plan mode（图层，难）
2.1 任务/座席节点卡 → 用元素库 node-card 浅色化 · 2.2 **edge-routing（libavoid）** 依赖边避障 · 2.3 **proximity-connect** 拖拽建依赖 · 2.4 feature-group 框架浅色化。

## Phase 3 · 蚂蚁动效引擎（signature，贯穿）
3.1 AntEdge 升级支持**词表**（速度区分 / 原地转圈 / 汇聚 / spawn 钻出 / 报错红焦躁 / 颜色）· 3.2 接到**实时态**（下发/干活/等审/审中/打回/通过/spawn/报错/合并/空闲）· 3.3 蚂蚁出现在 **画布边 + Live 卡片 + Office** · reduced-motion 全程降级。

## Phase 4 · Office（Make Grid 式，signature 空间视图）
4.1 desk/座席卡浅色 + **活跃凸起着色 / 空闲压平变灰** 深度 · 4.2 **Comms/Links 面板（方向 pill：reviews→/←reviewed by/owns/depends on/hands off）** · 4.3 **Lenses（活跃/依赖/成本token/通信 叠加层）** · 4.4 office 内完整蚂蚁语言 · 4.5 Needs-attention 栏。

## Phase 5 · 收尾 + 一致性
5.1 跨视图 chrome（replay 条 / ⌘K / TopBar 终版）· 5.2 一致性审计（间距/色/组件跨全页）· 5.3 reduced-motion + 可达性（focus ring/对比/aria）· 5.4 红警告降噪 + 语言一致性 · 5.5 动效性能 + bundle。

---

## 节奏与门禁
- 每阶段/每页：`shots.mjs` 截图自审 → 你审 → 满意才 ship。
- 门禁不变：vitest + Playwright e2e 双绿（office-zoom flake 已修）+ tsc。
- 分支：当前 `feat-light-theme` 升级为 Phase 0 工作分支；Phase 0 锁定后再开 Phase 1 分支逐页。
- 协作：标准独立子任务可派 opencode（如某页转换、某端点），复杂/设计判断 claude。

## 当前状态
- `feat-light-theme`：token 翻了一半 + 结构页转浅（Phase 0.1 起步）。画布/office 仍深色（属 Phase 2/4）。
- **下一步：Phase 0 —— 建元素画廊 + 锁 Foundations/Card/Button/StatusPill 第一批。**
