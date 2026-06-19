package serve

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// The stream must emit a flushed frame the instant it opens — before any pact
// event — so a buffering reverse proxy (Cloudflare Tunnel) forwards the response
// head and the client's EventSource fires onopen (dashboard goes "live"). Without
// this a quiet project shows "offline" forever behind such a proxy.
func TestSSEEmitsInitialFrameImmediately(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	srv := New([]registry.Project{{Name: "pactify", Path: root}})
	if err := srv.StartWatchers(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/projects/pactify/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering=%q, want no", got)
	}

	// Read the first bytes WITHOUT appending any event; they must arrive promptly.
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := resp.Body.Read(buf)
		got <- string(buf[:n])
	}()
	select {
	case frame := <-got:
		if !strings.Contains(frame, "retry:") && !strings.HasPrefix(strings.TrimSpace(frame), ":") {
			t.Fatalf("initial frame = %q, want a flushed comment/retry frame", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no initial frame before any event — onopen would stall behind a proxy")
	}
}

func TestSSEStreamsAppendedEvents(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	srv := New([]registry.Project{{Name: "pactify", Path: root}})
	if err := srv.StartWatchers(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/projects/pactify/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content-type=%q", resp.Header.Get("Content-Type"))
	}

	time.Sleep(50 * time.Millisecond)
	f, _ := os.OpenFile(filepath.Join(root, ".pact", "log.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"event_id":"2","ts":"2026-01-01T00:02:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"assign","task_id":"T1","feature":"F","payload":{"owner":"opencode","reviewer":"claude-opus","branch":"b","spec":"s"}}` + "\n")
	f.Close()

	done := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data:") && strings.Contains(sc.Text(), `"event_type":"assign"`) {
				done <- sc.Text()
				return
			}
		}
	}()
	select {
	case line := <-done:
		if !strings.Contains(line, "T1") {
			t.Fatalf("unexpected SSE line: %s", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

// A client opening Live on an already-active project must see recent history,
// not an empty stream — the hub only carries events appended after subscribe, so
// handleEvents replays the log tail on connect. Here the event predates any
// subscriber (the watcher has already advanced past it), so it can only arrive
// via that backfill.
func TestSSEBackfillsHistoryOnConnect(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	f, _ := os.OpenFile(filepath.Join(root, ".pact", "log.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"event_id":"hist1","ts":"2026-01-01T00:05:00Z","agent_id":"opencode","role":"worker","event_type":"checkpoint","task_id":"T9","feature":"F","payload":{}}` + "\n")
	f.Close()

	srv := New([]registry.Project{{Name: "pactify", Path: root}})
	if err := srv.StartWatchers(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/projects/pactify/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// No event is appended after connect: the pre-existing one must be replayed.
	done := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data:") && strings.Contains(sc.Text(), `"task_id":"T9"`) {
				done <- sc.Text()
				return
			}
		}
	}()
	select {
	case line := <-done:
		if !strings.Contains(line, "hist1") {
			t.Fatalf("unexpected backfill line: %s", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out — historical event was not backfilled on connect")
	}
}
