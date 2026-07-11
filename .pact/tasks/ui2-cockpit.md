# Task ui2-cockpit (UI v2 包 3/5) — Cockpit 全页视图重做(会话流 + 右栏)

## 设计源(必读)
README.md §3 Cockpit + §Interactions(approvals 三条铁律)+ designs/"Pactify Cockpit.dc.html"
整文件。字体 Inter,gold 角色 accent。

## 交付(evolve CockpitPanel → 全页 Cockpit 视图,数据面全部现成:SSE 流/审批/座席下拉/resume)
### 1. 布局
主列(会话流,弹性)+ 右栏 340px,1px 分隔。**Session header**(#0c1119 薄条):座席头像+名+
角色 chip(orchestrator gold)· `live · <model>` mono 行(emerald 脉冲;model 字段现有则显,
无则只显 live)· 右对齐 MetricStrip(TOK/COST/ITER,来自现有 usage 事件累计)。
### 2. 会话流卡片化(现有事件类型映射,顺序滚动,CLI 密度)
- turn divider(居中 `turn started · HH:MM` 细线);user 消息右对齐蓝 tint 气泡(radius
  12 12 4 12);agent 消息 = 头像+流文本(内联 code mono chips;`t-xxx` 任务引用做蓝色
  link-chip → 点击跳 Board 并选中该 task);
- tool-call 卡:深色头($ + 命令截断)+ **exit pill**(exit 0 emerald/非零红,现有 exit 数据
  有则显)+ 折叠(▾ N lines)+ --bg-code pre 尾部;
- diff 卡(事件里有 diff/patch 内容时):头(⑂ path + +N/−N)+ 行号 gutter pre(增绿/删红 tint);
  无此类事件则组件就绪不强造;
- plan-todo 卡(有 plan/todo 事件时同理);
- **inline approval 卡**:normal=中性卡单 Allow/Deny;strong/network/destructive=红边+glow
  危险卡 + 两步确认(现有 GradeRisk/两步确认逻辑迁移进新卡);**RawInput 永远原文展示,
  Title 标注 advisory**(现有铁律,不得回退);
- streaming 指示:头像 + eq 三条 + 文案。
### 3. Sticky 输入行(底部 #0c1119)
圆角输入(占位 "Message the orchestrator…")+ 禁用的 ＋ 附件位 + ■ Interrupt(danger 描边,
现有 cancel)+ gold Send ↵;turn 进行中 Send 改排队提示(gold 点 hint 行——现有 prompt 串行
即"排队",文案照稿)。
### 4. 右栏(独立滚动)
① Approval queue(`N pending` 红 pill;卡片 Allow/Deny,与流内卡同源同状态);② Session info
卡(头像/角色/live 点/model/usage 累计/thread id + copy——resume threadId 现有);③ Run context
卡(当前 feature 的 mini task-pipeline ✓/◉/⚡/▸merge,从 state 推导;点击跳 Board);
④ Sessions 列表(当前会话 gold 高亮 + 其它 cockpit-capable 座席可切换=现有座席下拉升级为列表;
底部虚线 "Attach a worker session" 占位禁用)。
### 5. 测试与视觉
现有 CockpitPanel 测试迁移 + 新卡测试(approval 两步/queue 同源/task link-chip 跳转);
全量绿;playwright 实拍对比设计稿(流卡/审批卡/右栏三处必对)。
verify: cd web && npx tsc --noEmit && npx vitest run
