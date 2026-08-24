package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

// nonTempDir returns an absolute path deliberately outside every temp root. It
// does not need to exist: registration records a path, it never stats one — and
// creating a real directory outside t.TempDir() would litter the machine.
func nonTempDir(t *testing.T) string {
	t.Helper()
	return filepath.FromSlash("/pactify-test-not-a-temp-path/demo-project")
}

// [REGISTRY-2] autoRegister must not write throwaway temp paths into the user's
// real ~/.pactify/projects.json. A `mktemp -d` experiment once landed there for
// good; the dashboard then offered a project whose board was blank, which read as
// a broken product. The skip has to be VISIBLE and tell the user the explicit
// escape hatch, otherwise "my project didn't show up" is the next bug report.
func TestAutoRegisterSkipsTempPathWithActionableNote(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir() // under os.TempDir() by construction

	var w bytes.Buffer
	autoRegister(&w, dir, false)

	note := w.String()
	if note == "" {
		t.Fatal("skipping a temp path must tell the user why; got silence")
	}
	if !strings.Contains(strings.ToLower(note), "temp") {
		t.Errorf("note must say the path is a temp path; got %q", note)
	}
	if !strings.Contains(note, "pactify register") {
		t.Errorf("note must name the explicit escape hatch `pactify register`; got %q", note)
	}

	r, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Projects) != 0 {
		t.Fatalf("temp path must not be auto-registered, got %+v", r.Projects)
	}
}

// A non-temp path is unaffected: auto-register is a wanted feature (agent-started
// projects appear on the dashboard without a manual step) and must keep working.
func TestAutoRegisterStillRegistersRealPath(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := nonTempDir(t)

	var w bytes.Buffer
	autoRegister(&w, dir, false)

	r, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Projects) != 1 || r.Projects[0].Path != dir {
		t.Fatalf("real path must auto-register, got %+v (note: %q)", r.Projects, w.String())
	}
	if !strings.Contains(w.String(), "registered") {
		t.Errorf("registering must be reported; got %q", w.String())
	}
}
