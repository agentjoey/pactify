package pact

import (
	"os"
	"path/filepath"
)

// Project is a dir-aware handle onto a pact repo. Every protocol verb is a
// method on *Project; the package-level funcs (Init, Assign, …) are thin
// wrappers over At(".") and preserve the historical cwd-bound behavior.
//
// dir is the repo root all paths and git ops are rooted at. actor, when
// non-empty, overrides PACT_AGENT_ID for this handle (see As) so a caller can
// act as a given seat without mutating process env.
type Project struct {
	dir   string
	actor string
}

// At returns a handle rooted at dir. Use "." for the historical cwd behavior.
//
// When dir is "." AND PACT_DIR is an ABSOLUTE path, the handle is rooted at the
// repo CONTAINING that .pact (filepath.Dir(PACT_DIR)) rather than the process cwd.
// This keeps the ledger and the git ops in lock-step: the orchestrate runner pins
// a worker to the driver's worktree by exporting an absolute PACT_DIR, and the
// worker's checkpoint (via the cwd-bound MCP / package wrappers) must then commit
// into THAT repo, not wherever the agent happens to have chdir'd. Without this the
// ledger lands in PACT_DIR while git runs in cwd — the split that silently dropped
// opencode's delivery (worker checkpointed, branch left empty, driver saw nothing).
func At(dir string) *Project {
	if dir == "." {
		if d := os.Getenv("PACT_DIR"); filepath.IsAbs(d) {
			dir = filepath.Dir(d)
		}
	}
	return &Project{dir: dir}
}

// Dir returns the repo root this handle is rooted at, after the PACT_DIR
// resolution in At. Callers that need to inspect repo state outside the engine
// (the checkpoint run-guard reads .pact/orchestrate/) must use this rather than
// the process cwd, or they inspect a different repo than the verb writes to.
func (p *Project) Dir() string { return p.dir }

// As returns a copy of p acting as seat, overriding PACT_AGENT_ID for the
// returned handle only. The receiver is left unchanged.
func (p *Project) As(seat string) *Project {
	q := *p
	q.actor = seat
	return &q
}
