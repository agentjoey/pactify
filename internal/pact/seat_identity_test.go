package pact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// engine.agentID resolves through the identity chain rooted at the project dir
// (spec seat-identity §3.1): with no env and a .pact/seat file, the acting seat
// is read from the file — so every CLI verb and MCP tool (both funnel through
// the engine) picks it up without per-call plumbing.
func TestEngineResolvesSeatFromFile(t *testing.T) {
	repo := newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv("PACT_DIR", "")
	if err := pact.At(repo).As("orch").Init("p", []string{"orch:orchestrator:CLAUDE.md", "rev:reviewer:R.md"}); err != nil {
		t.Fatal(err)
	}
	// No env identity; the working-copy seat file names 'rev'.
	t.Setenv("PACT_AGENT_ID", "")
	if err := os.WriteFile(filepath.Join(repo, ".pact", "seat"), []byte("rev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, source, err := pact.At(repo).ResolveSeat()
	if err != nil {
		t.Fatalf("ResolveSeat: %v", err)
	}
	if id != "rev" || source != "file" {
		t.Fatalf("ResolveSeat = (%q,%q), want (rev,file)", id, source)
	}
}

// With neither env nor file, ResolveSeat fails closed (fail-fast — spec §3.1).
func TestEngineResolveSeatFailsClosed(t *testing.T) {
	repo := newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv("PACT_DIR", "")
	if err := pact.At(repo).As("orch").Init("p", []string{"orch:orchestrator:CLAUDE.md"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACT_AGENT_ID", "")
	if _, _, err := pact.At(repo).ResolveSeat(); err == nil {
		t.Fatal("ResolveSeat must fail closed when neither env nor seat file is present")
	}
}

// The .As() actor override still wins over the chain (used by orchestrate's
// --as and tests): it is an explicit, in-process identity assertion.
func TestEngineActorOverridesChain(t *testing.T) {
	repo := newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv("PACT_DIR", "")
	if err := pact.At(repo).As("orch").Init("p", []string{"orch:orchestrator:CLAUDE.md", "rev:reviewer:R.md"}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, ".pact", "seat"), []byte("rev\n"), 0o644)
	t.Setenv("PACT_AGENT_ID", "")
	if id, source, _ := pact.At(repo).As("orch").ResolveSeat(); id != "orch" || source != "actor" {
		t.Fatalf("actor override must win: got (%q,%q), want (orch,actor)", id, source)
	}
}

// UseSeat writes the working-copy seat file after validating the id is in the
// roster, and excludes it from git so it can never be committed (spec §3.1).
func TestUseSeatWritesFileAndExcludes(t *testing.T) {
	repo := newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv("PACT_DIR", "")
	if err := pact.At(repo).As("orch").Init("p", []string{"orch:orchestrator:CLAUDE.md", "rev:reviewer:R.md"}); err != nil {
		t.Fatal(err)
	}
	if err := pact.At(repo).UseSeat("rev"); err != nil {
		t.Fatalf("UseSeat: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(repo, ".pact", "seat"))
	if err != nil || string(b) != "rev\n" {
		t.Fatalf("seat file = %q (%v), want \"rev\\n\"", b, err)
	}
	// It must be excluded from git — a committed seat file is the accident this
	// whole design exists to prevent.
	excl, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if !strings.Contains(string(excl), ".pact/seat") {
		t.Fatalf(".pact/seat must be in .git/info/exclude, got:\n%s", excl)
	}
	// And it must resolve — once the env identity is cleared, the file layer wins.
	t.Setenv("PACT_AGENT_ID", "")
	if id, src, _ := pact.At(repo).ResolveSeat(); id != "rev" || src != "file" {
		t.Fatalf("after UseSeat, ResolveSeat = (%q,%q), want (rev,file)", id, src)
	}
}

// UseSeat refuses an id that is not in the roster, naming the valid seats —
// a typo must fail loudly, not write a dead identity.
func TestUseSeatRejectsUnknownSeat(t *testing.T) {
	repo := newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv("PACT_DIR", "")
	if err := pact.At(repo).As("orch").Init("p", []string{"orch:orchestrator:CLAUDE.md", "rev:reviewer:R.md"}); err != nil {
		t.Fatal(err)
	}
	err := pact.At(repo).UseSeat("nope")
	if err == nil || !strings.Contains(err.Error(), "roster") {
		t.Fatalf("UseSeat of an unknown seat must fail naming the roster, got %v", err)
	}
	if _, e := os.Stat(filepath.Join(repo, ".pact", "seat")); e == nil {
		t.Fatal("no seat file may be written for a rejected id")
	}
}
