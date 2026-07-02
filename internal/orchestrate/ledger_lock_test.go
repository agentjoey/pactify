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

func testEvent(id string) string {
	return `{"event_id":"` + id + `","ts":"2026-07-02T00:00:00Z","agent_id":"w","role":"worker","event_type":"checkpoint","task_id":"ta","feature":"fa","payload":{}}` + "\n"
}

// writeLedger must hold the cross-process log lock (pactify-log.lock in the git
// common dir) across its whole read-merge-write: an event appended to the
// destination log by a concurrent lock-honouring writer while writeLedger is
// pending must survive the merge — without the lock, the rewrite silently
// dropped it (TOCTOU).
func TestWriteLedger_ConcurrentAppendSurvives(t *testing.T) {
	dir := newProject(t)
	logPath := filepath.Join(dir, ".pact", "log.jsonl")

	lockPath, err := gitx.GitPath(dir, "pactify-log.lock")
	if err != nil {
		t.Fatal(err)
	}
	release, err := lockx.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		writeLedger(dir, map[string][]byte{"log.jsonl": []byte(testEvent("sb1"))})
		close(done)
	}()

	// writeLedger must be queued on the lock; give it time to reach the acquire,
	// then append "under its nose" while still holding the lock.
	select {
	case <-done:
		t.Fatal("writeLedger completed while the log lock was held elsewhere")
	case <-time.After(200 * time.Millisecond):
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(testEvent("hum1")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	release()
	<-done

	merged, _ := os.ReadFile(logPath)
	for _, id := range []string{"hum1", "sb1"} {
		if !strings.Contains(string(merged), `"event_id":"`+id+`"`) {
			t.Errorf("event %s missing after copy-back under concurrency:\n%s", id, merged)
		}
	}
}

// A busy log lock must make writeLedger SKIP the cycle after the timeout — the
// copy-back is best-effort observation and must never stall the driver loop.
func TestWriteLedger_LockTimeoutSkipsCycle(t *testing.T) {
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

	start := time.Now()
	writeLedger(dir, map[string][]byte{"log.jsonl": []byte(testEvent("skip1"))})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("writeLedger blocked %v on a busy lock; want a timeout skip", elapsed)
	}
	if after, _ := os.ReadFile(logPath); !bytes.Equal(after, before) {
		t.Error("writeLedger wrote the log despite failing to acquire the lock")
	}
}

// The "is .pact ignored in the runtime dir" probe is resolved once per run when
// the memo is wired (as RunSandbox does): flipping the ignore state mid-run must
// not change mirrorLedger's behavior — proving no per-iteration `git
// check-ignore` spawn.
func TestMirrorLedger_IgnoreProbeMemoizedPerRun(t *testing.T) {
	main := newProject(t)
	gi := filepath.Join(main, ".gitignore")
	if err := os.WriteFile(gi, []byte(".pact/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sandbox := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sandbox, ".pact"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed, _ := os.ReadFile(filepath.Join(main, ".pact", "log.jsonl"))
	sbLog := filepath.Join(sandbox, ".pact", "log.jsonl")
	if err := os.WriteFile(sbLog, append(append([]byte{}, seed...), testEvent("m1")...), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{Dir: sandbox, RuntimeDir: main, pactIgnoredMemo: &ignoredMemo{}}
	opts.mirrorLedger() // first mirror resolves the probe: .pact IS ignored

	// Flip the ignore state: a live re-probe would now say "tracked" and skip.
	if err := os.Remove(gi); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sbLog, append(append(append([]byte{}, seed...), testEvent("m1")...), testEvent("m2")...), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.mirrorLedger()

	merged, _ := os.ReadFile(filepath.Join(main, ".pact", "log.jsonl"))
	if !strings.Contains(string(merged), `"event_id":"m2"`) {
		t.Fatalf("second mirror skipped — the ignore probe was re-run mid-loop instead of memoized:\n%s", merged)
	}
}
