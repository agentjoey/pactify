# Task flow-view (Flow F1-b) — Board|Flow 切换 + FlowView 壳 + 泳道渲染器

## 目标
把 Flow 视图接进 dashboard:App 加 **Board | Flow** 主视图切换;FlowView 容器带三渲染模式
tab(泳道|会话流|办公室,本任务只实现**泳道**,另两个 tab 先渲染占位"即将上线");
泳道渲染器 FlowLanes 消费 deriveFlow(T1 已并入 main:web/src/lib/flowderive.ts)。
视觉基准:实机原型(用户已拍板)——深色 token、左座席栏+右 SVG 时间画布。

## 改文件
- `web/src/App.tsx`:主视图 state `boardMode: "board"|"flow"`(localStorage "pactify:boardMode" 记忆);
  在 Board 区域位置按 mode 渲染 <Board> 或 <FlowView>;切换 pills 放 Board 上下文头条左侧
  (Board.tsx 的 "All tasks" chip 左边)——**更简单的落法**:pills 放在 App 里 Board/Flow 容器上方一行,
  紧凑(参照 .tabs/.pill 样式用现有 token 类);键盘不做。
- 新增 `web/src/components/flow/FlowView.tsx`:props {state, events, project, selected, onSelect}。
  内部 mode state("lanes"|"feed"|"office",localStorage "pactify:flowMode");顶部小 tab;
  lanes → <FlowLanes>,feed/office → 占位卡("会话流/办公室渲染器 — 下一任务")。
- 新增 `web/src/components/flow/FlowLanes.tsx`:props {model: FlowModel, agents: State["agents"], selected, onSelect}。
  渲染(照原型):
  - 左座席栏:avatar(角色色:orchestrator #ffd479 系/reviewer 蓝/worker 绿——role 从 state.agents 的
    roles 推;用现有 token 变量)+ 名 + 角色 + **状态 chip**(从 model 推导 live 态:该 agent 有
    t1===null 的 stint → working/review/rework 相应色;否则 idle;blocked 不做——留 F2)。
  - 右画布 SVG(横向 overflow-auto):时间刻度(model.tMin/tMax 取 5-6 个刻度,gap 区段标 ⌇)、
    lane 分隔线、stint 圆角条(work 绿/rework 琥珀/review 蓝斜纹 pattern,t1===null 画到最右+渐隐)、
    箭头(assign 灰/checkpoint 蓝/changes 红弧/accept 绿,marker 箭头)、merge ◆ 金菱形、join 小圆点。
  - x 坐标:model.x(t)*画布宽(画布宽 = max(容器宽, 900))。
  - **点击穿透**:stint 条/箭头 click → onSelect(task)(与 Board 卡片同一 selected 语义,
    RightRail/TaskDetail 自动接管);selected 的 stint 描边高亮。
  - 空 model(无事件):居中空态文案 "No activity yet"。
- 测试:`web/src/components/flow/FlowView.test.tsx` + `FlowLanes.test.tsx`:
  - FlowView:mode tab 切换/localStorage 记忆/占位渲染。
  - FlowLanes:喂一组事件(用 deriveFlow 或手造 model)→ 渲染出 lane 名、stint rect(data-testid="flow-stint")、
    changes 箭头存在(data-testid="flow-arrow-changes")、点击 stint 调 onSelect(task);空态。
  - App:boardMode 切换渲染 FlowView(若 App 测试重,可最小断言 pills 存在+点击切换 data-testid="view-flow")。

## 验收(视角: ux — 原型观感还原、点击穿透接 RightRail、零 Board 回归)
- reviewer 会起 dev serve 实拍对照原型。
verify: cd web && npx tsc --noEmit && npx vitest run
