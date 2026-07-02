// Package gitx wraps the git CLI for the side-effects pact verbs perform.
package gitx

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}

// GitPath resolves a path inside the git dir (e.g. "info/exclude"), correctly
// handling worktrees where .git is a file pointing at a separate gitdir. Returns
// an absolute path rooted at dir when git reports it relative.
func GitPath(dir, rel string) (string, error) {
	out, err := run(dir, "rev-parse", "--git-path", rel)
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-path %s: %w (%s)", rel, err, out)
	}
	if filepath.IsAbs(out) {
		return out, nil
	}
	return filepath.Join(dir, out), nil
}

// CurrentBranch returns the current branch name.
func CurrentBranch(dir string) (string, error) { return run(dir, "branch", "--show-current") }

// PathIgnored reports whether rel is git-ignored in dir (per .gitignore /
// .git/info/exclude). `git check-ignore` exits 0 when the path is ignored, 1
// when it is not, and >1 on a real error — so only a clean exit-0 counts as
// ignored (an error, e.g. dir is not a repo, is treated as not-ignored).
func PathIgnored(dir, rel string) bool {
	_, err := run(dir, "check-ignore", "-q", rel)
	return err == nil
}

// BranchExists reports whether a local branch exists.
func BranchExists(dir, branch string) bool {
	_, err := run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// CheckoutOrCreate switches to branch, creating it from HEAD if absent.
func CheckoutOrCreate(dir, branch string) error {
	var err error
	if BranchExists(dir, branch) {
		_, err = run(dir, "checkout", "-q", branch)
	} else {
		_, err = run(dir, "checkout", "-q", "-b", branch)
	}
	return err
}

// Checkout switches to an existing branch.
func Checkout(dir, branch string) error {
	_, err := run(dir, "checkout", "-q", branch)
	return err
}

// HasChanges reports whether the working tree has uncommitted changes.
func HasChanges(dir string) (bool, error) {
	out, err := run(dir, "status", "--porcelain")
	return out != "", err
}

// CommitAll stages everything and commits with msg. --no-verify: pactify commits
// are machine-generated and must never run the repo's HUMAN commit hooks
// (commitlint/lint-staged) — a rejecting pre-commit hook would otherwise drop the
// work and leave the ledger ahead of git (the checkpoint-phantom).
func CommitAll(dir, msg string) error {
	if _, err := run(dir, "add", "-A"); err != nil {
		return err
	}
	_, err := run(dir, "commit", "-q", "--no-verify", "-m", msg)
	return err
}

// CommitPaths stages only the given paths and commits with msg — the safe variant
// for committing a known set of files (e.g. .gitignore scaffolding) without
// vacuuming the user's unrelated working-tree changes the way `add -A` would.
// --no-verify for the same reason as CommitAll: machine commits skip human hooks.
func CommitPaths(dir, msg string, paths ...string) error {
	if _, err := run(dir, append([]string{"add", "--"}, paths...)...); err != nil {
		return err
	}
	_, err := run(dir, "commit", "-q", "--no-verify", "-m", msg)
	return err
}

// DefaultBranch returns the repo's default branch via origin/HEAD (e.g. "main"),
// or "" when it can't be determined (no origin, or origin/HEAD unset). Used to
// catch a pact base branch that was accidentally captured as a feature branch.
func DefaultBranch(dir string) string {
	out, err := run(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil || out == "" {
		return ""
	}
	return strings.TrimPrefix(out, "origin/")
}

// IsAncestor reports whether commit-ish a is an ancestor of (or identical to) b.
// Used to verify a merge actually landed: after merging a feature branch, base
// must contain it.
func IsAncestor(dir, a, b string) bool {
	_, err := run(dir, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// MergeNoFF performs a --no-ff merge of branch into the current branch.
// --no-verify: the merge commit is machine-generated and must not run the repo's
// human merge/commit hooks (pre-merge-commit / commit-msg, e.g. commitlint).
func MergeNoFF(dir, branch, msg string) error {
	out, err := run(dir, "merge", "--no-ff", "--no-verify", "-m", msg, branch)
	if err != nil {
		// On conflict (or any failure) git leaves a half-merged, conflicted working
		// tree. Abort to restore a clean tree on the base branch — never leave the
		// repo mid-merge — and surface a clear error for the caller to escalate.
		_, _ = run(dir, "merge", "--abort")
		return fmt.Errorf("merge %s failed and was aborted (tree restored clean): %s", branch, strings.TrimSpace(out))
	}
	return nil
}

// HasRemote reports whether the named remote is configured.
func HasRemote(dir, name string) bool {
	_, err := run(dir, "remote", "get-url", name)
	return err == nil
}

// Push pushes branch to remote.
func Push(dir, remote, branch string) error {
	if out, err := run(dir, "push", remote, branch); err != nil {
		return fmt.Errorf("push %s %s: %s", remote, branch, strings.TrimSpace(out))
	}
	return nil
}

// Fetch updates remote-tracking refs for ref from remote (e.g. "origin", "main").
func Fetch(dir, remote, ref string) error {
	if out, err := run(dir, "fetch", "--quiet", remote, ref); err != nil {
		return fmt.Errorf("fetch %s %s: %s", remote, ref, strings.TrimSpace(out))
	}
	return nil
}

// RemoteBranchExists reports whether the remote-tracking ref remote/branch exists
// locally (after a fetch).
func RemoteBranchExists(dir, remote, branch string) bool {
	_, err := run(dir, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/"+branch)
	return err == nil
}

// FastForwardTo fast-forwards local branch to ref (e.g. origin/main), checking it
// out first. It refuses (errors) when branch has diverged — i.e. carries commits
// not on ref — rather than creating a merge: base must only ever advance, never
// fork. A no-op when branch is already at/ahead-containing ref.
func FastForwardTo(dir, branch, ref string) error {
	if err := Checkout(dir, branch); err != nil {
		return err
	}
	// Already contains ref → nothing to pull forward (local is at or ahead of ref).
	if IsAncestor(dir, ref, branch) {
		return nil
	}
	out, err := run(dir, "merge", "--ff-only", ref)
	if err != nil {
		return fmt.Errorf("local %s diverged from %s (cannot fast-forward): %s", branch, ref, strings.TrimSpace(out))
	}
	return nil
}

// RebaseOnto rebases branch onto ref (checking branch out first). On any failure
// (conflict) it aborts the rebase to restore a clean tree and returns an error —
// never leaving the repo mid-rebase. A no-op when branch already sits on ref.
func RebaseOnto(dir, branch, ref string) error {
	if err := Checkout(dir, branch); err != nil {
		return err
	}
	if IsAncestor(dir, ref, branch) {
		return nil // already on top of ref
	}
	out, err := run(dir, "rebase", ref, branch)
	if err != nil {
		_, _ = run(dir, "rebase", "--abort")
		return fmt.Errorf("rebase %s onto %s failed and was aborted (tree restored clean): %s", branch, ref, strings.TrimSpace(out))
	}
	return nil
}
