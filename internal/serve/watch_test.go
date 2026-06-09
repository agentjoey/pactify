package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// newDrainHarness builds a Server (no watcher goroutine) with offsets seeded to
// the current log size, so drainNew can be exercised deterministically in-test.
func newDrainHarness(t *testing.T, initial string) (*Server, string, chan string) {
	t.Helper()
	root := t.TempDir()
	pact := filepath.Join(root, ".pact")
	os.MkdirAll(pact, 0o755)
	lp := filepath.Join(pact, "log.jsonl")
	os.WriteFile(lp, []byte(initial), 0o644)

	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.offsets = map[string]int64{}
	if fi, err := os.Stat(lp); err == nil {
		srv.offsets["p"] = fi.Size()
	}
	ch := srv.hub.subscribe("p")
	t.Cleanup(func() { srv.hub.unsubscribe("p", ch) })
	return srv, lp, ch
}

func recvWithin(t *testing.T, ch chan string, d time.Duration) string {
	t.Helper()
	select {
	case line := <-ch:
		return line
	case <-time.After(d):
		t.Fatal("timed out waiting for broadcast")
		return ""
	}
}

func TestDrainNewResetsOnTruncation(t *testing.T) {
	// big initial log → large seeded offset
	srv, lp, ch := newDrainHarness(t, strings.Repeat("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", 5))

	// simulate a branch switch swapping log.jsonl for a SHORTER file
	os.WriteFile(lp, []byte(`{"event_id":"A","event_type":"assign"}`+"\n"), 0o644)
	srv.drainNew("p", lp)

	if got := recvWithin(t, ch, time.Second); !strings.Contains(got, `"event_id":"A"`) {
		t.Fatalf("after shrink, expected re-read of new content, got %q", got)
	}
}

func TestDrainNewLeavesPartialLine(t *testing.T) {
	srv, lp, ch := newDrainHarness(t, `{"event_id":"1"}`+"\n")

	// append a PARTIAL line (no trailing newline yet)
	f, _ := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"event_id":"2"`)
	f.Close()
	srv.drainNew("p", lp)
	select {
	case got := <-ch:
		t.Fatalf("partial line must not be broadcast, got %q", got)
	case <-time.After(120 * time.Millisecond):
		// good: nothing broadcast for the partial line
	}

	// complete the line
	f, _ = os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`,"event_type":"join"}` + "\n")
	f.Close()
	srv.drainNew("p", lp)
	if got := recvWithin(t, ch, time.Second); !strings.Contains(got, `"event_id":"2"`) || !strings.Contains(got, "join") {
		t.Fatalf("completed line not broadcast intact, got %q", got)
	}
}
