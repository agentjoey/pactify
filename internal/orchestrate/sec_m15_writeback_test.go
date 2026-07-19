package orchestrate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/lockx"
)

// Correctness regression — review finding M15.
//
// The sandbox epilogue回灌 (writeLedger) is the ONLY authoritative write-back of a
// run's results into the main ledger — for a repo that git-tracks .pact the mid-run
// mirror is a no-op. It used to `return` silently on a lock timeout, dropping the
// whole run's checkpoint/accept/merge events while git had already merged. It must
// now surface the failure and preserve the events for recovery.

// writeLedger returns an error (no longer silent) when it cannot acquire the log
// lock, and does not write.
func TestSEC_M15_WriteLedgerReturnsErrorOnLockTimeout(t *testing.T) {
	old := logCopybackLockTimeout
	logCopybackLockTimeout = 120 * time.Millisecond
	defer func() { logCopybackLockTimeout = old }()

	dir := newProject(t)
	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	before, _ := os.ReadFile(logPath)

	lockPath, err := gitx.GitPath(dir, "pactify-log.lock")
	if err != nil {
		t.Fatal(err)
	}
	release, err := lockx.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	werr := writeLedger(dir, map[string][]byte{"log.jsonl": []byte(testEvent("m15a"))})
	if werr == nil {
		t.Fatal("writeLedger must return an error when it cannot acquire the lock (M15: no silent drop)")
	}
	if after, _ := os.ReadFile(logPath); !bytes.Equal(after, before) {
		t.Error("writeLedger wrote the log despite failing to acquire the lock")
	}
}

// preserveUnmergedLedger drops the un-mergeable snapshot to a recovery file.
func TestSEC_M15_PreserveUnmergedLedgerPersistsSnapshot(t *testing.T) {
	dir := newProject(t)
	path, err := preserveUnmergedLedger(dir, map[string][]byte{"log.jsonl": []byte(testEvent("m15b"))})
	if err != nil {
		t.Fatalf("preserve: %v", err)
	}
	if path == "" {
		t.Fatal("no recovery path returned")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recovery file: %v", err)
	}
	if !bytes.Contains(b, []byte("m15b")) {
		t.Fatalf("recovery file missing the run's events: %q", b)
	}
	if !strings.Contains(path, filepath.Join(".pact", "orchestrate", "unmerged-ledger-")) {
		t.Fatalf("recovery file in unexpected location: %s", path)
	}
}

// End-to-end: when the epilogue write-back fails, the run PRESERVES its events to a
// recovery file and FAILS loudly instead of silently losing them.
func TestSEC_M15_EpiloguePreservesAndFailsWhenWritebackFails(t *testing.T) {
	old := logCopybackLockTimeout
	logCopybackLockTimeout = 80 * time.Millisecond
	defer func() { logCopybackLockTimeout = old }()

	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	// Hold the MAIN dir's copyback log lock for the whole run. The run itself runs in
	// the sandbox worktree (a different per-worktree lock) so it proceeds; only the
	// epilogue回灌 (which locks the main dir) is forced to fail — reproducing the M15
	// crash window without an actual crash.
	lockPath, err := gitx.GitPath(dir, "pactify-log.lock")
	if err != nil {
		t.Fatal(err)
	}
	release, err := lockx.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	notify := &recNotify{}
	opts := sandboxOpts(t, dir)
	opts.Notify = notify

	err = RunSandbox(context.Background(), opts)
	if err == nil {
		t.Fatal("RunSandbox must fail when the authoritative write-back fails (M15: was a silent drop)")
	}
	if !strings.Contains(err.Error(), "write this run's ledger back") {
		t.Fatalf("run error should name the failed write-back, got: %v", err)
	}

	// The run's events are preserved for reconciliation, not lost.
	matches, _ := filepath.Glob(filepath.Join(dir, ".pact", "orchestrate", "unmerged-ledger-*.jsonl"))
	if len(matches) == 0 {
		t.Fatal("no unmerged-ledger recovery file — the run's events were lost")
	}
	if b, _ := os.ReadFile(matches[0]); len(b) == 0 {
		t.Fatal("recovery file is empty")
	}
	// And it was surfaced loudly.
	loud := false
	for _, m := range notify.msgs {
		if strings.Contains(m, "FAILED to write this run's ledger") {
			loud = true
		}
	}
	if !loud {
		t.Errorf("expected a loud write-back-failure notification, got %v", notify.msgs)
	}
}
