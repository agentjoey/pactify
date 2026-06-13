package serve

import (
	"bufio"
	"encoding/json"
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

func TestRelayIntegrationFullChain(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "p")

	var mu sync.Mutex
	var bodies []string
	relaySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer relaySrv.Close()

	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetRelay(relaySrv.URL, "")

	if err := srv.StartWatchers(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	time.Sleep(150 * time.Millisecond)

	lp := filepath.Join(root, ".pact", "log.jsonl")
	f, err := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"event_id":"A","event_type":"checkpoint","task_id":"t1"}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	deadline := time.After(5 * time.Second)
	found := false
	for !found {
		select {
		case <-deadline:
			mu.Lock()
			got := append([]string(nil), bodies...)
			mu.Unlock()
			t.Fatalf("relay should have received the event (fsnotify→drainNew→relay), got %d bodies: %v", len(got), got)
		default:
		}
		mu.Lock()
		for _, b := range bodies {
			if strings.Contains(b, `"event_id":"A"`) && strings.Contains(b, `"project":"p"`) {
				found = true
				break
			}
		}
		mu.Unlock()
		if !found {
			time.Sleep(20 * time.Millisecond)
		}
	}

	mu.Lock()
	var last string
	if len(bodies) > 0 {
		last = bodies[len(bodies)-1]
	}
	mu.Unlock()

	var envelope struct {
		Project string          `json:"project"`
		Event   json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal([]byte(last), &envelope); err != nil {
		t.Fatalf("invalid envelope JSON: %v, body=%s", err, last)
	}
	if envelope.Project != "p" {
		t.Fatalf("project = %q, want p", envelope.Project)
	}
	if !strings.Contains(string(envelope.Event), `"event_id":"A"`) {
		t.Fatalf("event field missing event_id: %s", envelope.Event)
	}
	if !strings.Contains(string(envelope.Event), `"event_type":"checkpoint"`) {
		t.Fatalf("event field missing event_type: %s", envelope.Event)
	}
}

func TestRelayIntegrationFailureIsolation(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "p")

	relaySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer relaySrv.Close()

	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetRelay(relaySrv.URL, "")

	if err := srv.StartWatchers(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/api/projects/p/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content-type=%q, want text/event-stream", resp.Header.Get("Content-Type"))
	}

	time.Sleep(150 * time.Millisecond)

	lp := filepath.Join(root, ".pact", "log.jsonl")
	f, err := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"event_id":"B","event_type":"assign"}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	done := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data:") && strings.Contains(line, `"event_id":"B"`) {
				done <- line
				return
			}
		}
	}()

	select {
	case line := <-done:
		if !strings.Contains(line, `"event_type":"assign"`) {
			t.Fatalf("SSE event arrived but wrong content: %s", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SSE event — relay failure must NOT block the SSE stream")
	}
}
