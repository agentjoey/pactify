# Task flow-renderers (Flow F1-c) — 会话流 + 办公室渲染器(替换占位)

## 目标
FlowView 的另两个模式落地(视觉基准=已拍板的实机原型):
- **FlowFeed(会话流)**:动词消息卡列表 + 顶部座席状态 chips。
- **FlowOffice(办公室)**:工位信息卡群像 + 中央 ⎇ main 基座 + 事件播报。
两者输入与 FlowLanes 同源(events + deriveFlow 的模型/或直接 events)。

## 改文件
- 新增 `web/src/components/flow/FlowFeed.tsx`:props {events: PactEvent[], agents, selected, onSelect}。
  - 顶部 chips 行:每座席一枚(同 FlowLanes 的 live 态推导——可从 deriveFlow(events) 取 stints 推,
    或抽 FlowLanes 里已有的推导为共享 helper 放 flowderive.ts,**推荐后者**:
    `export function liveStates(model: FlowModel): Record<string,{kind:"idle"|"work"|"rework"|"review"; task?:string}>`,
    FlowLanes 同步改用,消重复)。
  - 消息卡(时间升序,自动滚到底):join(就位)/assign(→owner·note 摘 payload.reviewer)/
    checkpoint(→reviewer,蓝框)/changes(红框,payload.reason 若有)/accept(绿框)/merge(金框 ◆)。
    卡片:avatar+mono 文本+右侧 ts(HH:MM)。点击卡片(有 task 的)→ onSelect(task);selected 卡描边高亮。
  - 空态 "No activity yet"。
- 新增 `web/src/components/flow/FlowOffice.tsx`:props {events, agents, onSelect}。
  - 工位卡 grid(自适应 2-3 列,不做绝对定位——产品里响应式优先):每卡 avatar/名/角色 +
    live 态行(liveStates:working·task / reviewing·task / idle)+ 事件计数 + merge 计数不在卡上;
  - 中央(grid 顶部横条)⎇ main 条:merges 计数(数 merge 事件)+ 最近一条 merge 的 task/时间。
  - 底部播报:最近 3 条事件一行文本(mono,新→旧渐隐)。
  - 工位卡点击 → onSelect(该座席当前 task,若有)。
  - **不做包裹飞行动画**(原型的演示糖;产品 v1 求信息,动效 F3 再议)。
- `web/src/components/flow/FlowView.tsx`:两个占位替换为真组件,props 透传。
- flowderive.ts:加 `liveStates`(如上)+ 单测。

## 测试
- FlowFeed.test:喂 assign/checkpoint/changes/accept/merge 事件 → 对应卡出现(testid flow-msg-<verb>),
  changes 卡含红框语义(class/testid 断言),点击带 task 的卡调 onSelect;chips 反映 working。
- FlowOffice.test:工位卡渲染(名/角色/live 态/事件计数),main 条 merges 计数正确,播报显示最近事件。
- liveStates 单测(进行中 work → {kind:"work",task};全关 → idle)。
- FlowView.test 更新(占位断言改真组件存在)。

## 验收(视角: ux — 与原型语义一致、点击穿透、零 Lanes 回归)
verify: cd web && npx tsc --noEmit && npx vitest run
