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

var _ = time.Now
