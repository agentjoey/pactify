package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/audit"
	"github.com/agentjoey/pactify/internal/sarif"
)

func TestRecordToFindingMapping(t *testing.T) {
	cases := []struct {
		name     string
		rec      audit.Record
		wantRule string
		wantLvl  string
		wantMsg  string
	}{
		{
			name:     "exec maps to warning",
			rec:      audit.Record{Risk: "exec", Tool: "bash", Summary: "go build", Seat: "dev", Task: "t1", Project: "demo", TS: "2026-06-16T01:00:00Z"},
			wantRule: "pact.audit.exec",
			wantLvl:  "warning",
			wantMsg:  "go build",
		},
		{
			name:     "mcp maps to warning",
			rec:      audit.Record{Risk: "mcp", Tool: "mcp.server", Summary: "call", Seat: "dev"},
			wantRule: "pact.audit.mcp",
			wantLvl:  "warning",
			wantMsg:  "call",
		},
		{
			name:     "read maps to note",
			rec:      audit.Record{Risk: "read", Tool: "fs.read", Summary: "/x.go"},
			wantRule: "pact.audit.read",
			wantLvl:  "note",
			wantMsg:  "/x.go",
		},
		{
			name:     "write maps to note",
			rec:      audit.Record{Risk: "write", Tool: "Edit", Summary: "edit x"},
			wantRule: "pact.audit.write",
			wantLvl:  "note",
			wantMsg:  "edit x",
		},
		{
			name:     "empty risk becomes unknown",
			rec:      audit.Record{Risk: "", Tool: "Tool", Summary: "summary"},
			wantRule: "pact.audit.unknown",
			wantLvl:  "note",
			wantMsg:  "summary",
		},
		{
			name:     "empty summary falls back to tool",
			rec:      audit.Record{Risk: "exec", Tool: "bash", Summary: ""},
			wantRule: "pact.audit.exec",
			wantLvl:  "warning",
			wantMsg:  "bash",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := recordToFinding(tc.rec)
			if f.RuleID != tc.wantRule {
				t.Fatalf("ruleId = %q, want %q", f.RuleID, tc.wantRule)
			}
			if f.Level != tc.wantLvl {
				t.Fatalf("level = %q, want %q", f.Level, tc.wantLvl)
			}
			if f.Message != tc.wantMsg {
				t.Fatalf("message = %q, want %q", f.Message, tc.wantMsg)
			}
		})
	}
}

func TestAuditSarifStdout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	seed(t, audit.Record{Project: "demo", TS: "2026-06-16T01:00:00Z", Seat: "dev", Task: "t1", Tool: "bash", Summary: "go build", Risk: "exec", Decision: "allow"})
	seed(t, audit.Record{Project: "demo", TS: "2026-06-16T02:00:00Z", Seat: "rev", Task: "t1", Tool: "fs.read", Summary: "/x.go", Risk: "read", Decision: "allow"})

	out := runAudit(t, "sarif", "--project", "demo")

	var log sarif.Log
	if err := json.Unmarshal([]byte(out), &log); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(log.Runs))
	}
	results := log.Runs[0].Results
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}

	levels := map[string]int{}
	for _, r := range results {
		levels[r.Level]++
	}
	if levels["warning"] != 1 || levels["note"] != 1 {
		t.Fatalf("level mapping wrong: %v", levels)
	}
}

func TestAuditSarifWritesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	seed(t, audit.Record{Project: "demo", TS: "2026-06-16T03:00:00Z", Seat: "dev", Task: "t2", Tool: "bash", Summary: "go test", Risk: "exec", Decision: "allow"})

	outFile := filepath.Join(home, "out.sarif")
	cmdOut := runAudit(t, "sarif", "--project", "demo", "--out", outFile)

	if !contains(cmdOut, "wrote 1 results to") || !contains(cmdOut, outFile) {
		t.Fatalf("unexpected output: %s", cmdOut)
	}

	buf, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	var log sarif.Log
	if err := json.Unmarshal(buf, &log); err != nil {
		t.Fatalf("output file is not valid JSON: %v", err)
	}
	if len(log.Runs[0].Results) != 1 {
		t.Fatalf("results = %d, want 1", len(log.Runs[0].Results))
	}
}

func TestAuditSarifEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)

	out := runAudit(t, "sarif", "--project", "missing")
	var log sarif.Log
	if err := json.Unmarshal([]byte(out), &log); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
	}
	if len(log.Runs[0].Results) != 0 {
		t.Fatalf("results = %d, want 0", len(log.Runs[0].Results))
	}
}

func TestAuditSarifCommandDirectly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	seed(t, audit.Record{Project: "demo", TS: "2026-06-16T04:00:00Z", Seat: "dev", Task: "t3", Tool: "mcp.tool", Summary: "mcp call", Risk: "mcp", Decision: "allow"})

	cmd := newAuditCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"sarif", "--project", "demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sarif command failed: %v", err)
	}

	var log sarif.Log
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if log.Runs[0].Tool.Driver.Name != "pactify" {
		t.Fatalf("driver.name = %q, want pactify", log.Runs[0].Tool.Driver.Name)
	}
	if len(log.Runs[0].Results) != 1 {
		t.Fatalf("results = %d, want 1", len(log.Runs[0].Results))
	}
	if log.Runs[0].Results[0].Level != "warning" {
		t.Fatalf("level = %q, want warning", log.Runs[0].Results[0].Level)
	}
}
