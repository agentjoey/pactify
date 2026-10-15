package testenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// AssertNoAutoMaintenance runs a fixture's real git writes with Trace2 enabled.
// Detached maintenance can still create objects/maintenance.lock after the git
// caller returns, racing TempDir cleanup even in a repository too small for GC.
func AssertNoAutoMaintenance(t *testing.T, write func()) {
	t.Helper()
	trace := filepath.Join(t.TempDir(), "git-trace.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	write()
	f, err := os.Open(trace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for dec.More() {
		var e struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if err := dec.Decode(&e); err != nil {
			t.Fatal(err)
		}
		if e.Event == "child_start" && len(e.Argv) > 1 && (e.Argv[1] == "maintenance" || e.Argv[1] == "gc") {
			t.Fatalf("temporary fixture launched automatic git maintenance: %v", e.Argv)
		}
	}
}
