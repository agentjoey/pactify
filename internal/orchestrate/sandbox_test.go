package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// A --sandbox run drives the feature to shipped in an isolated worktree, copies the
// advanced ledger back to the main .pact (回灌), and cleans up the worktree — all
// without leaving the main working tree on the scratch park branch.
func TestRunSandbox_ShipsAndCleansUp(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	before := branchOf(t, dir)

	opts := Options{
		Dir:          dir,
		Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
		Run:          parFakeRunner{t: t},
		Exec:         &okExec{},
		Notify:       StdoutNotifier{},
		Now:          func() string { return "20260621-000000" },
		SeatKind:     func(string) string { return "claude-code" },
		Orchestrator: "orch",
	}
	if err := RunSandbox(context.Background(), opts); err != nil {
		t.Fatalf("RunSandbox: %v", err)
	}

	// 回灌: the main .pact ledger now reflects the shipped feature.
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	shipped := false
	for _, f := range st.Features {
		if f.ID == "fa" && f.Status == "shipped" {
			shipped = true
		}
	}
	if !shipped {
		t.Fatalf("feature fa not shipped in main .pact after sandbox run: %+v", st.Features)
	}
	// the scratch worktree is gone and the main tree is back on its original branch.
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "sandbox")); !os.IsNotExist(err) {
		t.Error("sandbox worktree was not removed")
	}
	if after := branchOf(t, dir); after != before {
		t.Errorf("main tree left on %q, want original %q (park not restored)", after, before)
	}
}

func branchOf(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if i := len("ref: refs/heads/"); len(s) > i {
		return s[i : len(s)-1]
	}
	return s
}
