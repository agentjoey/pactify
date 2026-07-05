package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/agentreg"
	"github.com/agentjoey/pactify/internal/pact"
)

// registerAgents seeds the machine agent registry under an isolated PACTIFY_HOME
// so setup --yes has a roster to staff.
func registerAgents(t *testing.T, kinds ...string) {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	reg, err := agentreg.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kinds {
		if err := reg.Register(k, k, "20260101-000000"); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
}

// setup --yes inits + wires from the registered-agent roster with no prompts,
// and the result must pass validate (init provenance matches the declared lead).
func TestSetupYesInitsAndWiresFromRoster(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gitInitWithCommit(t, dir)
	registerAgents(t, "claude-code", "opencode")
	t.Setenv("PACT_AGENT_ID", "")

	var out bytes.Buffer
	if err := runSetupYes(&out, dir, ""); err != nil {
		t.Fatalf("setup --yes: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".pact")); err != nil {
		t.Fatalf(".pact/ should exist after setup --yes: %v", err)
	}
	if err := pact.Validate(); err != nil {
		t.Fatalf("validate should pass after setup --yes: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Initialized .pact/") || !strings.Contains(s, "seat(s)") {
		t.Fatalf("setup --yes should report init + seat count:\n%s", s)
	}
	if !strings.Contains(s, "pactify run") {
		t.Fatalf("setup --yes should point at the run command next:\n%s", s)
	}
	// Project name defaults to the repo dir basename.
	if !strings.Contains(s, filepath.Base(dir)) {
		t.Errorf("project name should default to repo dir %q:\n%s", filepath.Base(dir), s)
	}
}

func TestSetupYesRefusesInitializedRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gitInitWithCommit(t, dir)
	registerAgents(t, "claude-code")
	t.Setenv("PACT_AGENT_ID", "lead")
	if err := pact.Init("existing", []string{"lead:orchestrator,reviewer:CLAUDE.md"}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runSetupYes(&out, dir, "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("setup --yes on an initialized repo should refuse, got %v", err)
	}
}

func TestSetupYesNoAgentsFailsWithGuidance(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gitInitWithCommit(t, dir)
	registerAgents(t) // none

	var out bytes.Buffer
	err := runSetupYes(&out, dir, "")
	if err == nil || !strings.Contains(err.Error(), "no registered agents") {
		t.Fatalf("setup --yes with no agents should fail with guidance, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".pact")); statErr == nil {
		t.Fatal("setup --yes must fail closed: no .pact/ on empty roster")
	}
}
