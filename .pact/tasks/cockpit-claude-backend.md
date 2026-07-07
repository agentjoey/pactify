# Task cockpit-claude-backend (A3-go) — claudeSdkBridge 后端 + 共享 JSON-RPC 传输

## 目标
1. 把 codex.go 里的 JSON-RPC/stdio 传输**抽出**为包内共享 `rpcConn`(新文件 jsonrpc.go),
   codex.go 改用它;
2. 实现 `internal/cockpit` 的 **claude** 后端 `claude.go`(claudeSdkBridge):spawn
   `node <绝对路径>/vendor/claude-host/host.mjs`(**永不 npx**),用共享 rpcConn 跟已写好的
   host.mjs 通信(协议见下,orchestrator 已写好 host.mjs 并真机验证)。

## 背景:host.mjs 已就位且真机验证
`vendor/claude-host/host.mjs`(Node,Claude Agent SDK 桥)已由 orchestrator 写好并端到端验证:
initialize→thread/start(立即回)→turn/start→cockpit/session+事件流+approval 往返全通。
**你不改 host.mjs**,只写 Go 侧对接它。

## 改文件
- 新增 `internal/cockpit/jsonrpc.go`(抽出的共享传输)
- 改 `internal/cockpit/codex.go`(改用共享 rpcConn;不改外部行为,codex 测试与真机 smoke 必须仍绿)
- 新增 `internal/cockpit/claude.go`(claudeSdkBridge Backend/Session)
- 新增 `internal/cockpit/claude_test.go`(fake node 进程 + gated 真机 smoke)

## §1 抽共享传输 jsonrpc.go
把 codex.go 现有的 codexClient(writeCh + pending map + writeLoop + readLoop + 三分支 dispatch +
Spawn(Setpgid/killGroup) + filteredEnviron + call/notify)**抽成通用 `rpcConn`**,**不含任何 codex/claude
专有逻辑**。通用 dispatch 三分支后调用可插拔钩子:
- 响应(有 id 无 method)→ 喂 pending(内部处理)。
- server 请求(有 id 有 method)→ 调 `onRequest(id json.RawMessage, method string, params json.RawMessage)`。
- 通知(有 method 无 id)→ 调 `onNotification(method string, params json.RawMessage)`。
rpcConn 暴露:`call(ctx, method, params)`、`notify(method, params)`、`reply(id json.RawMessage, result any)`、
`replyError(id, ...)`、`Close()`。`nextID` 从 1 起(**关键**:id 0 会被 omitempty 丢,对端当通知永不回,initialize 挂死——codex 已踩过)。
codex.go 改为:new 一个 rpcConn,传入 codex 的 onRequest(codex 审批 method)+ onNotification(codex 通知→Event)。
**codex 的现有测试和真机 smoke 必须仍全绿**(抽取是重构,零行为变化)。

## §2 claude.go — 用 rpcConn 对接 host.mjs
Backend `claudeBackend`:
- Start:定位 host.mjs 绝对路径。约定:相对 pactify 仓库根的 `vendor/claude-host/host.mjs`;
  定位法——先试 exe 同级/上溯找 `vendor/claude-host/host.mjs`,再试环境 `PACT_CLAUDE_HOST`(测试用),
  找不到则返回清晰 error(提示跑 `pactify doctor --setup-bridge`)。spawn `node <abs host.mjs>`
  (cwd=repoDir,Setpgid,filteredEnviron **但不剥 claude auth**——见 §3),用 rpcConn。
  发 initialize → thread/start(**立即回 {}**,threadId 此时未知)。返回 claudeSession。
- Resume(threadID):spawn + initialize + thread/resume{threadId} → {thread:{id}}。
claudeSession 实现 Session:
- Prompt:turn/start {threadId(可空), input:[{type:"text",text:msg.Text}]}。
- Interrupt:turn/interrupt。
- ThreadID:返回从 `cockpit/session` 通知里收到的 id(**初始 "",第一轮后被 host 的 cockpit/session 通知填上**)。
- Close:幂等,killGroup + close channels。
- onRequest:host 只发一种 server 请求 `approval/request` {toolName,input,title,requestId,danger}
  → 造 cockpit.ApprovalRequest{Kind:"tool",ToolName:toolName,RawInput:该 params 原文,
  Respond: 收 Decision→reply(id,{decision: "allow"/"deny"})}。Allow→"allow",其余→"deny"。投进 Approvals()。
- onNotification:host→Go 通知映射:
  | method | 动作 |
  |---|---|
  | `cockpit/session` {threadId} | 记到 session.threadID(供 ThreadID() 返回) |
  | `cockpit/message` {text,final} | Events() 发 Event{Kind:EventMessage,Text,Final} |
  | `cockpit/tool` {phase,name,text} | Event{Kind:EventTool,Tool:&ToolEvent{Phase,Name,Text}} |
  | `cockpit/usage` {inputTokens,outputTokens,totalTokens,costUSD} | Event{Kind:EventUsage,Usage:&Usage{...}} |
  | `cockpit/state` {state} | Event{Kind:EventState,State} |
  | `cockpit/error` {error} | Event{Kind:EventError,Err} |
  每个 Event.Raw 存原始 params。未知 method 忽略。

## §3 auth env(修 E0-③)
claude 桥 **不剥 ANTHROPIC_API_KEY / CLAUDE_CODE_OAUTH_TOKEN 等**——按官方优先级链原样透传
(SDK 自己走 ~/.claude OAuth 或 env)。仍剔 PACT_RELAY_TOKEN + PACTIFY_*(denylist,同 codex)。
即:与 codex 同一个 filteredEnviron denylist 即可(它本就只剔那两类,不碰 claude auth)。确认复用同一 denylist。

## §4 测试(claude_test.go)
- **fake node 桥**:测试里用一个**最小 fake host**(可以是内嵌的一段 mjs 写到 t.TempDir 再 `node` 跑,
  或更稳:用 io.Pipe + 手写 Go fake 直接对 rpcConn 说协议——推荐后者,不依赖 node)。
  脚本化:回 initialize/thread/start;推 cockpit/session + cockpit/message + 一个 approval/request。
  断言:Start 成功;Prompt 发出 turn/start;cockpit/session 后 ThreadID() 返回该 id;
  cockpit/message→Events() 得 EventMessage;approval/request→Approvals() 得 ApprovalRequest,
  Respond(DecisionAllow) 写回 {decision:"allow"};Close 幂等。
- **gated 真机 smoke**(`COCKPIT_SMOKE=1` 且 node+桥 deps 就绪才跑,否则 t.Skip):
  spawn 真 host.mjs,initialize→thread/start→turn/start(一句轻 prompt 如 "reply hi")→
  等 cockpit/session 断言 threadId 非空、收到至少一个 cockpit/message 或 cockpit/state。
  (真机 smoke 由 reviewer 亲跑;PACT_CLAUDE_HOST 指向 vendor/claude-host/host.mjs。)

## 验收 / Acceptance(视角: correctness — 传输抽取零回归、claude 映射贴协议、ThreadID 异步填充正确)
- reviewer 独立跑 verify(含 -race)+ codex 既有真机 smoke 仍绿 + claude 真机 smoke 亲跑。

## verify
verify: go build ./... && go test -race ./internal/cockpit/...
