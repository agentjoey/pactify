# Task ui2-board (UI v2 包 2/5) — Board 演进:context header + 通知条 + 卡片基因组

## 设计源(必读)
~/AgentWorks/Code_Claude/design_handoff_pactify_ui/README.md §2 Board +
designs/"Pactify Board.dc.html"(整文件,布局/文案/状态全按它)。字体统一 Inter。

## 交付
### 1. Context header(padding 11px 18px,bg #0c1119)
项目选择器(● 项目名 ▾,现有 ProjectMenu 迁入)· 分隔线 · **消息通知条**:bell 图标 +
`N new` pill + 横向滚动事件 chips(从 events 推导最近值得关注项:awaiting review(gold ◉)/
started(⚡)/changes requested(↺),带 `· Nm` 相对时间)· 右侧:**座席头像簇**(重叠 monogram
tiles,idle 者 60% 透明度)· 分隔线 · Group: status ▾(占位,仅 status 分组即现状)· New task
(现有按钮迁入)。
### 2. 列头刷新(5 列现有:assigned/working/review/accepted/shipped)
status dot(working/live 动画 breath)+ uppercase 600/11px 标题 + count pill;review 列加
gold `N due` pill(due = awaiting_review 数)。
### 3. TaskCard 基因组(evolve 现有 TaskCard/Board 卡)
- **medallion** 글리프 tile(◇ assigned/⚡ working/◉ review/✓ accepted·shipped,列色 tint)
  + task id(mono 小字)+ 标题(600/12.5px)+ 右侧 chip(feature tag 或状态 pill);
- medallion 缩进下:**MetricStrip**(RUN/TOK/×iter,未跑卡用斜体 est)+ **owner 行**
  (agent chip;review 卡显示 `owner → reviewer` 交接;右侧 4 段 lifecycle bar);
- 状态样式:working 蓝 focus glow + 动画点;review gold tint + 内联 ✓ Accept(emerald)/
  ↺ Changes(ops 橙)(现有逻辑,对齐样式);draft 虚线边框 + "drag onto a seat" 提示
  (现有 draft 概念如无则仅样式支持,不新造拖拽);shipped 加 `→ local main` provenance 行 +
  全绿 lifecycle bar;
- 终态列底部 **fold expander**:`▸ N more accepted/shipped`(emerald tint 全宽钮,现有折叠逻辑对齐样式)。
### 4. 测试与视觉
组件测试更新 + 新增(通知条推导/medallion 状态/fold);全量 vitest+tsc+build 绿;
playwright 实拍与设计稿对比(列头/卡片/头条三处必对)。
verify: cd web && npx tsc --noEmit && npx vitest run
