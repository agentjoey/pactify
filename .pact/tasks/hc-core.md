# Task hc-core (E3-1) — cockpit 门/限速/到期 + escalation 账本双写(纯本地,无 relay)

## 背景
hosted cockpit(spec §2 E3/E4、§4 安全)前置:机器侧策略门与资源约束必须先立,与传输无关。

## 四项
### 1. RemotePolicy 加 Cockpit 门
`internal/serve/stint.go` RemotePolicy 加 `Cockpit bool \`json:"cockpit"\``(默认 false,与
Stint/Orchestrate/Plan 正交)。本任务只加字段+读取(供 T2 dispatcher 用),不接任何行为。

### 2. Manager 会话闲置到期
`internal/cockpit/manager.go`:
- Session 包装记录 lastActivity(prompt/permission/resume/新 subscriber 时 touch,加锁)。
- Manager 后台 reaper(启动一个 goroutine,每 1min 扫;NewManagerCtx 的 ctx 取消时退出):
  闲置 > idleTimeout(默认 30min,`NewManagerCtx` 加可选 option 覆盖,测试用短值)→
  关闭该 session(等价 Close 单个:cancel 其 ctx + 从 map 删 + 事件 JSONL 保留 + threads.json
  保留 → 之后 Resume 可续)。pending approvals 随进程消亡的既有契约不变(文档注释声明)。
- 测试:fake clock 不好注入就用短 timeout 真等(≤2s);到期后 map 无该 session、再 Session() 新建、
  threads.json 仍在。

### 3. 限速与队列上限
- per-session prompt 限速:滑窗 10 次/分钟,超出 Prompt 返回错误(不排队);serve prompt 端点
  透传 429。
- pending approval 上限 32:超出时新 permission 请求直接以 deny 回给 backend 并记一条
  session 事件(kind "system",text 说明 queue full)——绝不静默丢。
- 测试:连打 11 次 prompt 第 11 次 429;伪造 33 个 pending 第 33 个自动 deny。

### 4. escalation 账本双写
现状 orchestrate escalation 只落 `status.json`(隐式)。加:
- `internal/event` 加事件类型 `escalate`(task 级,payload: `{reason string, seat string}`),
  照 `start` 事件先例(spec §2.6 模式)。
- orchestrate 升级(escalated=true 的那个写点,搜 internal/orchestrate 里置 escalated 的位置)
  同时 append 一条 `escalate` 到 pact 账本(withLedgerLock;失败只 warn 不阻断 orchestrate)。
- 投影/STATE 不变(escalate 不改变任务状态机,是可观测性事件);validate 不报未知事件。
- docs/specs 相应协议文档追加 §(escalate 事件语义,机器写,幂等无要求)。
- 测试:orchestrate 升级路径 append 了事件;replay/validate 对含 escalate 的账本不报错。

## 门
go test ./... 全绿(重点 internal/cockpit internal/serve internal/orchestrate internal/pact)+
`go vet ./...`。不碰 web。
verify: go test ./internal/cockpit/... ./internal/serve/... ./internal/orchestrate/... ./internal/event/... && go vet ./...
