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

var _ = audit.Record{}
