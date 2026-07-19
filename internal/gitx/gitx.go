// Package gitx wraps the git CLI for the side-effects pact verbs perform.
package gitx

import (
	"fmt"
	"os"
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

// GitCommonPath resolves a path inside the SHARED git common dir — .git of the
// main tree even when dir is a linked worktree (where GitPath resolves into the
// per-worktree gitdir). This is the handle for repo-wide resources like the
// base-integration lock: every worktree must contend on the same file.
func GitCommonPath(dir, rel string) (string, error) {
	out, err := run(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		// Older git (<2.31) lacks --path-format; the bare query may print a
		// dir-relative path, rooted at dir below (same handling as GitPath).
		out, err = run(dir, "rev-parse", "--git-common-dir")
		if err != nil {
			return "", fmt.Errorf("git rev-parse --git-common-dir: %w (%s)", err, out)
		}
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	return filepath.Join(out, rel), nil
}

// EnsureExcluded appends marker to the repo's .git/info/exclude — the per-clone,
// NEVER-committed ignore list — when it is not already present. Idempotent. This is
// the same runtime-ignore mechanism v0.8.0 wired for orchestrate's .pact/orchestrate/
// artifacts (spec coordination-authority P0a: routing runtime ignores here instead
// of committing .gitignore keeps the writer off any tracked branch); it is exported
// so other packages — the ledger snapshot cache — can share the one接线.
func EnsureExcluded(dir, marker string) error {
	excl, err := GitPath(dir, "info/exclude")
	if err != nil {
		return err
	}
	cur, err := os.ReadFile(excl)
	switch {
	case err == nil && strings.Contains(string(cur), marker):
		return nil
	case err == nil:
		return os.WriteFile(excl, append(cur, []byte("\n"+marker+"\n")...), 0o644)
	case os.IsNotExist(err):
		return os.WriteFile(excl, []byte(marker+"\n"), 0o644)
	default:
		return err
	}
}

// ValidBranchName reports whether name is a well-formed git branch name (per
// `git check-ref-format --branch`). Two shapes are rejected up front, before any
// shell-out: a dash-prefixed name, because check-ref-format takes the name as a
// positional argument with no "--" separator — "-evil" would be parsed as a flag
// by the very tool meant to vet it; and anything containing "@{", because
// --branch mode EXPANDS previous-checkout syntax ("@{-1}") to a real branch name
// instead of rejecting it.
func ValidBranchName(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.Contains(name, "@{") {
		return false
	}
	_, err := run(".", "check-ref-format", "--branch", name)
	return err == nil
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

// PathTracked reports whether rel is already tracked in dir's git index (i.e.
// someone has committed it before — `git ls-files` lists it). Distinct from
// "not ignored": a brand-new file can be neither ignored nor tracked yet.
// Callers use this to gate ledger auto-commit (see pact.appendAndRender) so a
// project that has never added .pact/ to git is never surprised by pactify
// silently starting to commit it — only a repo that already tracks it (a
// deliberate choice, as this repo itself makes to dogfood a full audit
// history) gets the ledger kept continuously committed.
func PathTracked(dir, rel string) bool {
	_, err := run(dir, "ls-files", "--error-unmatch", "--", rel)
	return err == nil
}

// CheckoutOrCreate switches to branch, creating it from HEAD if absent. On
// failure the error carries git's actual stderr/stdout (via wrapCheckoutErr) —
// a bare `exit status 1` swallows the one thing a caller needs to diagnose it
// (e.g. "already used by worktree at ...", "pathspec did not match"), which
// cost real diagnostic time chasing an orchestrate sandbox failure (2026-07-05
// dogfood P4/P5) whose actual cause was hidden behind exactly this.
func CheckoutOrCreate(dir, branch string) error {
	var out string
	var err error
	if BranchExists(dir, branch) {
		out, err = run(dir, "checkout", "-q", branch)
	} else {
		out, err = run(dir, "checkout", "-q", "-b", branch)
	}
	return wrapGitErr(err, out)
}

// wrapGitErr appends run()'s combined output to err so the caller's error
// message carries git's actual diagnostic text instead of a bare "exit status
// N". out is empty on success, so this is a no-op then.
func wrapGitErr(err error, out string) error {
	if err == nil {
		return nil
	}
	if out == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, out)
}

// TreeFingerprint captures the working tree's observable state — HEAD commit
// plus the porcelain status of every tracked/untracked change — as an opaque
// string. Two equal fingerprints mean nothing in the tree changed between the
// captures: no new commit, no file added/modified/deleted. (Ignored files are
// invisible on both sides, so runtime noise routed through .git/info/exclude
// never perturbs the fingerprint.)
func TreeFingerprint(dir string) string {
	head, _ := run(dir, "rev-parse", "HEAD")
	status, _ := run(dir, "status", "--porcelain")
	return strings.TrimSpace(head) + "\n" + status
}

// CheckoutReset checks out branch, creating it at — or RESETTING it to — the
// current HEAD (`git checkout -B`). For scratch branches whose only valid
// position is "where HEAD is right now": reusing a stale pointer (as
// CheckoutOrCreate would) rewinds the working tree to wherever the branch last
// pointed.
func CheckoutReset(dir, branch string) error {
	out, err := run(dir, "checkout", "-q", "-B", branch)
	return wrapGitErr(err, out)
}

// DeleteBranch force-deletes a local branch (`git branch -D`).
func DeleteBranch(dir, branch string) error {
	out, err := run(dir, "branch", "-q", "-D", branch)
	return wrapGitErr(err, out)
}

// Checkout switches to an existing branch.
func Checkout(dir, branch string) error {
	out, err := run(dir, "checkout", "-q", branch)
	return wrapGitErr(err, out)
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

// HasMergeOfBranch reports whether base's history contains a merge commit that
// integrated branch — a merge commit carrying branch's tip as a merged-in (non-
// first) parent. IsAncestor(branch, base) alone can't tell "branch was merged
// into base" from "branch never diverged, so it's trivially an ancestor"; both
// are true. Only a real --no-ff merge leaves such a commit, so this is the safe
// signal that a prior merge actually landed (used to recover a merge whose git
// step committed but whose ledger event was never recorded — a crash between the
// two — without re-running or misjudging a genuinely empty feature).
func HasMergeOfBranch(dir, base, branch string) (bool, error) {
	tip, err := run(dir, "rev-parse", "--verify", branch+"^{commit}")
	if err != nil {
		return false, fmt.Errorf("rev-parse %s: %w", branch, err)
	}
	tip = strings.TrimSpace(tip)
	// Each line: "<merge-sha> <parent1> <parent2>...". A --no-ff merge of branch
	// into base has base's prior tip as parent1 and branch's tip as parent2, so a
	// match on any parent AFTER the first means a real merge brought branch in.
	out, err := run(dir, "rev-list", "--merges", "--parents", base)
	if err != nil {
		return false, fmt.Errorf("rev-list --merges %s: %w", base, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue // not a merge with >= 2 parents
		}
		for _, parent := range fields[2:] {
			if parent == tip {
				return true, nil
			}
		}
	}
	return false, nil
}

// ChangedFiles returns the names of files changed on the current branch relative
// to base using git's merge-base-aware `base...HEAD` syntax. The returned slice
// is empty when there are no changes; an error means base is missing or
// unreachable.
func ChangedFiles(dir, base string) ([]string, error) {
	out, err := run(dir, "diff", "--name-only", base+"...HEAD")
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s...HEAD: %w", base, err)
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// MergeNoFF performs a --no-ff merge of branch into the current branch.
// --no-verify: the merge commit is machine-generated and must not run the repo's
// human merge/commit hooks (pre-merge-commit / commit-msg, e.g. commitlint).
// Merge integrates ref into the current branch (ff or merge commit, no hooks).
// On conflict/failure the half-merge is aborted so the tree is never left dirty.
// Used by the remote-stint driver to fold a worker machine's pushed branch (its
// ledger checkpoint rides the union merge driver) into the local feature branch.
func Merge(dir, ref string) error {
	out, err := run(dir, "merge", "--no-edit", "--no-verify", ref)
	if err != nil {
		_, _ = run(dir, "merge", "--abort")
		return fmt.Errorf("merge %s failed and was aborted (tree restored clean): %s", ref, strings.TrimSpace(out))
	}
	return nil
}

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

// Push pushes branch to remote. The "--" separator prevents a branch name that
// slips past validation from being parsed as a git option (e.g. "--mirror").
func Push(dir, remote, branch string) error {
	if out, err := run(dir, "push", remote, "--", branch); err != nil {
		return fmt.Errorf("push %s %s: %s", remote, branch, strings.TrimSpace(out))
	}
	return nil
}

// Fetch updates remote-tracking refs for ref from remote (e.g. "origin", "main").
// The "--" separator prevents a ref that slips past validation from being parsed
// as a git option.
func Fetch(dir, remote, ref string) error {
	if out, err := run(dir, "fetch", "--quiet", remote, "--", ref); err != nil {
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
