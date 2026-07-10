# Task flow-depth (Flow F2/F3-b) — 状态深度 + 座席侧卡

## 四项
### 1. blocked 态推导(三渲染器共享)
- `flowderive.ts` 加 `export function blockedTasks(state: State): Map<string, string[]>`:
  遍历 state.tasks,`deps` 中存在未 accepted/shipped 的任务 → 该任务 blocked,值=未完成的 dep ids。
  (State/Task 类型在 `web/src/lib/types.ts`,Task.deps?: string[];状态字段看 Board 现有 blocked 推导,
  搜 `deps` 在 web/src/components/Board.tsx 的用法,保持同一语义。)
- FlowLanes 行头 live chip:座席当前 stint 的 task 若 blocked → 琥珀 chip `blocked·等 <dep>`(优先级高于 working)。
- FlowOffice 工位卡、FlowFeed 顶部座席 chips 同样标 blocked。FlowView 把 state 传给三渲染器(已有 props 就复用)。
### 2. FlowOffice 工位实时计时
- 工位卡在座席有进行中 stint(t1===null)时显示已用时 `elapsed Nm`,每 30s tick 刷新
  (setInterval,unmount 清理;非进行中不显示)。
### 3. 座席侧卡(lane 行头点击)
- FlowLanes 行头点击 → 打开右侧浮层小卡(absolute 定位在容器右上,非模态,点外或 ✕ 关):
  座席名/角色徽标 + 来自 getStats 的 AgentStat:Tasks/Accepted/Reworked/Tokens/±LOC/时长。
  数据:FlowView 已有 project prop,用 `DataSource.getStats(project)`(api.ts:ProjectStats.agents,
  按 Seat 匹配);拉一次缓存于 state,打开时 fetch,失败显示 "stats unavailable" 不崩。
- 再次点同一行头关闭。选中态行头高亮。
### 4. FlowFeed 座席 chips blocked/live 化
- FlowFeed 顶部 chips 目前只有 liveStates;并入 blocked(同 1)——blocked 优先显示。

## 测试
- blockedTasks 单测(dep 未完成/已完成/无 deps/链式);FlowLanes blocked chip;Office elapsed 出现与
  fake timers 步进;侧卡开合 + stats 渲染(mock getStats)+ 失败态;Feed chips blocked。
- 全量 vitest 绿。
verify: cd web && npx tsc --noEmit && npx vitest run
