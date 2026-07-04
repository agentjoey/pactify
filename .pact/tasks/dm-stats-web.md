# dm-stats-web — 座席卡显示可靠度 ✓N ↻M

> 母 spec:`docs/specs/driver-modernization.md` §5。依赖 dm-stats(后端字段已出)。

## 交付
web 座席统计条(Board 卡片统计/RosterDock 座席行,找现有 per-seat stats 消费处)追加 `✓{accepted} ↻{reworked}`,样式与现有 mono 统计条一致(参考 ui/MetricStrip 用法)。stats 类型定义同步加两字段(web/src/lib 的 ProjectStats 类型)。

## 测试
组件测试更新 + 新增断言两个值渲染;`cd web && npx vitest run` 与 `npx tsc -b --noEmit` 全绿。

## 边界
只碰 web/;不改后端;不重设计卡片布局。
