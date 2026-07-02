package pact

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// After merge, the STATE.yml committed at HEAD must match the working tree
// (feature shipped). Regression: the merge commit previously captured the feature
// branch's pre-merge STATE (in_progress/accepted) because the merge event was
// appended + STATE re-rendered into the working tree AFTER the git merge commit
// and never committed — leaving HEAD's STATE lagging behind the working tree.
func TestMergeHEADStateMatchesWorktree(t *testing.T) {
	newRepo(t)
	toAwaiting(t)
	// toAwaiting checkpoints in-place without joining; create the declared feature
	// branch so the merge integrates it (bug-1-fix puts the worker on this branch in
	// real runs, so a declared branch always exists by merge time).
	exec.Command("git", "branch", "feat/x").Run()
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Accept("t1"); err != nil {
		t.Fatal(err)
	}
	if err := Merge("f"); err != nil {
		t.Fatal(err)
	}

	wt, _ := os.ReadFile(".pact/STATE.yml")
	head, err := exec.Command("git", "show", "HEAD:.pact/STATE.yml").Output()
	if err != nil {
		t.Fatalf("git show HEAD:.pact/STATE.yml: %v", err)
	}
	if !strings.Contains(string(head), "status: shipped") {
		t.Fatalf("HEAD STATE lags (not shipped):\n%s", head)
	}
	if strings.TrimSpace(string(head)) != strings.TrimSpace(string(wt)) {
		t.Fatalf("HEAD STATE != worktree\nHEAD:\n%s\nWT:\n%s", head, wt)
	}
}
