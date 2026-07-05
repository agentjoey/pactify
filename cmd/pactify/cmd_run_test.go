package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRequiresFeature(t *testing.T) {
	cmd := newRunCmd()
	cmd.SetArgs([]string{"do a thing"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--feature is required") {
		t.Fatalf("run without --feature should error, got %v", err)
	}
}

func TestRunRequiresActingSeat(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("PACT_AGENT_ID", "")
	cmd := newRunCmd()
	cmd.SetArgs([]string{"do a thing", "--feature", "f"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "acting seat") {
		t.Fatalf("run without PACT_AGENT_ID should error, got %v", err)
	}
}

// planSummary renders the manifest's tasks (ids/owner/reviewer/deps) for the
// preview→confirm gate.
func TestPlanSummaryRendersTasks(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".pact"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
	  "feature": "feat-x",
	  "branch": "feat/x",
	  "seats": [{"id":"w","roles":["worker"]},{"id":"r","roles":["reviewer"]}],
	  "tasks": [
	    {"id":"t1","owner":"w","reviewer":"r","spec":"tasks/t1.md"},
	    {"id":"t2","owner":"w","reviewer":"r","spec":"tasks/t2.md","deps":["t1"]}
	  ]
	}`
	if err := os.WriteFile(filepath.Join(dir, ".pact", "plan-feat-x.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := planSummary(dir, "feat-x")
	if err != nil {
		t.Fatalf("planSummary: %v", err)
	}
	for _, want := range []string{"2 task(s)", "t1", "t2", "owner=w", "reviewer=r", "deps=t1"} {
		if !strings.Contains(s, want) {
			t.Errorf("planSummary missing %q:\n%s", want, s)
		}
	}
}

func TestConfirmYesNo(t *testing.T) {
	cases := map[string]bool{"y\n": true, "yes\n": true, "Y\n": true, "n\n": false, "\n": false, "maybe\n": false}
	for in, want := range cases {
		var out bytes.Buffer
		if got := confirm(strings.NewReader(in), &out, "? "); got != want {
			t.Errorf("confirm(%q) = %v, want %v", in, got, want)
		}
		if !strings.Contains(out.String(), "? ") {
			t.Errorf("confirm should print the prompt for %q", in)
		}
	}
}
