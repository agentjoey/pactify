package pact

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
func At(dir string) *Project { return &Project{dir: dir} }

// As returns a copy of p acting as seat, overriding PACT_AGENT_ID for the
// returned handle only. The receiver is left unchanged.
func (p *Project) As(seat string) *Project {
	q := *p
	q.actor = seat
	return &q
}
