package pact

import (
	"strings"
	"sync"
	"testing"
)

// Two concurrent Assigns for the same task id must serialize on the ledger
// write lock (withLedgerLock): exactly one appends; the other re-reads the
// post-append state and fails checkAssign's uniqueness check. Without the lock
// both read the same pre-state, both pass the check, both append — and the
// projection silently keeps the later definition (TOCTOU). Run with -race.
func TestAssignConcurrentSameTaskIDExactlyOneWins(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)
	if err := At(repo).Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}

	// Barrier so both goroutines enter the read-check-append window together.
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	features := []string{"fa", "fb"}
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = At(repo).As("orch").Assign("t1", features[i], "feat/"+features[i], "w", "orch", "", nil)
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for i, err := range errs {
		if err == nil {
			wins++
			continue
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("assign %d failed for the wrong reason: %v", i, err)
		}
	}
	if wins != 1 {
		t.Fatalf("want exactly one concurrent assign to win, got %d (errs: %v)", wins, errs)
	}
}
