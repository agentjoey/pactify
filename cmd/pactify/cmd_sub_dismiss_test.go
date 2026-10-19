package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubDismissCLI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Create("handled", sub.Request{Harness: "codex", Access: "read-only"})
	if err != nil {
		t.Fatal(err)
	}
	r := sub.Result{JobID: st.ID, Status: sub.StatusFailed, FinalText: "retained answer", Warnings: []string{"review this"}}
	if err := store.Finish(st.ID, &r, "failed", time.Now()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		out, code := runCLI(t, bin, t.TempDir(), "", "sub", "dismiss", st.ID)
		if code != 0 || out != "dismissed handled\n" {
			t.Fatalf("dismiss: %d %s", code, out)
		}
	}
	out, code := runCLI(t, bin, t.TempDir(), "", "sub", "status", st.ID, "--json")
	var row sub.JobSummary
	if err := json.Unmarshal([]byte(out), &row); err != nil {
		t.Fatalf("status: %d %s: %v", code, out, err)
	}
	if code != 0 || row.Status != sub.StatusFailed || len(row.Attention) != 0 || row.DismissedAt.IsZero() || row.WarningCount != 1 {
		t.Fatalf("dismissed status: %d %s", code, out)
	}
	out, code = runCLI(t, bin, t.TempDir(), "", "sub", "result", st.ID, "--json")
	var result sub.Result
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.FinalText != r.FinalText || result.Status != sub.StatusFailed || len(result.Warnings) != 1 {
		t.Fatalf("result: %d %s: %v", code, out, err)
	}
	out, code = runCLI(t, bin, t.TempDir(), "", "sub", "dismiss", "unknown")
	if code == 0 || !strings.Contains(out, `unknown job "unknown"`) {
		t.Fatalf("unknown job: %d %s", code, out)
	}
	for _, args := range [][]string{{"sub", "dismiss"}, {"sub", "dismiss", st.ID, "extra"}} {
		out, code := runCLI(t, bin, t.TempDir(), "", args...)
		if code != 2 {
			t.Fatalf("argument count: %d %s", code, out)
		}
	}
}
