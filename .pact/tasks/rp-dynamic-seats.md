# rp-dynamic-seats — 动态座席(小步)

> 母 spec:`docs/specs/review-runtime-deepening.md` §6。范围收紧:不做解散/完整 auto-staff。

## 交付
1. `pactify join --kind <kind>`:join 事件 payload 带 kind;投影 Agents[].Kind 写入路径补全(字段已存在)。
2. loop.go:seat→kind 映射改为每轮迭代重读 state(--seat-kind flag 仍最高优先;新 join 的 seat 下一轮即可驱动)。
3. planner prompt(internal/planner/prompt.go)补一条:可提议新座席 `{id, kind, roles}`;plan apply 对不在 roster 的 seat 自动生成带 kind 的 join 事件。

## 测试
join --kind 落账本+投影;fake runner 下 mid-run 新 seat 下轮被驱动;apply 自动 join。`go test ./internal/pact/ ./internal/orchestrate/ ./internal/planner/ -run 'Join|Seat|Plan'` 绿。
