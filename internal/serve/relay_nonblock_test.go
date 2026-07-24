package serve

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// RELAY-2 (2026-07-23): a down/slow relay must not block StartWatchers — and
// thus Run's ListenAndServe — or the LOCAL dashboard goes dark. A stalled relay
// (1-slot queue, no sender draining it) makes replayProject's enqueueBlocking
// wait forever; StartWatchers must still return promptly because replay now runs
// off the startup path.
func TestStartWatchers_NotBlockedByStalledRelay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	dir := newAuthorRepo(t)

	// Append enough events to overflow a 1-slot queue so enqueueBlocking blocks.
	lp := filepath.Join(dir, ".pact", "log.jsonl")
	f, _ := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	for i := 0; i < 5; i++ {
		f.WriteString(`{"event_id":"e` + string(rune('a'+i)) + `","ts":"2026-07-02T00:00:00Z","agent_id":"w","role":"worker","event_type":"checkpoint","task_id":"t","feature":"f","payload":{}}` + "\n")
	}
	f.Close()

	s := New(nil)
	s.AddProject(registry.Project{Name: "p", Path: dir})
	// A minimal relay whose queue holds one item and whose sender never runs, so
	// the second enqueueBlocking blocks indefinitely.
	s.relay = &relay{
		endpoint:    "http://127.0.0.1:0/v1/pact/ingest",
		seqs:        map[string]int64{},
		projectKeys: map[string][]byte{},
		queue:       make(chan pactMsg, 1),
		stopCh:      make(chan struct{}),
		done:        make(chan struct{}),
		wmPaths:     map[string]string{},
		wmMarks:     map[string]int64{},
		wmDirty:     map[string]bool{},
	}
	// Teardown WITHOUT relay.stop() (it blocks on r.done, which the never-started
	// sender never closes): just unblock the replay goroutine and close the
	// fsnotify watcher.
	t.Cleanup(func() {
		close(s.relay.stopCh)
		if s.watcher != nil {
			_ = s.watcher.Close()
		}
	})

	done := make(chan error, 1)
	go func() { done <- s.StartWatchers() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StartWatchers: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartWatchers blocked on a stalled relay — HTTP would never start (RELAY-2)")
	}
}
