package cockpit

import (
	"context"
	"encoding/json"
	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"io"
)

// Access selects explicit delegation permissions; the zero value preserves backend defaults.
type Access string

const (
	AccessDefault  Access = ""
	AccessReadOnly Access = "read-only"
	AccessWrite    Access = "write"
)

// StartOpts contains what is needed to start a session.
type StartOpts struct {
	// Transcript is opt-in sanitized protocol recording; normal jobs leave it nil.
	Transcript          *golden.Recorder
	ReadOnlyPolicy      bool   // Exercise the managed read-only hook with native write access (doctor).
	CompletionFollowups *int   // agy: nil uses two completion followups; zero disables them.
	RepoDir             string // agent cwd = repository root
	Seat                string // PACT_AGENT_ID
	Model               string // when non-empty, must be honored; otherwise Start returns an error
	SystemPrompt        string // optional
	Effort              string // optional; per-turn reasoning effort
	Access              Access
	WritableRoots       []string  // codex: additional writable directories; opencode write: roots plus its literal cwd.
	Env                 []string  // appended after the inherited environment; same-name keys use these values
	Stderr              io.Writer // nil → os.Stderr
	// Hidden keeps a delegated session out of the user's own thread list: codex
	// archives the thread when the session closes and unarchives it before a
	// resume. Backends without such a list ignore it.
	Hidden bool
	// MinimalConfig disables optional user integrations for delegated sessions.
	// The zero value preserves the shared cockpit backends' existing behavior.
	MinimalConfig bool
}

// SessionInfo reports actual session parameters; empty fields were not reported.
type SessionInfo struct {
	Home     string // Actual data directory, when supplied by the session owner.
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

// UserMessage is a text message sent by the user to the backend.
type UserMessage struct{ Text string }

// Decision is an approval decision (an enum string, passing through each backend's semantics).
type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionDeny            Decision = "deny"
	DecisionAllowForSession Decision = "allow_for_session"
)

// EventKind is the event type of the unified envelope.
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

// ToolEvent describes one segment of a tool call (start/output/end).
type ToolEvent struct {
	Phase string `json:"phase"` // "start"|"output"|"end"
	Name  string `json:"name"`
	Text  string `json:"text"` // output delta or summary
}

// Usage is one usage update (token counts, using what each backend provides).
type Usage struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	TotalTokens  int     `json:"totalTokens"`
	CostUSD      float64 `json:"costUSD"`
}

// Event is the unified envelope; select fields by Kind, and fields may be absent (fabrication is forbidden). Raw holds the original backend payload.
// JSON tags are lowercase: this struct is the SSE wire shape the dashboard's
// CockpitPanel consumes (ev.kind / ev.text / ev.tool / …); without tags Go would
// emit capitalized keys and every event would render as an empty system row.
type Event struct {
	Kind  EventKind       `json:"kind"`
	Text  string          `json:"text,omitempty"`  // message: delta/final text
	Final bool            `json:"final,omitempty"` // message or terminal state: whether this is the final text
	Tool  *ToolEvent      `json:"tool,omitempty"`  // Kind==EventTool
	Usage *Usage          `json:"usage,omitempty"` // Kind==EventUsage
	State string          `json:"state,omitempty"` // Kind==EventState: "turn_started"|"turn_completed"|"turn_failed" etc.
	Err   string          `json:"err,omitempty"`   // Kind==EventError: structured error summary
	Raw   json.RawMessage `json:"raw,omitempty"`   // original backend payload (optional)
}

// ApprovalRequest is an approval request initiated by the backend; Respond returns a decision (idempotent; repeated calls return an error).
// RawInput is the complete original arguments of the tool/command (the trust root of the approval card; free text such as Title must not be used as the root).
type ApprovalRequest struct {
	Kind     string // "command"|"file_change"|"permission"|"mcp_elicitation" etc.
	ToolName string
	RawInput json.RawMessage
	Respond  func(Decision) error
}

// Backend is a session factory; concrete backends (codex / claude / acp) implement it.
type Backend interface {
	Start(ctx context.Context, opts StartOpts) (Session, error)
	Resume(ctx context.Context, threadID string) (Session, error)
}

// Session is one conversation with a backend; consumers read from Events/Approvals and call Prompt to advance.
type Session interface {
	Prompt(ctx context.Context, msg UserMessage) error // serialized queue; multiple turns in the same session
	Interrupt(ctx context.Context) error
	Events() <-chan Event              // unified envelope stream (closed when the session ends)
	Approvals() <-chan ApprovalRequest // approval stream (closed when the session ends)
	ThreadID() string                  // persistent resume handle
	Close() error                      // reap; idempotent
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
