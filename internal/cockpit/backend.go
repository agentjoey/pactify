package cockpit

import (
	"context"
	"encoding/json"
	"io"
)

// Access selects explicit delegation permissions; the zero value preserves backend defaults.
type Access string

const (
	AccessDefault  Access = ""
	AccessReadOnly Access = "read-only"
	AccessWrite    Access = "write"
)

// StartOpts 启动一个会话所需。
type StartOpts struct {
	RepoDir      string // agent 的 cwd = 仓库根
	Seat         string // PACT_AGENT_ID
	Model        string // 非空时必须兑现，否则 Start 返回错误
	SystemPrompt string // 可空
	Effort       string // 可空；每轮推理 effort
	Access       Access
	Env          []string  // 追加在继承环境之后；同名键以此为准
	Stderr       io.Writer // nil → os.Stderr
	// Hidden keeps a delegated session out of the user's own thread list: codex
	// archives the thread when the session closes and unarchives it before a
	// resume. Backends without such a list ignore it.
	Hidden bool
}

// SessionInfo reports actual session parameters; empty fields were not reported.
type SessionInfo struct {
	Model    string
	Provider string
	Effort   string
	Access   string
}

// InfoSession is implemented by sessions that report actual runtime parameters.
type InfoSession interface{ Info() SessionInfo }

// OptsResumer can resume with explicit options; Resume uses zero-value options.
type OptsResumer interface {
	ResumeWith(ctx context.Context, threadID string, opts StartOpts) (Session, error)
}

// Terminal turn states.
const (
	StateTurnCompleted = "turn_completed"
	StateTurnFailed    = "turn_failed"
)

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
	EventSystem  EventKind = "system"
)

// ToolEvent 描述一次工具调用的一段（start/output/end）。
type ToolEvent struct {
	Phase string `json:"phase"` // "start"|"output"|"end"
	Name  string `json:"name"`
	Text  string `json:"text"` // 输出增量或摘要
}

// Usage 是一次 usage 更新（token 计数，后端各取所能）。
type Usage struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	TotalTokens  int     `json:"totalTokens"`
	CostUSD      float64 `json:"costUSD"`
}

// Event 是统一信封；按 Kind 取对应字段，允许字段缺位（禁伪造）。Raw 留原始后端载荷。
// JSON tags are lowercase: this struct is the SSE wire shape the dashboard's
// CockpitPanel consumes (ev.kind / ev.text / ev.tool / …); without tags Go would
// emit capitalized keys and every event would render as an empty system row.
type Event struct {
	Kind  EventKind       `json:"kind"`
	Text  string          `json:"text,omitempty"`  // message: 增量/终稿文本
	Final bool            `json:"final,omitempty"` // message: 是否终稿
	Tool  *ToolEvent      `json:"tool,omitempty"`  // Kind==EventTool
	Usage *Usage          `json:"usage,omitempty"` // Kind==EventUsage
	State string          `json:"state,omitempty"` // Kind==EventState: "turn_started"|"turn_completed"|"turn_failed" 等
	Err   string          `json:"err,omitempty"`   // Kind==EventError: 结构化错误摘要
	Raw   json.RawMessage `json:"raw,omitempty"`   // 原始后端载荷（可空）
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

// StartError preserves the ID of a session created before configuration failed.
// Delegation callers can conceal it without receiving a usable Session.
type StartError struct {
	ID  string
	Err error
}

func (e *StartError) Error() string     { return e.Err.Error() }
func (e *StartError) Unwrap() error     { return e.Err }
func (e *StartError) SessionID() string { return e.ID }
