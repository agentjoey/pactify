package audit

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestAppendAndStorePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	r := Record{
		TS: "2026-06-16T05:10:00Z", Project: "demo", Repo: "/x", Seat: "dev",
		Task: "t1", Kind: "opencode", Session: "ses_1", Tool: "bash",
		Summary: "go test ./...", Risk: "exec", Decision: "allow",
	}
	if err := Append(r); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// File at ~/.pactify/audit/demo/2026-06-16.jsonl, one JSON line.
	want := home + "/.pactify/audit/demo/2026-06-16.jsonl"
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if !strings.Contains(string(b), `"tool":"bash"`) || !strings.HasSuffix(string(b), "\n") {
		t.Fatalf("store line = %q", b)
	}
}

func TestAppendDateBucketsByUTC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T23:59:00Z", Tool: "a"})
	mustAppend(t, Record{Project: "p", TS: "2026-06-17T00:01:00Z", Tool: "b"})
	for _, d := range []string{"2026-06-16", "2026-06-17"} {
		if _, err := os.Stat(home + "/.pactify/audit/p/" + d + ".jsonl"); err != nil {
			t.Errorf("missing day file %s: %v", d, err)
		}
	}
}

func mustAppend(t *testing.T, r Record) {
	t.Helper()
	if err := Append(r); err != nil {
		t.Fatal(err)
	}
}

func TestQueryFiltersAndOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T01:00:00Z", Seat: "dev", Task: "t1", Tool: "bash", Risk: "exec"})
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T02:00:00Z", Seat: "rev", Task: "t1", Tool: "fs.read", Risk: "read"})
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T03:00:00Z", Seat: "dev", Task: "t2", Tool: "fs.write", Risk: "write"})

	// Filter by seat=dev → 2 records, newest-first.
	got, err := Query(Filter{Project: "p", Seat: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Task != "t2" || got[1].Task != "t1" {
		t.Fatalf("seat filter = %+v", got)
	}
	// Filter by task=t1 → 2 records.
	if got, _ := Query(Filter{Project: "p", Task: "t1"}); len(got) != 2 {
		t.Fatalf("task filter len = %d, want 2", len(got))
	}
	// Filter by risk=write → 1.
	if got, _ := Query(Filter{Project: "p", Risk: "write"}); len(got) != 1 {
		t.Fatalf("risk filter len = %d, want 1", len(got))
	}
}

func TestQuerySkipsTornLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T01:00:00Z", Tool: "bash"})
	// Append a torn/garbage line directly.
	p := home + "/.pactify/audit/p/2026-06-16.jsonl"
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("{not json\n")
	f.Close()
	got, err := Query(Filter{Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 valid record (torn line skipped), got %d", len(got))
	}
}

func TestSummarize(t *testing.T) {
	rs := []Record{
		{Tool: "bash", Risk: "exec", Seat: "dev"},
		{Tool: "bash", Risk: "exec", Seat: "dev"},
		{Tool: "fs.write", Risk: "write", Seat: "rev"},
	}
	s := Summarize(rs)
	if s.Total != 3 || s.ByRisk["exec"] != 2 || s.BySeat["dev"] != 2 || s.ByTool["bash"] != 2 {
		t.Fatalf("summary = %+v", s)
	}
}

var _ = time.Now

func TestPruneRemovesOldDayFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	// An old day-file (well past 30d) and a recent one (today).
	mustAppend(t, Record{Project: "p", TS: "2026-01-01T00:00:00Z", Tool: "bash"})
	today := time.Now().UTC().Format("2006-01-02")
	mustAppend(t, Record{Project: "p", TS: today + "T00:00:00Z", Tool: "bash"})
	n, err := Prune(30 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d, want 1 (only the old day-file)", n)
	}
	if _, err := os.Stat(home + "/.pactify/audit/p/2026-01-01.jsonl"); !os.IsNotExist(err) {
		t.Error("old day-file should be gone")
	}
	if _, err := os.Stat(home + "/.pactify/audit/p/" + today + ".jsonl"); err != nil {
		t.Error("today's day-file should remain")
	}
}
