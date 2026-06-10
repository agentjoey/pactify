package pact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSeatValid(t *testing.T) {
	s, err := ParseSeat("claude-opus:orchestrator,reviewer:CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "claude-opus" || len(s.Roles) != 2 || s.Entry != "CLAUDE.md" {
		t.Fatalf("bad seat: %+v", s)
	}
}

func TestParseSeatRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"a:worker", "a:worker:../x", "a:worker:/etc/x", "a::CLAUDE.md", ":worker:E.md"} {
		if _, err := ParseSeat(bad); err == nil {
			t.Errorf("ParseSeat(%q) should error", bad)
		}
	}
}

func TestBakeEntryManagedBlockPreservesContent(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(entry, []byte("# Mine\nkeep this\n"), 0o644)
	s := Seat{ID: "opencode", Roles: []string{"worker"}, Entry: "AGENTS.md"}
	if err := BakeEntry(dir, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(entry)
	got := string(b)
	if !strings.Contains(got, "keep this") || !strings.Contains(got, "PACT_AGENT_ID=opencode") || !strings.Contains(got, "pact:begin") {
		t.Fatalf("bad entry: %s", got)
	}
	BakeEntry(dir, s)
	b2, _ := os.ReadFile(entry)
	if strings.Count(string(b2), "pact:begin") != 1 {
		t.Fatalf("re-bake duplicated block")
	}
}

func TestBakeEntryReplacesSymlink(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# real\nkeep\n"), 0o644)
	os.Symlink("CLAUDE.md", filepath.Join(dir, "AGENTS.md"))
	if err := BakeEntry(dir, Seat{ID: "opencode", Roles: []string{"worker"}, Entry: "AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	c, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if strings.Contains(string(c), "PACT_AGENT_ID=opencode") {
		t.Fatal("symlink write leaked into CLAUDE.md")
	}
	fi, _ := os.Lstat(filepath.Join(dir, "AGENTS.md"))
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("AGENTS.md should be a real file now")
	}
}

func TestBakeManagedBlockRoundTrips(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(p, []byte("# Mine\n\nkeep me\n"), 0o644)
	if err := BakeManagedBlock(p, "BODY-A"); err != nil {
		t.Fatal(err)
	}
	if err := BakeManagedBlock(p, "BODY-B"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, "keep me") || strings.Contains(s, "BODY-A") || !strings.Contains(s, "BODY-B") {
		t.Fatalf("managed block not replaced cleanly:\n%s", s)
	}
}
