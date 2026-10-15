package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// Preserve RELAY-2's startup contract after removing relay replay: startup
// returns promptly while the background watch loop delivers new local events.
func TestStartWatchersReturnsAndStreams(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	root := t.TempDir()
	pactDir := filepath.Join(root, ".pact")
	if err := os.MkdirAll(pactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lp := filepath.Join(pactDir, "log.jsonl")
	if err := os.WriteFile(lp, []byte("{\"event_id\":\"old\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New([]registry.Project{{Name: "p", Path: root}})
	ch := s.hub.subscribe("p")
	defer s.hub.unsubscribe("p", ch)
	done := make(chan error, 1)
	go func() { done <- s.StartWatchers() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartWatchers blocked HTTP startup")
	}
	defer s.Stop()
	f, err := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"event_id\":\"new\"}\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := recvWithin(t, ch, 2*time.Second); !strings.Contains(got, `"event_id":"new"`) {
		t.Fatalf("expected only the new event, got %q", got)
	}
}
