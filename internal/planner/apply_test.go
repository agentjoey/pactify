package planner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/planner"
)

func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.CombinedOutput()
	}
	return dir
}

func TestApplyValidPlan(t *testing.T) {
	dir := newGitRepo(t)
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "claude")

	orch := pact.At(dir).As("claude")
	if err := orch.Init("testp", []string{
		"claude:orchestrator,reviewer:CLAUDE.md",
		"alice:worker:A.md",
		"bob:worker:B.md",
	}); err != nil {
		t.Fatal(err)
	}

	os.MkdirAll(filepath.Join(dir, ".pact", "tasks"), 0o755)
	os.WriteFile(filepath.Join(dir, ".pact", "tasks", "T1.md"), []byte("# T1"), 0o644)
	os.WriteFile(filepath.Join(dir, ".pact", "tasks", "T2.md"), []byte("# T2"), 0o644)

	plan := planner.Plan{
		Feature: "F",
		Branch:  "feat/x",
		Tasks: []planner.PlanTask{
			{ID: "T1", Owner: "alice", Reviewer: "bob", Spec: ".pact/tasks/T1.md", Verify: "go test"},
			{ID: "T2", Owner: "bob", Reviewer: "alice", Spec: ".pact/tasks/T2.md", Verify: "go test", Deps: []string{"T1"}},
		},
	}
	roster := []string{"alice", "bob", "claude"}

	assigned, err := planner.Apply(dir, plan, roster)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if assigned != 2 {
		t.Fatalf("assigned = %d, want 2", assigned)
	}

	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}

	var feat *struct{ ID, Branch, Status string }
	for i := range st.Features {
		if st.Features[i].ID == "F" {
			feat = &struct{ ID, Branch, Status string }{
				st.Features[i].ID, st.Features[i].Branch, st.Features[i].Status,
			}
			break
		}
	}
	if feat == nil {
		t.Fatal("feature F not found in state")
	}
	if feat.Branch != "feat/x" {
		t.Fatalf("branch = %q", feat.Branch)
	}

	// Check tasks in state
	found := map[string]bool{}
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			found[tk.ID] = true
			switch tk.ID {
			case "T1":
				if tk.Owner != "alice" || tk.Reviewer != "bob" || tk.Spec != ".pact/tasks/T1.md" || tk.Status != "assigned" {
					t.Fatalf("T1: owner=%s reviewer=%s spec=%s status=%s", tk.Owner, tk.Reviewer, tk.Spec, tk.Status)
				}
				if len(tk.Deps) != 0 {
					t.Fatalf("T1 deps = %v, want empty", tk.Deps)
				}
			case "T2":
				if tk.Owner != "bob" || tk.Reviewer != "alice" || tk.Spec != ".pact/tasks/T2.md" || tk.Status != "assigned" {
					t.Fatalf("T2: owner=%s reviewer=%s spec=%s status=%s", tk.Owner, tk.Reviewer, tk.Spec, tk.Status)
				}
				if len(tk.Deps) != 1 || tk.Deps[0] != "T1" {
					t.Fatalf("T2 deps = %v, want [T1]", tk.Deps)
				}
			}
		}
	}
	if !found["T1"] || !found["T2"] {
		t.Fatalf("tasks T1/T2 not found in state: %v", found)
	}
}

func TestApplySpecMissing(t *testing.T) {
	dir := newGitRepo(t)
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "claude")

	orch := pact.At(dir).As("claude")
	if err := orch.Init("testp", []string{
		"claude:orchestrator,reviewer:CLAUDE.md",
		"alice:worker:A.md",
	}); err != nil {
		t.Fatal(err)
	}

	// Do NOT create the spec file
	plan := planner.Plan{
		Feature: "F",
		Branch:  "feat/x",
		Tasks: []planner.PlanTask{
			{ID: "T1", Owner: "alice", Reviewer: "claude", Spec: ".pact/tasks/T1.md", Verify: "go test"},
		},
	}
	roster := []string{"alice", "claude"}

	assigned, err := planner.Apply(dir, plan, roster)
	if err == nil {
		t.Fatal("expected error for missing spec file")
	}
	if assigned != 0 {
		t.Fatalf("assigned = %d, want 0", assigned)
	}

	// Verify no tasks were assigned: only init event in log
	b, _ := os.ReadFile(filepath.Join(dir, ".pact", "log.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 1 {
		t.Fatalf("log should have only init event, got %d lines", len(lines))
	}
}

func TestApplyInvalidPlan(t *testing.T) {
	dir := newGitRepo(t)
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "claude")

	orch := pact.At(dir).As("claude")
	if err := orch.Init("testp", []string{
		"claude:orchestrator,reviewer:CLAUDE.md",
		"alice:worker:A.md",
	}); err != nil {
		t.Fatal(err)
	}

	os.MkdirAll(filepath.Join(dir, ".pact", "tasks"), 0o755)
	os.WriteFile(filepath.Join(dir, ".pact", "tasks", "T1.md"), []byte("# T1"), 0o644)

	// owner == reviewer: invalid
	plan := planner.Plan{
		Feature: "F",
		Branch:  "feat/x",
		Tasks: []planner.PlanTask{
			{ID: "T1", Owner: "alice", Reviewer: "alice", Spec: ".pact/tasks/T1.md", Verify: "go test"},
		},
	}
	roster := []string{"alice", "claude"}

	assigned, err := planner.Apply(dir, plan, roster)
	if err == nil {
		t.Fatal("expected error for invalid plan (owner==reviewer)")
	}
	if assigned != 0 {
		t.Fatalf("assigned = %d, want 0", assigned)
	}

	// Verify no tasks were assigned
	b, _ := os.ReadFile(filepath.Join(dir, ".pact", "log.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 1 {
		t.Fatalf("log should have only init event, got %d lines", len(lines))
	}
}
