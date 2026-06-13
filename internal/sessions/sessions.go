package sessions

import "fmt"

// Runner runs an external command and returns combined output. Injected so tests
// fake the agent CLIs without spawning processes.
type Runner func(name string, args ...string) (string, error)

// Spec declares how to manage one agent kind's sessions. Command is the CLI
// binary; List lists sessions; Prune deletes them. An empty Prune means the kind
// has no verified headless session-prune command (cleanup unsupported — a no-op
// that reports so, never an error).
type Spec struct {
	Command string
	List    []string
	Prune   []string
}

// specs holds the per-kind session commands. Only gemini-cli has a verified
// headless session command set; the others are unsupported until confirmed (kept
// here as the single place to add them later).
var specs = map[string]Spec{
	"gemini-cli": {Command: "gemini", List: []string{"--list-sessions"}, Prune: nil},
}

// Manager prunes/lists sessions via an injected Runner.
type Manager struct{ Run Runner }

// Supported reports whether kind has any known session command.
func Supported(kind string) bool {
	s, ok := specs[kind]
	return ok && s.Command != ""
}

// CanPrune reports whether kind has a verified prune command.
func CanPrune(kind string) bool {
	s, ok := specs[kind]
	return ok && len(s.Prune) > 0
}

// List returns the kind's session listing (or an error if unsupported).
func (m Manager) List(kind string) (string, error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" || len(s.List) == 0 {
		return "", fmt.Errorf("sessions: listing not supported for kind %q", kind)
	}
	return m.Run(s.Command, s.List...)
}

// Prune deletes the kind's sessions. When the kind has no verified prune command
// it returns (skipped=true, nil) — a graceful no-op, NOT an error — so callers can
// report "nothing to prune (unsupported)" instead of failing.
func (m Manager) Prune(kind string) (output string, skipped bool, err error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" || len(s.Prune) == 0 {
		return "", true, nil
	}
	out, err := m.Run(s.Command, s.Prune...)
	return out, false, err
}
