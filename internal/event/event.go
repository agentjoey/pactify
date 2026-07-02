// Package event defines the pact log event envelope and the append-only log.
package event

// Event is the frozen v1 log envelope (see docs/specs/pact-protocol.md §2).
type Event struct {
	EventID   string         `json:"event_id"`
	TS        string         `json:"ts"`
	AgentID   string         `json:"agent_id"`
	Role      string         `json:"role"`
	EventType string         `json:"event_type"`
	TaskID    string         `json:"task_id"`
	Feature   string         `json:"feature"`
	Payload   map[string]any `json:"payload"`
}

var roleByType = map[string]string{
	"init": "orchestrator", "assign": "orchestrator", "merge": "orchestrator",
	"cancel": "orchestrator", "withdraw": "orchestrator", "rebaseline": "orchestrator",
	"config_gate": "orchestrator", "start": "orchestrator",
	"join": "worker", "checkpoint": "worker",
	"accept": "reviewer", "changes_requested": "reviewer",
}

// RoleFor derives the actor role from the event/verb (self-reported roles are not trusted).
func RoleFor(eventType string) string { return roleByType[eventType] }
