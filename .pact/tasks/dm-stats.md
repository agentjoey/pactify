# dm-stats — per-seat 可靠度统计(Go 侧)

> 母 spec:`docs/specs/driver-modernization.md` §5。无依赖。

## 交付
1. `internal/stats` `AgentStat` 加 `Accepted int` + `Reworked int`:fold 事件流——task owner 的任务出现 `accept` 事件 → owner.Accepted++;出现 `changes` 事件 → owner.Reworked++(每次都计)。
2. serve /stats DTO 透传两字段(找 stats DTO 序列化处,大概率零改动自动带出,验证)。

## 测试
fold 用例:直接 accept(1/0)、两次 changes 后 accept(1/2)、未 accept(0/N)。`go test ./internal/stats/ ./internal/serve/` 绿。

## 边界
web 显示是 dm-stats-web(另一任务),别碰 web/。
