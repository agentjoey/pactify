# Task board-shell-fixes (P0#2/#7/#8 + TOK 降噪) — 壳层/看板布局修复

## 四项
### 1. Cockpit 按钮不再遮挡 New task(根因已定位)
App.tsx:404 的 cockpit-toggle 是 `absolute right-3 top-3` 悬浮在 board 行上——正压住 Board
上下文头条右侧的 "＋ New task"。修:**移进顶部 header 工具栏**(和 ⌘K/live/dispatch/settings
一排,Toolbar 里加一个 "Cockpit" 文本按钮或图标按钮,保留 data-testid="cockpit-toggle" 与
canOrchestrate 门控),删除 absolute 悬浮版。若 Toolbar 是独立组件则给它加 prop(onToggleCockpit
+ showCockpit),App 传入。
### 2. 上下文头条溢出碰撞
Board.tsx:216-274 一行里 feature chips(overflow-x-auto)+ 座席簇 + 计数 + New task 相互挤压
(chips 多时 `sp1-` 被 avatar 盖住)。修:chips 容器加 `min-w-0 flex-1`,右侧簇容器加
`shrink-0`,保证 chips 在自己的滚动区内滚、右侧永不被盖。
### 3. RunRail 全 shipped 状态语义
RunRail:355 `done→"Delivered"`,但 totals 只统计活跃 feature——全部 shipped 时显示
"Delivered · 0 features · 0/0 accepted" + 常亮 Ship,自相矛盾。修:当 `total === 0 && done`:
label 显示 "All shipped",隐藏 "0 features/0/0 accepted" 计数与进度条,隐藏/禁用 Ship 按钮
(title "nothing to ship")。活跃时行为不变。
### 4. TOK 缺数据降噪
TaskCard 的 stat strip 在无 token 数据时显示 "TOK –" ——73 张卡全是死横线。修:MetricStrip/
TaskCard 逻辑:token 值缺失/0 时**不渲染 TOK 段**(RUN/×iter 照旧)。有值时照旧。

## 改文件
web/src/App.tsx · web/src/components/shell/Toolbar.tsx(如存在)· web/src/components/Board.tsx ·
web/src/components/board/RunRail.tsx · web/src/components/TaskCard.tsx(或 ui/MetricStrip.tsx)· 相应 test

## 测试
- cockpit-toggle 仍按 canOrchestrate 门控渲染/点击 toggle(现有测试若有则改挂载点后保持绿)。
- RunRail:total=0&&done → 文案 "All shipped"、无 Ship 按钮;有活跃 feature 回归不变。
- TaskCard:tok 缺失不渲染 TOK 段;tok 有值渲染。
- 全部现有 vitest 保持绿(挂载点变动可能影响 App 相关测试,同步更新)。

## 验收 / Acceptance(视角: ux — 无遮挡、无溢出碰撞、全 shipped 语义自洽、TOK 不再死横线)
verify: cd web && npx tsc --noEmit && npx vitest run
