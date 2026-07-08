# Task cockpit-acp-backend — ACP 档 cockpit 后端(kimi/gemini 作 cockpit.Backend)

## 目标(deep-integration-backends spec §5 / cockpit 多厂商)
让 kimi/gemini 也能作 cockpit 后端:实现 `cockpit.Backend`/`Session`(A1 接口)覆盖 ACP 档,
底层复用 `internal/acp` 的 Client。这样 backendForKey 能给 kimi-cli/gemini-cli 选到 cockpit 后端。
纯新增。

## 改文件
- 新增 `internal/cockpit/acp.go`(acpBackend + acpSession)
- 新增 `internal/cockpit/acp_test.go`(pipe-backed fake ACP server 单测 + gated 真机 smoke)
- 改 `internal/serve/cockpit.go` 的 `backendForKey`:kind `kimi-cli` / `gemini-cli` → 新的 ACP 后端

## 背景:先读
- `internal/cockpit/backend.go`(Backend/Session/Event/ApprovalRequest/Decision)
- `internal/acp/acp.go`:Spawn/Initialize/NewSession/LoadSession/Prompt/Close、OnSessionUpdateFor/
  OnPermissionRequestFor/ClearSession、SessionUpdate{SessionID,Kind,Usage,Raw}、
  PermissionRequest{SessionID,ToolCall{ToolCallID,Title},Options[]PermissionOption{OptionID,Kind,Name},Raw}、
  PermissionOutcome{OptionID,Cancelled}、Usage{InputTokens,OutputTokens,Cost}
- `internal/orchestrate/acprunner.go` 的 acpCommand(kind)→(command,args)(kimi:"kimi","acp";gemini:"gemini","--acp")
  —— 它是 unexported,你在 acp.go 里自建一个等价小映射(别 import orchestrate)。

## §1 acpBackend(Backend)
- 构造:`newACPBackend(kind string) *acpBackend`(kind = "kimi-cli"/"gemini-cli")。
- Start(ctx, opts):
  1. cmd/args = acpCommandFor(kind)(kimi→"kimi",["acp"];gemini→"gemini",["--acp"];未知→error)。
  2. `client, err := acp.Spawn(ctx, cmd, args, nil, opts.RepoDir)`(acp.Spawn 内部已 filteredEnviron)。
  3. `client.Initialize(ctx)`;`sid, err := client.NewSession(ctx, opts.RepoDir)`。
  4. 造 acpSession{client, sid, events, approvals, ...},注册 per-session 回调(见 §2/§3),返回它。
- Resume(ctx, threadID):Spawn+Initialize+`client.LoadSession(ctx, SessionID(threadID))`,sid=threadID。

## §2 事件映射(client.OnSessionUpdateFor(sid, fn))
fn(u acp.SessionUpdate) → 投 cockpit.Event 到 events channel(非阻塞,照 CockpitSession 消费):
- u.Usage != nil → Event{Kind:EventUsage, Usage:&cockpit.Usage{InputTokens:u.Usage.InputTokens,
  OutputTokens, TotalTokens:In+Out, CostUSD:u.Usage.Cost}, Raw:u.Raw}。
- u.Kind == "agent_message_chunk" → 从 u.Raw 解析出文本增量(ACP 形如
  `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"..."}}`;解析 content.text)
  → Event{Kind:EventMessage, Text:<text>, Raw:u.Raw}。
- u.Kind == "tool_call" 或 "tool_call_update" → Event{Kind:EventTool, Tool:&ToolEvent{Phase:(new→"start"/update→"output"),
  Name:从 Raw 取 title/kind, Text:""}, Raw:u.Raw}(尽力解析,取不到留空,别 panic)。
- u.Kind == "plan" → Event{Kind:EventPlan, Raw:u.Raw}。
- 其它 Kind → 忽略(或 Event{Kind:EventState, State:u.Kind, Raw:u.Raw};择一,别伪造 message)。
events channel 缓冲(如 64),满则丢弃不阻塞(pump 由 CockpitSession 消费)。

