# UI 参考调研（2026-06-14）

调研来源：用户提供的截图（`~/.../UI_ref/`：dify 4 张 + make grid 4 张 + React Flow 3 个 MHTML）+ 实地看官网（reactflow proximity-connect / editable-edge / edge-routing、dify agent node 文档、make.com/grid）。本文把每个参考**提炼成对 Pactify 的可落地映射**。

## 1. Dify —— 布局 / 卡片 / 配置详情（页面布局、卡片、UI 细节）

**看到的：**
- **节点卡上直接显示配置摘要**：Agent 节点卡内联 `STRATEGY: FunctionCalling` / `MODEL: chatgpt-4o-latest [CHAT]` / `TOOLBOX: [G][YT]` 图标——不打开面板就能一眼读懂这个节点是什么。
- **运行态可视**：节点右上角状态 pill（`Running` / `Wait for running`），运行中的节点有蓝色发光边。边上有中点 `+` 插入按钮。
- **右侧配置面板**：UPPERCASE 分组 label（AGENTIC STRATEGY / MODEL* / TOOLS LIST* / INSTRUCTION / QUERY / MAX ITERATIONS / OUTPUT VARIABLES），必填带红 `*`，每个控件是一张软灰圆角卡，留白充足，顶部有 SETTINGS / LAST RUN tab。
- **TOOLS LIST 模式**：`1/2 Enabled  +`，每个工具一行 = provider 图标 + PROVIDER 名 + tool_name + 状态（蓝色 toggle / `Not authorized` 琥珀 pill）。
- 文本域有字符数、`{x}` 变量插入、展开、复制等 affordance。

**→ Pactify 映射：**
- **任务/座席卡显示一眼摘要**：座席卡内联 kind/model/roles + 状态 pill；任务卡显示 owner→reviewer + pact 状态 pill（assigned/awaiting_review/accepted…）。
- **运行态 pill + 发光**：把现有的一次性 pulse 升级成「持续状态 pill + 运行中发光边」，对齐 orchestrate 的实时态。
- **RightRail 重做**：用「UPPERCASE 分组 + 软圆角卡 + 留白」重排（spec / evidence / timeline / actions 分组）。
- **Agent Config 重做（近期高价值）**：用 Dify 的 **TOOLS LIST toggle 模式**替换我现在的逗号输入框做 allowed-tools——每工具一行 toggle + 状态，`X/Y enabled`。分组：MODEL / 权限姿态 / TOOLS。

## 2. React Flow —— 动效 / 边交互（动效参考 template）

**Proximity Connect（核心）：** 拖一个节点靠近另一个（< MIN_DISTANCE 150px），**实时**出现一条 `temp` 临时虚线边预览将要建立的连接；松手（onNodeDragStop）才落定，且自动按水平位置定 source/target。"snap zone + 非破坏性预览 + 实时反馈" 是它手感好的关键。
**Editable Edge：** 边上有可拖的**控制点**重塑路径（bezier）；连线时按 `Space` 画自由曲线。（Pro）
**Edge Routing：** 用 `libavoid` 做**智能避障路由**——边自动绕开节点走正交/step 路径，密集图里大幅降低"边穿过节点"的视觉杂乱。（Pro）

**→ Pactify 映射：**
- **proximity-connect 做依赖编排**：在画布/plan 里拖一个 task 靠近另一个 → 出现临时虚线 + snap → 松手建立 dep 边。把"建任务图"从填表降到拖拽，手感对标 dify/reactflow。
- **edge-routing 治理密集依赖图**：任务多时用避障路由让 dep 边绕开节点，比现在的 bezier 交叉清爽得多。
- **保留 + 强化蚂蚁爬线**：它是我们独有的"通信在流动"signature；和上面两个组合——结构清晰（路由）+ 活着（蚂蚁）+ 好编排（proximity）。

## 3. Make Grid —— Office 模式 / agent 通信交互管理（office、通信展示）

**看到的：**
- **等距 3D 空间图**：每个 scenario/agent 是一块 3D tile，**活跃=着色+凸起**，**空闲=压平+变灰**，深度即状态。
- **选中节点详情面板**：header（图标+名+`Team/Folder`面包屑）+ 产品大图标 + **Links 关系区** + Properties。
- **Links 关系区（金矿）**：每行 = [源图标] —`Read →`/`← Write` 方向 pill— [目标图标 + 名]；同一目标可同时显示读/写两条；`View all (5) →`。
- **Layers / 镜头**：同一空间图切换不同数据视图（Explore / Operations / Data-transfer），每个带 mini 预览缩略图；"Add optional data elements"。
- **Needs your attention · 3 objects**：左上红色卡，surfac e 关键/阻塞对象。
- chrome：搜索 `Find anything ⌘F`、过滤 chips（Active/Stopped scenarios ×）、底部 `2D` 切换 + 暗色 + 缩放%、`Select Layer`、camera。

