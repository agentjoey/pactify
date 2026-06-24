package serve

import (
	"net/http"
	"os/exec"
	"strings"
)

// WorktreeDTO is one git worktree of a project's repo. Primary is the repo's
// main working tree (the registered project path).
type WorktreeDTO struct {
	Branch  string `json:"branch"`
	Path    string `json:"path"`
	Primary bool   `json:"primary"`
}

// execGitWorktree runs `git worktree list --porcelain` for repoPath. Swappable
// in tests.
var execGitWorktree = func(repoPath string) ([]byte, error) {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain")
	return cmd.Output()
}

// gitWorktrees parses `git worktree list --porcelain`. Each record is a block of
// lines: "worktree <path>", optional "HEAD <sha>", optional "branch refs/heads/<b>"
// or "detached", separated by a blank line. The FIRST worktree is the primary.
// On any git error (not a repo, no git) it returns a single primary entry for
// repoPath so callers always have at least the project itself.
func gitWorktrees(repoPath string) []WorktreeDTO {
	out, err := execGitWorktree(repoPath)
	if err != nil {
		return []WorktreeDTO{{Path: repoPath, Primary: true}}
	}
	var res []WorktreeDTO
	var cur *WorktreeDTO
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			res = append(res, WorktreeDTO{Path: strings.TrimPrefix(line, "worktree ")})
			cur = &res[len(res)-1]
		case strings.HasPrefix(line, "branch ") && cur != nil:
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	if len(res) == 0 {
		return []WorktreeDTO{{Path: repoPath, Primary: true}}
	}
	res[0].Primary = true
	return filterInternalWorktrees(res)
}

// filterInternalWorktrees drops pact's OWN run worktrees from the project list:
// the sandbox/parallel trees live under .pact/orchestrate/ and a parked main sits
// on a pact-*-park branch. These hold transient, mid-run-divergent ledger state
// (the worker's live .pact vs the parked main's seed) — surfacing them as
// selectable boards is what made "same task, two trees, inconsistent status". The
// primary is always kept (a parked main is still the project's canonical view).
func filterInternalWorktrees(ws []WorktreeDTO) []WorktreeDTO {
	kept := ws[:0]
	for i, w := range ws {
		if i == 0 { // primary: always the project itself
			kept = append(kept, w)
			continue
		}
		if strings.Contains(w.Path, "/.pact/orchestrate/") {
			continue
		}
		if w.Branch == "pact-sandbox-park" || w.Branch == "pact-parallel-park" {
			continue
		}
		kept = append(kept, w)
	}
	return kept
}

// resolveWorktreePath returns the on-disk path of the worktree on branch `wt`
// within repoPath's worktrees, and whether it was found. Guards against reading
// an arbitrary path: only a real worktree of this repo resolves.
func resolveWorktreePath(repoPath, wt string) (string, bool) {
	for _, w := range gitWorktrees(repoPath) {
		if w.Branch == wt {
			return w.Path, true
		}
	}
	return "", false
}

func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	writeJSON(w, http.StatusOK, gitWorktrees(dir))
}
