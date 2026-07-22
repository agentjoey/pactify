// Package paths resolves the .pact/ locations and protocol constants.
package paths

import (
	"os"
	"path/filepath"
	"strings"
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

// AgentID returns PACT_AGENT_ID (may be ""). Env-only; prefer AgentIDIn, which
// adds the untracked seat-file layer (spec seat-identity §3.1).
func AgentID() string { return os.Getenv("PACT_AGENT_ID") }

// Seat identity source, reported by AgentIDIn so `pactify seat` can explain where
// the acting identity came from.
const (
	SourceEnv        = "env"        // process env PACT_AGENT_ID
	SourceFile       = "file"       // the .pact/seat working-copy file
	SourceUnresolved = "unresolved" // neither present
)

// SeatFileIn is the per-working-copy seat file: an untracked single-line file
// holding this checkout's default seat id (spec seat-identity §3.1). It lives
// under the pact dir so it travels with PACT_DIR addressing.
func SeatFileIn(base string) string { return filepath.Join(DirIn(base), "seat") }

// AgentIDIn resolves the acting seat through the identity chain (spec
// seat-identity §3.1): process env PACT_AGENT_ID > the untracked .pact/seat file
// > unresolved. It returns the id and its source. A blank/whitespace-only file is
// treated as absent (never a blank seat id, which would corrupt the ledger).
func AgentIDIn(base string) (id, source string) {
	if v := strings.TrimSpace(os.Getenv("PACT_AGENT_ID")); v != "" {
		return v, SourceEnv
	}
	if b, err := os.ReadFile(SeatFileIn(base)); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v, SourceFile
		}
	}
	return "", SourceUnresolved
}
