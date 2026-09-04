// Package ledger is the one place that knows where a repo's pact ledger lives
// and how to read it.
//
// # Why this exists apart from internal/paths
//
// There are two legitimate ways to ask "where is the ledger?", and conflating
// them is a real bug, not a style question:
//
//   - **Process-scoped** (`paths.LogIn`): the ledger of the repo THIS PROCESS is
//     bound to. When PACT_DIR is an absolute path it wins outright and the base
//     argument is ignored (paths.DirIn). That is deliberate and load-bearing:
//     the orchestrate runner exports an absolute PACT_DIR to pin a worker to the
//     driver's worktree, so the worker's checkpoint commits into THAT repo
//     rather than wherever the agent happened to chdir.
//
//   - **Project-scoped** (this package): the ledger of a repo named by its root,
//     regardless of the calling process's environment. This is what any
//     multi-project caller needs — serve holds a registry of many projects and
//     must answer about the one it was asked about.
//
// Using the process-scoped form in a multi-project caller silently redirects
// every read to whatever repo PACT_DIR names. It is invisible in review, and
// invisible in tests too, because the suites neutralize PACT_DIR
// (internal/testenv.Isolate). internal/serve pins the rule with
// TestServeNeverResolvesLedgerPathsFromProcessEnv.
package ledger

import (
	"path/filepath"

	"github.com/agentjoey/pactify/internal/event"
)

// dirName is the pact directory name as it appears inside a repo. Project-scoped
// resolution deliberately does NOT honour a PACT_DIR rename: a registry entry
// names a repo root, and the ledger of that repo is at this fixed location.
const dirName = ".pact"

// Dir returns repoRoot's pact directory.
func Dir(repoRoot string) string { return filepath.Join(repoRoot, dirName) }

// Path returns repoRoot's append-only event log.
func Path(repoRoot string) string { return filepath.Join(Dir(repoRoot), "log.jsonl") }

// StatePath returns repoRoot's rendered STATE.yml projection.
func StatePath(repoRoot string) string { return filepath.Join(Dir(repoRoot), "STATE.yml") }

// Read returns every event in repoRoot's ledger. A missing log folds to an empty
// slice with no error, matching event.ReadAll — an uninitialized project is not
// an error condition for a reader.
func Read(repoRoot string) ([]event.Event, error) { return event.ReadAll(Path(repoRoot)) }
