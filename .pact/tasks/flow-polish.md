# Task flow-polish (Flow F3-a) — 泳道可读性 + 缩放跟随

## 四项(实拍确认的问题 + 设计文档 F3 项)
### 1. 时间刻度重叠根修
现状:刻度按**时间等分**取点再映射 x → 压缩空档后多个刻度挤在同一 x(实拍 "43,19:02:47" 叠字)。
修:改为按 **x 空间等分**(如 6 个刻度均布 [0,1]),标签用**反向映射**求时间。
- `flowderive.ts` 加 `export function tAt(model: FlowModel, x: number): number`(x∈[0,1]→毫秒,
  model.x 的逆;gap 区段内返回 gap 起点时间)+ 单测(x(tAt(x))≈x 往返、gap 内合理)。
- FlowLanes 刻度改用 tAt;落在 gap 压缩段内的刻度**跳过**(gap 已有 ⌇ 标记)。
### 2. stint 条内标签 + hover
- 条宽 > 60px 时条内画 `task·kind` 小字(mono 8.5px,超宽截断);任何宽度都给 `<title>`:
  `task · kind · 起止 HH:MM–HH:MM · 时长 Nm`(进行中 "– now")。
- gap ⌇ 标记加 `<title>`:真实空档时长(如 "1h02m idle,已压缩")。
### 3. 缩放 + 右缘跟随
- FlowLanes 头部(SEAT 行右侧)加缩放段选 `×1 ×2 ×4`(state,默认 ×1):画布宽 = base×zoom,
  x 坐标同乘;容器原生横向滚动即平移。
- **右缘跟随**:新事件到达(model.tMax 变大)且当前滚动位置接近右缘(距右 <80px)时,
  自动 scrollLeft 到最右——正在回看历史(不在右缘)则不打扰。
### 4. idle lane 折叠
- 无任何 stint/arrow(只有 join 或全无)的座席折叠为一行摘要 "N idle seats"(点击展开/收起,
  localStorage 不需要);有活动的照常。全部 idle 时不折叠(避免空屏)。

## 测试
- tAt 往返单测;刻度不重叠(渲染出的刻度 x 间距 > 阈值,可直接断言生成的刻度数组);
- stint title 内容;zoom 切换画布宽变化;idle 折叠行出现/展开。
- 全量 vitest 绿。

## 验收(视角: ux — 刻度可读、条可识别、缩放顺滑、折叠不藏活动座席)
verify: cd web && npx tsc --noEmit && npx vitest run
