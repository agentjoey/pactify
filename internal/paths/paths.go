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

// StateSnapshot is the persistent ledger snapshot cache (a projection of the log,
// NOT the source of truth). It is never committed — see the runtime-ignore wiring.
func StateSnapshot() string { return filepath.Join(Dir(), "state-snapshot.json") }

// DirIn resolves the pact directory rooted at base. It preserves the PACT_DIR
// override semantics of Dir(): if Dir() is absolute (e.g. PACT_DIR is an
// absolute path), it is returned verbatim and base is ignored; otherwise the
// relative pact dir is joined onto base. base="." reproduces Dir() exactly.
func DirIn(base string) string {
	d := Dir()
	if filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(base, d)
}

// LogIn, StateIn, TasksIn, BinIn are the base-rooted analogues of Log/State/
// Tasks/Bin, layered on DirIn so the PACT_DIR-absolute case is honoured.
func LogIn(base string) string   { return filepath.Join(DirIn(base), "log.jsonl") }
func StateIn(base string) string { return filepath.Join(DirIn(base), "STATE.yml") }
func TasksIn(base string) string { return filepath.Join(DirIn(base), "tasks") }
func BinIn(base string) string   { return filepath.Join(DirIn(base), "bin") }

// StateSnapshotIn is the base-rooted analogue of StateSnapshot.
func StateSnapshotIn(base string) string {
	return filepath.Join(DirIn(base), "state-snapshot.json")
}

// AgentID returns PACT_AGENT_ID (may be "").
func AgentID() string { return os.Getenv("PACT_AGENT_ID") }
