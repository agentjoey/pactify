// Package gitx wraps the git CLI for the side-effects pact verbs perform.
package gitx

import (
	"os/exec"
	"strings"
)

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}

// CurrentBranch returns the current branch name.
func CurrentBranch(dir string) (string, error) { return run(dir, "branch", "--show-current") }

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

// CommitAll stages everything and commits with msg.
func CommitAll(dir, msg string) error {
	if _, err := run(dir, "add", "-A"); err != nil {
		return err
	}
	_, err := run(dir, "commit", "-q", "-m", msg)
	return err
}

// CommitPaths stages only the given paths and commits with msg — the safe variant
// for committing a known set of files (e.g. .gitignore scaffolding) without
// vacuuming the user's unrelated working-tree changes the way `add -A` would.
func CommitPaths(dir, msg string, paths ...string) error {
	if _, err := run(dir, append([]string{"add", "--"}, paths...)...); err != nil {
		return err
	}
	_, err := run(dir, "commit", "-q", "-m", msg)
	return err
}

// IsAncestor reports whether commit-ish a is an ancestor of (or identical to) b.
// Used to verify a merge actually landed: after merging a feature branch, base
// must contain it.
func IsAncestor(dir, a, b string) bool {
	_, err := run(dir, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// MergeNoFF performs a --no-ff merge of branch into the current branch.
func MergeNoFF(dir, branch, msg string) error {
	_, err := run(dir, "merge", "--no-ff", "-m", msg, branch)
	return err
}
