package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/projection"
)

const (
	eventsGoldenFile = "events-golden.jsonl"
	stateGoldenFile  = "state-golden.json"
)

func TestFixtureExport(t *testing.T) {
	td := "testdata"
	eventsPath := filepath.Join(td, eventsGoldenFile)
	statePath := filepath.Join(td, stateGoldenFile)

	evs, err := event.ReadAll(eventsPath)
	if err != nil {
		t.Fatalf("read events golden: %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("events-golden.jsonl is empty")
	}

	st := projection.Project(evs)
	dto := toDTO(st)

	b, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')

	if os.Getenv("UPDATE_GOLDEN") == "1" || os.Getenv("UPDATE_GOLDEN") == "true" {
		if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath, b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", statePath)
		return
	}

	want, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state golden (run with UPDATE_GOLDEN=1 to generate): %v", err)
	}

	if string(b) != string(want) {
		t.Fatalf("StateDTO mismatch.\n--- got ---\n%s\n--- want ---\n%s", string(b), string(want))
	}
}
