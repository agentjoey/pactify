# Task ui2-dashboard (UI v2 包 4/5) — 新 Dashboard 总览视图

## 设计源(必读)
README.md §1 Dashboard + designs/"Pactify Dashboard.dc.html" 整文件。字体统一 Inter
(设计稿的 linx 语言不引入 Space Grotesk,accent 语义保留:emerald=系统/成功,gold=项目)。

## 交付(新组件 web/src/components/Dashboard.tsx,挂 lens=dashboard;全部数据现成推导,零新后端)
### 1. Context bar
项目选择器(gold 点)+ mono 副题(`<branch?> · N features · M seats`,branch 无则省)+
New task(现有)。
### 2. KPI strip(4 卡)
Active run(orchestrate status 现有轮询:跑=1+蓝脉冲"orchestrating",否则 0/idle)·
Awaiting review(state 推导,gold,"human decision")· Tokens today(stats/usage 现有;
无按 0)· Shipped·7d(events 推导 merge/shipped 计数,emerald)。uppercase mono 微标签 +
26px/700 mono 数字 + 小限定词。
### 3. 左列
- Run control 卡:跑时 "Orchestrating"+蓝脉冲、**Stop**(danger,现有 orchestrate stop 端点);
  **Pause** 仅在现有后端支持时做,否则不渲染(注记);mono run 统计行 + 6px 进度条
  (按 feature 任务完成比推导);idle 时显 idle 态 + Run 按钮(现有 runOrchestrate)。
- Feature lane 卡(每个进行中 feature):header(feature id mono + tok/time + n/m)+
  横向 pipeline 任务 chips(done ✓ emerald/review ◉ gold/working ⚡ blue+eq/queued ·/
  ▸ merge 虚线)——与 Cockpit run-context 共用一个 mini-pipeline 组件(lifecycle 同源);
  awaiting review 的 lane 内嵌 **review-gate 卡**:Approve merge(emerald=accept)/
  Reject → rework(ops=changes)/See diff(跳 Board 选中)/Take over(跳 Cockpit)。
### 4. 右列
- Seats roster:头像 tile + 名 + 角色 chips + mono 副行(shipped/tok 或当前任务)+ 状态
  (active/working 脉冲/idle)——数据同 Flow 的 liveStates+stats。
- Activity feed:events 推导时间线(◉ assigned/⚡ started/✓ accepted/↺ changes/⇧ shipped
  彩色方块 + 文本 + mono `feature · Nm ago`),头部 `N new` emerald pill(自上次查看,
  localStorage 时间戳)。
### 5. 测试与视觉
推导函数单测(KPI/lane/feed)+ 组件测试;全量绿;playwright 实拍对比设计稿。
verify: cd web && npx tsc --noEmit && npx vitest run
