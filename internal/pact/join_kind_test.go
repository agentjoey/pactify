package pact_test

import (
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// TestJoinKindWritesKindToLedgerAndProjection: `join --kind` records the kind on
// the join event payload (ledger) AND the fold lifts it onto Agents[].Kind so
// orchestrate can resolve seat→kind from the live roster (spec §6 WS-K).
func TestJoinKindWritesKindToLedgerAndProjection(t *testing.T) {
	repo, w := clientRepo(t)

	if err := w.JoinKind("w", "worker", "opencode"); err != nil {
		t.Fatalf("JoinKind: %v", err)
	}

	// (1) ledger: the join payload carries the declared kind.
	if got, _ := lastJoinPayload(t, repo)["kind"].(string); got != "opencode" {
		t.Fatalf("join payload kind = %q, want %q", got, "opencode")
	}

	// (2) projection: Agents[].Kind for seat w is set from the join.
	st, err := pact.At(repo).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	var kind string
	for _, a := range st.Agents {
		if a.ID == "w" {
			kind = a.Kind
		}
	}
	if kind != "opencode" {
		t.Fatalf("projection Agents[w].Kind = %q, want %q", kind, "opencode")
	}
}

// TestJoinNoKindByteIdentical: a kind-free join emits NO kind field, keeping
// client-free/kind-free logs byte-identical to the pre-feature payload shape.
func TestJoinNoKindByteIdentical(t *testing.T) {
	repo, w := clientRepo(t)
	if err := w.Join("w", "worker"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, ok := lastJoinPayload(t, repo)["kind"]; ok {
		t.Fatal("kind-free join must not emit a kind field")
	}
}
