package pact

import (
	"encoding/json"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

// snapHeader is a minimal view of state-snapshot.json for assertions (the full
// struct is unexported in the projection package).
type snapHeader struct {
	Version     int   `json:"version"`
	LedgerBytes int64 `json:"ledger_bytes"`
}

func readSnapHeader(t *testing.T, repo string) snapHeader {
	t.Helper()
	b, err := os.ReadFile(paths.StateSnapshotIn(repo))
	if err != nil {
		t.Fatalf("snapshot missing: %v", err)
	}
	var h snapHeader
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("snapshot not valid json: %v", err)
	}
	return h
}

// fullFold reads the ground-truth State straight from the log, with the snapshot
// forcibly bypassed — the reference every engine read must match.
func fullFold(t *testing.T, repo string) projection.State {
	t.Helper()
	evs, err := event.ReadAll(paths.LogIn(repo))
	if err != nil {
		t.Fatal(err)
	}
	return projection.Project(evs)
}

// After every verb, the snapshot is rewritten to cover the WHOLE log and the engine
// read (StateProjection, snapshot-backed) equals a full fold. Also asserts the cache
// is registered with the repo's runtime-ignore list so it can never be committed.
func TestVerbsKeepSnapshotFreshAndIgnored(t *testing.T) {
	repo := newRepo(t)
	t.Setenv("PACT_AGENT_ID", "orch")

	p := At(repo)
	if err := p.Init("proj", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}

	// The snapshot exists after the very first verb and is registered in
	// .git/info/exclude (never committed — it is a cache, not the source of truth).
	if _, err := os.Stat(paths.StateSnapshotIn(repo)); err != nil {
		t.Fatalf("no snapshot after Init: %v", err)
	}
	if !gitx.PathIgnored(repo, ".pact/state-snapshot.json") {
		excl, _ := gitx.GitPath(repo, "info/exclude")
		body, _ := os.ReadFile(excl)
		t.Fatalf("snapshot not git-ignored; info/exclude:\n%s", body)
	}

	// After a sequence of verbs the snapshot always covers the full current log and
	// the engine read matches a full fold exactly.
	steps := []func() error{
		func() error { return p.As("orch").Assign("t1", "f", "feat/f", "w", "orch", "", nil) },
		func() error { return At(repo).As("w").Join("w", "worker") },
		func() error { return At(repo).As("w").Checkpoint("t1", "did it") },
		func() error { return p.As("orch").Accept("t1") },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		logBytes, err := os.ReadFile(paths.LogIn(repo))
		if err != nil {
			t.Fatal(err)
		}
		h := readSnapHeader(t, repo)
		if h.Version != projection.SnapshotVersion {
			t.Fatalf("step %d: snapshot version %d", i, h.Version)
		}
		if h.LedgerBytes != int64(len(logBytes)) {
			t.Fatalf("step %d: snapshot covers %d bytes, log is %d — snapshot lags the log", i, h.LedgerBytes, len(logBytes))
		}
		got, err := At(repo).StateProjection()
		if err != nil {
			t.Fatalf("step %d: StateProjection: %v", i, err)
		}
		if want := fullFold(t, repo); !reflect.DeepEqual(want, got) {
			t.Fatalf("step %d: snapshot-backed read != full fold\n want %#v\n got  %#v", i, want, got)
		}
	}
}

// PACT_NO_SNAPSHOT=1 makes verbs skip writing the cache entirely, and reads still
// return the correct (full-folded) state.
func TestNoSnapshotEnvSkipsWrite(t *testing.T) {
	repo := newRepo(t)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv(projection.NoSnapshotEnv, "1")

	p := At(repo)
	if err := p.Init("proj", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.As("orch").Assign("t1", "f", "feat/f", "w", "orch", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.StateSnapshotIn(repo)); !os.IsNotExist(err) {
		t.Fatalf("snapshot written despite PACT_NO_SNAPSHOT=1 (err=%v)", err)
	}
	got, err := At(repo).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	if want := fullFold(t, repo); !reflect.DeepEqual(want, got) {
		t.Fatalf("read wrong state under PACT_NO_SNAPSHOT=1")
	}
}

// Concurrent verbs serialize on withLedgerLock, so each snapshot rewrite (atomic
// tmp+rename) is whole: a reader never sees a torn snapshot, and the final snapshot
// reflects every committed verb. Run under -race. Distinct features means every
// concurrent Assign is legal, so all N must land.
func TestConcurrentVerbsSnapshotNotTorn(t *testing.T) {
	repo := newRepo(t)
	t.Setenv("PACT_AGENT_ID", "orch")

	p := At(repo)
	if err := p.Init("proj", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}

	const n = 12
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			feat := "f" + string(rune('a'+i))
			errs[i] = At(repo).As("orch").Assign("t"+feat, feat, "feat/"+feat, "w", "orch", "", nil)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent assign %d failed: %v", i, err)
		}
	}

	// The persisted snapshot parses cleanly, covers the whole log, and — read back
	// through the engine — equals a full fold with all N features present.
	logBytes, _ := os.ReadFile(paths.LogIn(repo))
	if h := readSnapHeader(t, repo); h.LedgerBytes != int64(len(logBytes)) {
		t.Fatalf("final snapshot covers %d bytes, log is %d", h.LedgerBytes, len(logBytes))
	}
	got, err := At(repo).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Features) != n {
		t.Fatalf("want %d features, got %d", n, len(got.Features))
	}
	if want := fullFold(t, repo); !reflect.DeepEqual(want, got) {
		t.Fatalf("snapshot-backed read != full fold after concurrent verbs")
	}
	// Guard against a leftover temp file from a torn atomic write.
	if _, err := os.Stat(paths.StateSnapshotIn(repo) + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("snapshot .tmp left behind: %v", err)
	}
}

// A stale snapshot from an OLDER schema version is silently ignored: the engine
// full-folds to the correct state, then the next verb rewrites it to the current
// version. Proves the read validity check and the write refresh interlock.
func TestStaleVersionSnapshotHealed(t *testing.T) {
	repo := newRepo(t)
	t.Setenv("PACT_AGENT_ID", "orch")

	p := At(repo)
	if err := p.Init("proj", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.As("orch").Assign("t1", "f", "feat/f", "w", "orch", "", nil); err != nil {
		t.Fatal(err)
	}

	// Corrupt the on-disk snapshot to a future/unknown version.
	snapPath := paths.StateSnapshotIn(repo)
	b, _ := os.ReadFile(snapPath)
	var m map[string]any
	json.Unmarshal(b, &m)
	m["version"] = 99
	poisoned, _ := json.Marshal(m)
	if err := os.WriteFile(snapPath, poisoned, 0o644); err != nil {
		t.Fatal(err)
	}

	// Read still correct (version mismatch → full fold).
	got, err := At(repo).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	if want := fullFold(t, repo); !reflect.DeepEqual(want, got) {
		t.Fatalf("stale-version read != full fold")
	}

	// A subsequent verb heals the snapshot back to the current version.
	if err := p.As("orch").Assign("t2", "g", "feat/g", "w", "orch", "", nil); err != nil {
		t.Fatal(err)
	}
	if h := readSnapHeader(t, repo); h.Version != projection.SnapshotVersion {
		t.Fatalf("snapshot not healed to current version, got %d", h.Version)
	}
}
