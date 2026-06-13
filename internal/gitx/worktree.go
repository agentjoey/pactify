package gitx

// AddWorktree creates a git worktree at path checked out on branch, so an agent
// can work that branch in an isolated directory concurrently with others. If
// branch does not exist it is created from base; otherwise the existing branch is
// checked out into the new worktree. A branch can only be checked out in one
// worktree at a time — that is exactly the isolation the parallel driver relies
// on (one feature per worktree).
func AddWorktree(dir, path, branch, base string) error {
	if BranchExists(dir, branch) {
		_, err := run(dir, "worktree", "add", path, branch)
		return err
	}
	_, err := run(dir, "worktree", "add", "-b", branch, path, base)
	return err
}

// RemoveWorktree removes the worktree at path (force, to drop it even with
// untracked/modified files — the driver owns these scratch worktrees and reclaims
// them after a feature ships or escalates).
func RemoveWorktree(dir, path string) error {
	_, err := run(dir, "worktree", "remove", "--force", path)
	return err
}
