package cockpit

import (
	"context"
	"encoding/json"
)

// StartOpts 启动一个会话所需。
type StartOpts struct {
	RepoDir      string // agent 的 cwd = 仓库根
	Seat         string // PACT_AGENT_ID
	Model        string // 可空
	SystemPrompt string // 可空
}

// UserMessage 是用户发送给后端的文本消息。
type UserMessage struct{ Text string }

// Decision 是审批决策（枚举字符串，透传各后端的语义）。
type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionDeny            Decision = "deny"
	DecisionAllowForSession Decision = "allow_for_session"
)

// EventKind 是统一信封的事件类型。
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

// ToolEvent 描述一次工具调用的一段（start/output/end）。
type ToolEvent struct {
	Phase string // "start"|"output"|"end"
	Name  string
	Text  string // 输出增量或摘要
}

// Usage 是一次 usage 更新（token 计数，后端各取所能）。
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CostUSD      float64
}

// Event 是统一信封；按 Kind 取对应字段，允许字段缺位（禁伪造）。Raw 留原始后端载荷。
type Event struct {
	Kind  EventKind
	Text  string          // message: 增量/终稿文本
	Final bool            // message: 是否终稿
	Tool  *ToolEvent      // Kind==EventTool
	Usage *Usage          // Kind==EventUsage
	State string          // Kind==EventState: "turn_started"|"turn_completed"|"turn_failed" 等
	Err   string          // Kind==EventError: 结构化错误摘要
	Raw   json.RawMessage // 原始后端载荷（可空）
}

// ApprovalRequest 是后端发起的审批请求；Respond 回决策（幂等，重复调用返回 error）。
// RawInput 是工具/命令的完整原始参数（审批卡信任根，禁用 Title 之类自由文本作根）。
type ApprovalRequest struct {
	Kind     string // "command"|"file_change"|"permission"|"mcp_elicitation" 等
	ToolName string
	RawInput json.RawMessage
	Respond  func(Decision) error
}

// Backend 是会话工厂；具体后端（codex / claude / acp）实现它。
type Backend interface {
	Start(ctx context.Context, opts StartOpts) (Session, error)
	Resume(ctx context.Context, threadID string) (Session, error)
}

// Session 是一次与后端的对话；消费者从 Events/Approvals 读取，调用 Prompt 推进。
type Session interface {
	Prompt(ctx context.Context, msg UserMessage) error // 串行队列；多轮同会话
	Interrupt(ctx context.Context) error
	Events() <-chan Event              // 统一信封流（会话结束时 close）
	Approvals() <-chan ApprovalRequest // 审批流（会话结束时 close）
	ThreadID() string                  // 持久续接句柄
	Close() error                      // 收割；幂等
}