## §3 审批桥接(sync→async,关键)
ACP 的 `OnPermissionRequestFor(sid, fn)` 的 fn **同步返回** PermissionOutcome;而 cockpit 的
ApprovalRequest.Respond 是**异步**(UI 稍后应答)。桥:
- fn(req acp.PermissionRequest) PermissionOutcome {
    resCh := make(chan cockpit.Decision, 1)
    ar := cockpit.ApprovalRequest{
      Kind: "permission", ToolName: req.ToolCall.Title, RawInput: req.Raw,
      Respond: func(d Decision) error { /* sync.Once 幂等 */ resCh <- d; return nil },
    }
    投 ar 到 approvals channel(阻塞或带兜底:审批必须送达,别丢——可用带缓冲 channel + 若满则起 goroutine,
      或直接阻塞式 send。简单起见 approvals 缓冲足够 + 送不进则默认 deny)。
    select {
      case d := <-resCh: return mapDecision(d, req.Options)
      case <-ctx/session done: return PermissionOutcome{Cancelled:true}
    }
  }
- mapDecision(d, options):
  - DecisionAllow / DecisionAllowForSession → 从 req.Options 选一个"允许"项的 OptionID
    (Options[i].Kind 常含 "allow"——挑第一个 Kind 前缀 allow 的;AllowForSession 优先挑含 "always"/"session" 的,
     没有就用普通 allow)。返回 PermissionOutcome{OptionID:该id}。
  - DecisionDeny → 优先挑 Kind 含 "reject"/"deny" 的 OptionID;没有则 PermissionOutcome{Cancelled:true}。
- **审批送达不能死锁**:OnPermissionRequest 的 fn 在 acp reader 的独立 goroutine 里跑(acp 已如此),
  阻塞它不卡 reader;但 approvals channel 的消费方(CockpitSession)必须在跑。设计上 approvals 缓冲 16。

## §4 acpSession(Session)
- Prompt(ctx, msg): `_, err := client.Prompt(ctx, sid, msg.Text)`(acp.Prompt 是一轮阻塞返回 StopReason;
  串行即可)。
- Interrupt(ctx): acp 无独立 cancel API(现状),先实现为 no-op 返回 nil(注释标注:session/cancel 待 acp Client 支持);
  或若 acp 有 cancel 就用。**别假装**。
- Events()/Approvals(): 返回 channel。ThreadID(): string(sid)。
- Close(): client.ClearSession(sid) + client.Close();幂等(sync.Once);close channels。

## §5 backendForKey 接线(internal/serve/cockpit.go)
- kind `kimi-cli` → `newACPBackend("kimi-cli")`;`gemini-cli` → `newACPBackend("gemini-cli")`。
  claude-code/codex-cli 分支不变。其它仍 error。

## 测试(acp_test.go)
- pipe-backed fake ACP:用 acp 包的测试构造法(若 acp 暴露 newClient(w,r,closeFn,cwd) 就用;否则用
  io.Pipe 起一个假 ACP server 脚本化回 initialize/session/new + 推 session/update(agent_message_chunk)
  + 一个 session/request_permission)。断言:Start 后 ThreadID=sid;agent_message_chunk→Events() 得 EventMessage;
  usage→EventUsage;permission→Approvals() 得 ApprovalRequest,Respond(DecisionAllow) 让 acp 收到对应 OptionID 的回复;
  Close 幂等。
  (若直接驱动 acp 假 server 复杂,可退一步:把「SessionUpdate→Event」「Decision+Options→PermissionOutcome」
   两个纯映射函数抽出来单测,再加一个用真 acp.newClient over pipe 的集成 smoke。至少覆盖映射 + 审批桥。)
- gated 真机 smoke(COCKPIT_SMOKE=1 且 kimi 在 PATH):真起 `kimi acp` initialize→NewSession→Prompt 轻量,
  断言收到至少一个 EventMessage 或 usage。无 kimi 则 t.Skip。

## 验收 / Acceptance(视角: correctness — 映射贴 ACP、审批 sync→async 桥不死锁、Close 幂等、-race)
- reviewer 独立跑 verify(含 -race);真机 smoke reviewer 亲跑(kimi 在 PATH)。

## verify
verify: go build ./... && go test -race ./internal/cockpit/ ./internal/serve/
