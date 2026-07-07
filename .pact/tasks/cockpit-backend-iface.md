# Task cockpit-backend-iface (A1) — CockpitBackend 统一信封接口 + fake double

## 目标
新建 `internal/cockpit` 包,定义深度集成后端层的**统一信封接口**(spec
`deep-integration-backends-spec-2026-07-08.md` §2)。三种后端(codex app-server /
claude SDK 桥 / ACP)都实现它,cockpit manager 消费它。本任务只做**接口 + 类型 + 一个
内存 fake 实现 + 测试**,不接任何真 agent。纯新增,零现有代码改动。

## 改文件(仅新增)
- `internal/cockpit/backend.go`(接口 + 信封类型)
- `internal/cockpit/fake.go`(内存 FakeBackend/FakeSession,供后续 manager 与本任务测试用)
- `internal/cockpit/backend_test.go`

## 契约(§2 逐条落 Go 惯用法)
```go
package cockpit

// StartOpts 启动一个会话所需。
type StartOpts struct {
    RepoDir      string // agent 的 cwd = 仓库根
    Seat         string // PACT_AGENT_ID
    Model        string // 可空
    SystemPrompt string // 可空
}

type UserMessage struct { Text string } // 先文本,留结构体便于扩展

// Decision 是审批决策(枚举字符串,透传各后端的语义)。
type Decision string
const (
    DecisionAllow           Decision = "allow"
    DecisionDeny            Decision = "deny"
    DecisionAllowForSession Decision = "allow_for_session"
)

type EventKind string
const (
    EventMessage EventKind = "message"
    EventTool    EventKind = "tool"
    EventPlan    EventKind = "plan"
    EventDiff    EventKind = "diff"
    EventUsage   EventKind = "usage"
    EventState   EventKind = "state"
    EventError   EventKind = "error"
)

// ToolEvent 描述一次工具调用的一段(start/output/end)。
type ToolEvent struct {
    Phase string // "start"|"output"|"end"
    Name  string
    Text  string // 输出增量或摘要
}

// Usage 是一次 usage 更新(token 计数,后端各取所能)。
type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
    CostUSD      float64
}

// Event 是统一信封;按 Kind 取对应字段,允许字段缺位(禁伪造)。Raw 留原始后端载荷。
type Event struct {
    Kind  EventKind
    Text  string          // message: 增量/终稿文本
    Final bool            // message: 是否终稿
    Tool  *ToolEvent      // Kind==EventTool
    Usage *Usage          // Kind==EventUsage
    State string          // Kind==EventState: "turn_started"|"turn_completed"|"turn_failed" 等
    Err   string          // Kind==EventError: 结构化错误摘要
    Raw   json.RawMessage // 原始后端载荷(可空)
}

// ApprovalRequest 是后端发起的审批请求;Respond 回决策(幂等,重复调用返回 error)。
// RawInput 是工具/命令的**完整原始参数**(审批卡信任根,禁用 Title 之类自由文本作根)。
type ApprovalRequest struct {
    Kind     string          // "command"|"file_change"|"permission"|"mcp_elicitation" 等
    ToolName string
    RawInput json.RawMessage
    Respond  func(Decision) error
}

type Backend interface {
    Start(ctx context.Context, opts StartOpts) (Session, error)
    Resume(ctx context.Context, threadID string) (Session, error)
}

type Session interface {
    Prompt(ctx context.Context, msg UserMessage) error // 串行队列;多轮同会话
    Interrupt(ctx context.Context) error
    Events() <-chan Event                 // 统一信封流(会话结束时 close)
    Approvals() <-chan ApprovalRequest    // 审批流(会话结束时 close)
    ThreadID() string                     // 持久续接句柄
    Close() error                         // 收割;幂等
}
```
需 import `context`、`encoding/json`。

## fake.go(内存实现,供 manager 后续用 + 本任务测试)
- `FakeBackend` 实现 `Backend`:`Start` 返回一个 `*FakeSession`;`Resume` 返回一个带该
  threadID 的 `*FakeSession`。可配置一个"脚本"(预置要发出的 []Event 和 []ApprovalRequest)。
- `FakeSession` 实现 `Session`:
  - 构造时起一个 goroutine 把脚本里的 Event 依次投进 Events() channel,然后 close;
    ApprovalRequest 同理投进 Approvals()。或提供 `Emit(Event)`/`EmitApproval(...)` 方法
    让测试手动驱动(更可控,推荐)。二选一,fake 要能被测试确定性地驱动。
  - `Prompt` 记录收到的消息(暴露一个 `Prompts []UserMessage` 便于断言);串行。
  - `ThreadID()` 返回可配置 id;`Close()` 幂等(第二次调用不 panic、close channel 只一次,
    用 sync.Once);`Interrupt` 记录被调用。
  - Respond 用 sync.Once 保证幂等:第二次 Respond 返回 error。

## 测试(backend_test.go)
- 编译期断言:`var _ Backend = (*FakeBackend)(nil)`、`var _ Session = (*FakeSession)(nil)`。
- 通过 Session 接口驱动 FakeSession:Emit 几个不同 Kind 的 Event → 从 Events() 收齐、顺序对、
  channel 在结束时 close(range 能正常退出)。
- ApprovalRequest:EmitApproval → 从 Approvals() 收到 → Respond(DecisionAllow) 成功、
  第二次 Respond 返回 error(sync.Once 幂等)。
- Prompt 记录、Close 幂等(调用两次不 panic)。

## 验收 / Acceptance(视角: maintainability — 接口贴 §2、fake 可确定性驱动、零现有代码改动)
- reviewer 独立跑 verify + 阅读确认接口签名与 §2 一致、fake 幂等正确。

## verify
verify: go build ./... && go test ./internal/cockpit/
