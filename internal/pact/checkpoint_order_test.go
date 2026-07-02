package pact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
)

// Checkpoint must git-commit the work BEFORE appending the checkpoint event: a
// failed commit (here: a concurrent git process holding .git/index.lock) must
// leave NO checkpoint event in the log. The old event-first order left the
// ledger durably claiming awaiting_review-with-evidence over a branch with
// nothing — the phantom-checkpoint class v0.8.1 (#29/#30) paid down. And the
// order buys retry semantics: once the contention clears, a re-run commits the
// still-pending work and appends — self-healing, no operator surgery.
func TestCheckpointCommitFailureAppendsNoEvent(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)

	p := At(repo)
	if err := p.Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Assign("t1", "f", "feat/x", "w", "orch", "", nil); err != nil {
		t.Fatal(err)
	}
	wk := At(repo).As("w")
	if err := wk.Join("w", "worker"); err != nil { // checks out feat/x
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x\n"), 0o644)

	// Simulate a concurrent git process: a pre-existing index.lock makes the
	// `git add -A` inside CommitAll fail (the same contention a real parallel
	// run produces). It does not touch pactify's own flock files.
	idxLock := filepath.Join(repo, ".git", "index.lock")
	if err := os.WriteFile(idxLock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wk.Checkpoint("t1", "tests green"); err == nil {
		t.Fatal("checkpoint must fail when the work commit fails")
	}
	evs, err := event.ReadAll(filepath.Join(repo, ".pact", "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.EventType == "checkpoint" {
			t.Fatalf("checkpoint event landed in the log despite the failed commit: %+v", ev)
		}
	}

	// Contention clears → the same call commits the pending work and appends.
	os.Remove(idxLock)
	if err := wk.Checkpoint("t1", "tests green"); err != nil {
		t.Fatalf("checkpoint retry after contention cleared must succeed: %v", err)
	}
	if log := gitLog(t, repo); !contains(log, "pact t1: checkpoint by w") {
		t.Fatalf("work commit missing or message format changed: %s", log)
	}
}
