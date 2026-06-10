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
	// interactive: feed project name, seat, and an empty kind (skip wiring)
	in := strings.NewReader("demo\nclaude-opus\n\n")
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
	in := strings.NewReader("p\ns\nbadkind\n")
	err := runSetup(in, &out, dir, true)
	if err == nil || !strings.Contains(err.Error(), "unknown agent kind") {
		t.Fatalf("expected unknown-kind error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".pact")); statErr == nil {
		t.Fatal("setup must fail closed: no .pact/ should be written on invalid kind")
	}
}
