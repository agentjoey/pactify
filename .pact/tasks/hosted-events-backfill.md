# Task hosted-events-backfill — hosted 模式 Flow/事件流空白根修
用户报障:staging web(master secret 登录)下 pactify 项目泳道/会话流全空、办公室无有效信息、
footer log.jsonl · 0。根因:本地 SSE 连接时回放日志尾部,而 RelaySource.subscribe 只有 socket
live 增量、无历史回填;App 仅在 worktree 分支调 fetchEventsLog → hosted 事件数组恒从空开始。
修:App 订阅分支统一补一次 REST 事件回填(src.fetchEventsLog,EVENTS_CAP 上限),与 live 流共用
event_id 去重,合并后按 ts 排序防乱序;本地模式回填与 SSE 回放重叠由去重吸收。
实测:hosted(staging relay)下 opencode-remote-control 111 条、pactify 385 条,泳道完整渲染。
verify: cd web && npx tsc --noEmit && npx vitest run
