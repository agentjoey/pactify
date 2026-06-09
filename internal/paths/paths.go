// Package paths resolves the .pact/ locations and protocol constants.
package paths

import (
	"os"
	"path/filepath"
)

// ProtocolVersion is the protocol major this binary implements.
const ProtocolVersion = 1

// Dir is the pact directory (PACT_DIR env override, default ".pact").
func Dir() string {
	if d := os.Getenv("PACT_DIR"); d != "" {
		return d
	}
	return ".pact"
}

func Log() string   { return filepath.Join(Dir(), "log.jsonl") }
func State() string { return filepath.Join(Dir(), "STATE.yml") }
func Tasks() string { return filepath.Join(Dir(), "tasks") }
func Bin() string   { return filepath.Join(Dir(), "bin") }

// AgentID returns PACT_AGENT_ID (may be "").
func AgentID() string { return os.Getenv("PACT_AGENT_ID") }
