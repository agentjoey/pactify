package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// newRenameHarness builds a Server (no watcher goroutine) with one registered
// project whose watch bookkeeping was seeded by AddProject, so RenameProject's
// rekeying can be exercised deterministically in-test.
func newRenameHarness(t *testing.T, name, initial string) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	pact := filepath.Join(root, ".pact")
	os.MkdirAll(pact, 0o755)
	lp := filepath.Join(pact, "log.jsonl")
	os.WriteFile(lp, []byte(initial), 0o644)

	srv := New(nil)
	if err := srv.AddProject(registry.Project{Name: name, Path: root}); err != nil {
		t.Fatal(err)
	}
	return srv, lp
}

// resolveWatchHit mirrors watchLoop's (id,path) resolution: scan watchPaths for
// the entry watching lp and return its id ("" when unwatched).
func resolveWatchHit(srv *Server, lp string) string {
	srv.pmu.RLock()
	defer srv.pmu.RUnlock()
	for id, p := range srv.watchPaths {
		if p == lp {
			return id
		}
	}
	return ""
}

func TestRenameProjectRekeysWatchBookkeeping(t *testing.T) {
	srv, lp := newRenameHarness(t, "old", `{"event_id":"seed"}`+"\n")

	if err := srv.RenameProject("old", "new"); err != nil {
		t.Fatal(err)
	}

	srv.pmu.RLock()
	_, oldWatched := srv.watchPaths["old"]
	_, oldOffset := srv.offsets["old"]
	newLP, newWatched := srv.watchPaths["new"]
	newOff, newOffset := srv.offsets["new"]
	srv.pmu.RUnlock()

	if oldWatched || oldOffset {
		t.Fatalf("old id still in watch bookkeeping: watchPaths=%v offsets=%v", oldWatched, oldOffset)
	}
	if !newWatched || newLP != lp {
		t.Fatalf("new id not watching the log: watched=%v path=%q want %q", newWatched, newLP, lp)
	}
	fi, err := os.Stat(lp)
	if err != nil {
		t.Fatal(err)
	}
	if !newOffset || newOff != fi.Size() {
		t.Fatalf("new offset not reseeded to file size: got %d (present=%v), want %d", newOff, newOffset, fi.Size())
	}
}

func TestRenameProjectBroadcastsUnderNewID(t *testing.T) {
	srv, lp := newRenameHarness(t, "old", `{"event_id":"seed"}`+"\n")

	if err := srv.RenameProject("old", "new"); err != nil {
		t.Fatal(err)
	}

	// Subscribe the way /api/projects/{id}/events does — under the NEW name.
	ch := srv.hub.subscribe("new")
	t.Cleanup(func() { srv.hub.unsubscribe("new", ch) })

	f, _ := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"event_id":"after-rename","event_type":"assign"}` + "\n")
	f.Close()

	hit := resolveWatchHit(srv, lp)
	if hit != "new" {
		t.Fatalf("watchLoop would resolve the append to %q, want %q", hit, "new")
	}
	srv.drainNew(hit, lp)

	select {
	case got := <-ch:
		if !strings.Contains(got, `"event_id":"after-rename"`) {
			t.Fatalf("broadcast under new id carried wrong line: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("append after rename never reached the new id's subscribers")
	}
}
