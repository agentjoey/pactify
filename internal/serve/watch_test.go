package serve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	srv.watchPaths["p"] = lp
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

func TestDrainNewEnqueuesToRelay(t *testing.T) {
	srv, lp, ch := newDrainHarness(t, "")

	var mu sync.Mutex
	var bodies []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	seedRelaySession(t, ts.URL)
	srv.SetRelay(ts.URL, "")

	f, _ := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"event_id":"A","event_type":"assign"}` + "\n")
	f.Close()
	srv.drainNew("p", lp)

	if got := recvWithin(t, ch, time.Second); !strings.Contains(got, `"event_id":"A"`) {
		t.Fatalf("broadcast should still work with relay on, got %q", got)
	}
	deadline := time.After(2 * time.Second)
	found := false
	for !found {
		select {
		case <-deadline:
			mu.Lock()
			gotBodies := bodies
			mu.Unlock()
			t.Fatalf("relay should have received the event, got bodies: %v", gotBodies)
		default:
		}
		mu.Lock()
		gotBodies := bodies
		mu.Unlock()
		for _, b := range gotBodies {
			// The event body is now encrypted; assert on the cleartext header.
			if strings.Contains(b, `"eventType":"assign"`) && strings.Contains(b, `"projectId":"acct1:p"`) {
				found = true
				break
			}
		}
		if !found {
			time.Sleep(20 * time.Millisecond)
		}
	}
	srv.relay.stop()
}
