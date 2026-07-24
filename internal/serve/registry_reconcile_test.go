package serve

import (
	"os"
	"path/filepath"
	"testing"

	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// reconcileRegistry brings the live watched set in line with the on-disk
// registry file — so a CLI `pactify register` (or init/orchestrate
// auto-register), which only writes the file, becomes visible on a running
// serve without a restart (backlog B/C, 2026-07-23 urbanbricks/tradelinks).
func TestReconcileRegistry_AddsFileEntryToLiveSet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	dir := newAuthorRepo(t) // a valid .pact/ git repo

	s := New(nil) // serve boots with an empty live set
	// Simulate `pactify register`: write the file directly, bypassing serve.
	r, _ := registry.Load()
	if err := r.Add("proj", dir, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}

	// Before reconcile the live set is empty.
	if _, ok := s.projects["proj"]; ok {
		t.Fatal("precondition: project should not be in the live set yet")
	}
	s.reconcileRegistry()
	if _, ok := s.projects["proj"]; !ok {
		t.Fatalf("reconcile must add the file entry to the live set; have %v", s.projects)
	}
}

// A project removed from the file (CLI unregister) is dropped from the live set.
func TestReconcileRegistry_RemovesGoneEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	dir := newAuthorRepo(t)

	s := New(nil)
	r, _ := registry.Load()
	r.Add("proj", dir, "")
	r.Save()
	s.reconcileRegistry()
	if _, ok := s.projects["proj"]; !ok {
		t.Fatal("setup: project should be present after reconcile")
	}

	// Simulate `pactify unregister`: rewrite the file empty.
	empty := registry.Registry{}
	if err := os.WriteFile(filepath.Join(home, "projects.json"), mustJSON(t, empty), 0o644); err != nil {
		t.Fatal(err)
	}
	s.reconcileRegistry()
	if _, ok := s.projects["proj"]; ok {
		t.Fatal("reconcile must drop an entry removed from the file")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// C: POST /api/registry for a path already in the FILE but not yet watched (a
// CLI/auto register wrote it while serve was up) must reconcile and return 200,
// not the old 409-without-a-watch that left it invisible until restart.
func TestRegistryAdd_ReconcilesFileDupInsteadOf409(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := newAuthorRepo(t)
	s := New(nil) // no StartWatchers → no async watcher; exercise the handler path
	s.SetSeat("test")
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// Simulate a CLI register that only touched the file (name = basename slug,
	// the same name the API derives from an empty-name POST — so reg.Add collides
	// on name, the real urbanbricks/tradelinks case).
	r, _ := registry.Load()
	r.Add("", dir, "")
	r.Save()
	name := r.Projects[len(r.Projects)-1].Name

	body, _ := json.Marshal(map[string]string{"path": dir})
	resp, err := http.Post(ts.URL+"/api/registry", "application/json", bytesReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 (reconciled), got %d", resp.StatusCode)
	}
	if _, ok := s.projects[name]; !ok {
		t.Fatalf("handler must have reconciled the file entry %q into the live set", name)
	}
}

// End-to-end: with the watcher running, a CLI register (file write only) becomes
// visible on the live serve without a restart (backlog B).
func TestRegistryFileWatch_LivePickup(t *testing.T) {
	s, _ := registryServer(t) // StartWatchers watches the registry dir
	dir := newAuthorRepo(t)

	r, _ := registry.Load()
	r.Add("live", dir, "")
	r.Save()

	// The fsnotify event + reconcile are async; wait briefly.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.pmu.RLock()
		_, ok := s.projects["live"]
		s.pmu.RUnlock()
		if ok {
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("registry file write was not picked up live by the watcher within 3s")
}

func bytesReader(b []byte) *strings.Reader { return strings.NewReader(string(b)) }
