package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitInitWithCommit(t *testing.T, dir string) {
	t.Helper()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", dir, "add", "-A").Run()
	exec.Command("git", "-C", dir, "commit", "-q", "-m", "base").Run()
}

func TestSetupNonInteractiveGuides(t *testing.T) {
	var out bytes.Buffer
	// not a TTY (interactive=false) -> must not prompt, must point at primitives, exit nil
	if err := runSetup(strings.NewReader(""), &out, t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "pactify init") || !strings.Contains(s, "pactify agent add") {
		t.Fatalf("non-interactive setup should point at primitives:\n%s", s)
	}
}

func TestSetupInteractivePromptsForSeat(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gitInitWithCommit(t, dir)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	var out bytes.Buffer
	// interactive: feed project name, seat, blank roles (default worker), and an
	// empty kind (skip wiring)
	in := strings.NewReader("demo\nclaude-opus\n\n\n")
	if err := runSetup(in, &out, dir, true); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "PACT_AGENT_ID=claude-opus") {
		t.Fatalf("setup should echo the chosen seat export:\n%s", s)
	}
}

func TestSetupInteractiveRejectsUnknownKindBeforeWrites(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gitInitWithCommit(t, dir)
	t.Setenv("PACT_AGENT_ID", "s")
	var out bytes.Buffer
	in := strings.NewReader("p\ns\n\nbadkind\n")
	err := runSetup(in, &out, dir, true)
	if err == nil || !strings.Contains(err.Error(), "unknown agent kind") {
		t.Fatalf("expected unknown-kind error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".pact")); statErr == nil {
		t.Fatal("setup must fail closed: no .pact/ should be written on invalid kind")
	}
}

// Fresh repo with PACT_AGENT_ID unset: setup must adopt the prompted seat as the
// process identity so pact.Init (which records under PACT_AGENT_ID) succeeds.
func TestSetupAutoSetsAgentIDFromSeat(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gitInitWithCommit(t, dir)
	t.Setenv("PACT_AGENT_ID", "")
	var out bytes.Buffer
	// fresh branch order: project, seat, roles (blank=default), kind (blank=skip)
	in := strings.NewReader("demo\nlead\n\n\n")
	if err := runSetup(in, &out, dir, true); err != nil {
		t.Fatalf("setup should auto-adopt the seat and succeed: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".pact")); err != nil {
		t.Fatalf(".pact/ should exist after fresh setup: %v", err)
	}
	if s := out.String(); !strings.Contains(s, "PACT_AGENT_ID=lead") {
		t.Fatalf("setup should echo the chosen seat export:\n%s", s)
	}
}

// Existing .pact/: setup takes the wire-only branch and must actually write the
// agent config (and report it) rather than just claiming success.
func TestSetupExistingPactWiresAgent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gitInitWithCommit(t, dir)
	t.Setenv("PACT_AGENT_ID", "")
	// First create .pact/ via a fresh setup run (project, seat, roles, kind=skip).
	var first bytes.Buffer
	if err := runSetup(strings.NewReader("demo\nlead\n\n\n"), &first, dir, true); err != nil {
		t.Fatalf("scaffold setup failed: %v\n%s", err, first.String())
	}
	// Now .pact/ exists -> wire-only branch. Order: kind, seat, roles.
	var out bytes.Buffer
	in := strings.NewReader("opencode\nlead\nworker\n")
	if err := runSetup(in, &out, dir, true); err != nil {
		t.Fatalf("wire-only setup failed: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "opencode.json")); err != nil {
		t.Fatalf("opencode.json should be written: %v", err)
	}
	if s := out.String(); !strings.Contains(s, "wrote opencode.json") {
		t.Fatalf("setup should report the written config:\n%s", s)
	}
}