**→ Pactify 映射（这是用户最关心的 office + agent 通信）：**
- **Office 升级为"agent landscape"**：座席=desk tile，working=凸起着色（按 role 色）、idle=压平灰；深度直观表达"谁在干活"。（我们已有 Office desks，这是质的提升方向。）
- **Comms / Links 面板**：选中一个座席/任务，右侧用 **方向 pill** 展示它的通信/关系——`reviews →`（它评审谁）/`← reviewed by`、`owns`/`depends on`、`hands off →`；`View all`。这就是"agent 之间通信交互管理展示"。
- **Lenses 镜头**（我 backlog 早记过，现在有了范本）：office/canvas 上切换叠加层——**活跃 / 依赖 / 成本(token) / 通信流量 / needs-attention**。
- **Needs attention 栏**：surface escalated/blocked task + stale 座席。
- chrome 对齐：Find-anything 搜索（接 ⌘K）、状态过滤 chips、底部 2D/3D + 缩放。

## 跨参考的系统性提炼（要进品牌规范的）
1. **状态语言统一**：到处用「状态 pill + 颜色 + （运行中）发光」一套，替代现在零散的 badge/glow。
2. **分组配置面板**：UPPERCASE 分组 + 软圆角卡 + 留白 + 必填 `*`（dify 范式），统一 RightRail / AgentConfig / 各 modal。
3. **关系即一等公民**：Links 方向 pill（make grid）是展示 pact 关系（owner/reviewer/dep/comms）的最佳载体。
4. **深度/镜头表达状态**：用凸起/着色/镜头叠加表达"活跃/阻塞/成本"，而非塞满文字。
5. **空间编排手感**：proximity-connect + edge-routing 让"建图/读图"都顺滑。

## 待与用户讨论的决策点
- **D1 暗色 vs 浅色**：dify/make grid 都偏**浅色通透**；我们现在是**暗色**。保留暗色身份（克制专业）还是转/出浅色变体？（影响整套 token）
- **D2 Office 走多"3D"**：make grid 是**完整等距 3D**；我们 Office 是伪 3D desks。做到等距 3D（惊艳但工程重、且画布工艺规约几何严格）还是"2.5D 增强"（凸起/阴影/着色但不全等距）？
- **D3 范围/优先级**：①Agent Config 用 dify TOOLS 模式重做（近期、独立、高 ROI）②Comms/Links 面板（office 核心）③proximity-connect 编排 ④edge-routing ⑤Lenses ⑥状态 pill 系统统一。先打哪几个？
- **D4 蚂蚁爬线**：保留为 signature 并和 edge-routing/proximity 组合，对吗？

## 补充：Stitch（画布风格）+ 画布背景特效（2026-06-14）
**Stitch（Google，stitch.withgoogle.com）**：AI 原生**无限画布**（Figma 感）——生成的屏幕做成**精致卡片**摆在画布上、连成 **flow**，可 Play 预览交互。风格关键词：现代精致、一致排版、**渐变**、图标、完整交互态（含 delete 态）、**微动效**、风格跨生成保持一致。
- 来源：https://www.nxcode.io/resources/news/google-stitch-complete-guide-vibe-design-2026 ，https://alternativeto.net/news/2026/3/you-can-now-use-google-stitch-to-vibe-design-uis-on-an-infinite-canvas-using-your-voice （用户给的具体项目 URL 需登录，未取到其私有内容）
- **→ Pactify 画布映射**：无限画布 + 精致卡片（dify 节点卡）+ 连成 flow（pact 依赖/交接）+ 微动效（蚂蚁）+ 现代渐变背景。强化"卡片优先 + 干净画布 + 渐变深度"的方向。

**画布背景特效（已落地基线，Phase 2 继续打磨）**：
- `.canvas-stage` 改为**三主色 ambient 极光**（金右上 / 蓝左上 / 绿底部，低透明 color-mix）叠在 **白→page 竖向渐变**上 —— 呼应三品牌色身份 + 给画布深度（Stitch 式）。
- 细化 masked **点网格**（22px、低透明深点、中心可见边缘渐隐）。
- 待 Phase 2/4 把节点/desk 卡转浅后，这套背景才完整显现；可加**极慢极光漂移**（reduced-motion 关）增"活"感。

## 三主色（提辨识度，2026-06-14）
orchestrator/reviewer/worker 调为 **vivid 黄/蓝/绿**（`#eca400`/`#2563eb`/`#12a150`），辨识度显著提升；小字用 `-ink` 深色伴生（如 `--color-role-product-ink`）保证可读。
