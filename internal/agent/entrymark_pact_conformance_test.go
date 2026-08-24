package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// pact.BakeEntry (the path `pactify init`/`setup` takes for every seat, and thus
// the origin of most entry files in the wild) writes its managed block by hand
// instead of calling entryBody — internal/pact cannot import internal/agent
// without a cycle. So the attribution line exists in two places, and this test
// is the seam that keeps them from drifting: it bakes a real entry file through
// pact and parses it with THIS package's reader.
//
// Without it, a format change here would silently stop attributing every
// init-created entry file, which does not fail loudly — it degrades to the
// legacy fallback, quietly crediting doc-only co-tenants as wired again. That is
// exactly the bug entry attribution exists to fix, so it must not be able to
// come back by accident.
func TestBakeEntryAttributionParsesInAgent(t *testing.T) {
	dir := t.TempDir()
	const kind = "opencode"
	if err := pact.BakeEntry(dir, pact.Seat{ID: "w", Roles: []string{"worker"}, Entry: "AGENTS.md", Kind: kind}); err != nil {
		t.Fatalf("BakeEntry: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	kinds, attributed := entryKinds(string(b))
	if !attributed {
		t.Fatalf("pact.BakeEntry produced an UNATTRIBUTED block — the two marker formats have drifted:\n%s", b)
	}
	if len(kinds) != 1 || kinds[0] != kind {
		t.Errorf("attributed kinds = %v, want [%s]", kinds, kind)
	}
	// The whole point: a co-tenant of the same entry file must NOT be credited.
	if _, ok := Get("codex-cli"); ok {
		ws := probeKind("codex-cli", dir)
		if ws.Wired {
			t.Errorf("codex-cli shares AGENTS.md and was never wired, but probe reports %+v", ws)
		}
	}
	if ws := probeKind(kind, dir); !ws.Wired || ws.Via != ViaEntry {
		t.Errorf("the baked kind must read back as entry-wired, got %+v", ws)
	}
}

// A kind-less seat (the shell/legacy form, Seat.Kind == "") has nothing to
// attribute, so BakeEntry must emit no attribution line at all — and the block
// then falls through to the legacy scoring rather than claiming an empty set,
// which entryKinds would otherwise report as "attributed to nobody".
func TestBakeEntryKindlessSeatStaysUnattributed(t *testing.T) {
	dir := t.TempDir()
	if err := pact.BakeEntry(dir, pact.Seat{ID: "w", Roles: []string{"worker"}, Entry: "AGENTS.md"}); err != nil {
		t.Fatalf("BakeEntry: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if _, attributed := entryKinds(string(b)); attributed {
		t.Errorf("a kind-less seat must not produce an attribution line:\n%s", b)
	}
}
