# rp-schedule — 定时/周期 orchestrate(serve 侧,零新依赖)

> 母 spec:`docs/specs/review-runtime-deepening.md` §7。只支持 `daily@HH:MM`(本地时区)与 `every:Nh|Nm` 两种表达式,手写解析,**不引 cron 库**。

## 交付
1. 新包 `internal/schedule`:表达式解析 + `NextFire(now, expr) time.Time` 纯函数;`~/.pactify/schedules.json` 读写(id, project, feature, expr, enabled)。
2. `pactify schedule add <project> --at <expr> [--feature f]` / `list` / `remove <id>`;非法表达式 add 即拒。
3. serve:分钟粒度 ticker goroutine,到点对匹配 project 调 spawnOrchestrate(注入式,可测);orchestrate 在跑则跳过本次并记日志(冲突守卫已有)。

## 测试
解析表驱动(合法/非法);NextFire 含跨午夜/every 边界;fake spawn 断言触发与在跑跳过。`go test ./internal/schedule/ ./internal/serve/ -run 'Schedule|Sched'` 绿。

## 边界
web UI 不做;账本不记调度(机器运维,非协议事实)。
