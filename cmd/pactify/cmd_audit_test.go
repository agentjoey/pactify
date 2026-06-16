package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/agentjoey/pactify/internal/audit"
)

func TestAuditHookAppendsAndExitsZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACT_AGENT_ID", "dev")
	t.Setenv("PACT_TASK_ID", "t1")
	t.Setenv("PACT_PROJECT", "demo")

	cmd := newAuditCmd()
	cmd.SetArgs([]string{"hook", "--kind", "claude-code"})
	cmd.SetIn(bytes.NewBufferString(`{"tool_name":"Bash","tool_input":{"command":"go build"},"cwd":"/r","session_id":"s1"}`))
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil { // hook must never error (never block the agent)
		t.Fatalf("hook returned error: %v", err)
	}
	if _, err := os.Stat(home + "/.pactify/audit/demo/" + todayUTC() + ".jsonl"); err != nil {
		t.Fatalf("audit not written: %v", err)
	}
}

func TestAuditHookBadInputStillExitsZero(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	cmd := newAuditCmd()
	cmd.SetArgs([]string{"hook", "--kind", "claude-code"})
	cmd.SetIn(bytes.NewBufferString(`{garbage`))
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hook on bad input must not error: %v", err)
	}
}

func TestAuditLogAndSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	seed(t, audit.Record{Project: "demo", TS: "2026-06-16T01:00:00Z", Seat: "dev", Task: "t1", Tool: "bash", Summary: "go build", Risk: "exec", Decision: "allow"})
	seed(t, audit.Record{Project: "demo", TS: "2026-06-16T02:00:00Z", Seat: "rev", Task: "t1", Tool: "fs.read", Summary: "/x.go", Risk: "read", Decision: "allow"})

	out := runAudit(t, "log", "--project", "demo", "--json")
	if !contains(out, `"tool":"bash"`) || !contains(out, `"tool":"fs.read"`) {
		t.Fatalf("log --json missing records: %s", out)
	}
	sum := runAudit(t, "summary", "--project", "demo")
	if !contains(sum, "total 2") {
		t.Fatalf("summary missing total: %s", sum)
	}
}

func seed(t *testing.T, r audit.Record) {
	t.Helper()
	if err := audit.Append(r); err != nil {
		t.Fatal(err)
	}
}

func runAudit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newAuditCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("audit %v: %v", args, err)
	}
	return buf.String()
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
