package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/agentjoey/pactify/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorageCorruptReferringJobProtectsChain(t *testing.T) {
	for _, raw := range []string{`{"id":"bad","resumeOf":"old",`, `not-json`} {
		s := storageStore(t)
		storageJob(t, s, "old", StatusSucceeded, 40*24*time.Hour)
		os.MkdirAll(s.Dir("bad"), 0700)
		os.WriteFile(filepath.Join(s.Dir("bad"), "state.json"), []byte(raw), 0600)
		p, e := s.PlanStorage(context.Background(), PruneOptions{})
		if e != nil {
			t.Fatal(e)
		}
		if i := storageItem(p, "jobs/old"); i == nil || !i.Protected {
			t.Fatalf("corrupt referring side allowed deletion: %+v", i)
		}
		r, _ := s.openStorage()
		st, _ := s.storageState(r, "old")
		info, _ := r.Lstat("jobs/old")
		e = s.storageChainUnchanged(r, []State{st}, map[string]os.FileInfo{"jobs/old": info})
		r.Close()
		if e == nil {
			t.Fatal("revalidation ignored corrupt referring state")
		}
	}
}
func TestStorageFreshCrashLeftoverDoesNotBlockSharedCleanup(t *testing.T) {
	s := storageStore(t)
	p := s.Dir("incomplete")
	os.MkdirAll(p, 0700)
	os.WriteFile(filepath.Join(p, "state.lock"), nil, 0600)
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(p, old, old)
	os.Chtimes(filepath.Join(p, "state.lock"), old, old)
	cache := filepath.Join(s.storageHome(), "cache/go-build")
	os.MkdirAll(cache, 0700)
	os.WriteFile(filepath.Join(cache, "entry"), make([]byte, 1000), 0600)
	plan, e := s.PlanStorage(context.Background(), PruneOptions{CacheMaxBytes: 100})
	if e != nil {
		t.Fatal(e)
	}
	if plan.Locations[1].Reclaimable == 0 {
		t.Fatalf("crash leftover blocks cache: %+v", plan)
	}
	idle, e := s.storageIdle()
	if e != nil || !idle {
		t.Fatalf("crash leftover considered live job: %v %v", idle, e)
	}
	if i := storageItem(plan, "jobs/incomplete"); i == nil || !i.Protected || !strings.Contains(i.Reason, "crash") {
		t.Fatalf("leftover missing report: %+v", i)
	}
}
func TestStorageWorktreeFromCannotDeadlockPrune(t *testing.T) {
	s := storageStore(t)
	st, e := s.CreateRun("old", Request{Harness: "fake", Access: "write", Cwd: repo(t)}, true, false)
	if e != nil {
		t.Fatal(e)
	}
	st.Status = StatusSucceeded
	st.FinishedAt = time.Now().Add(-30 * 24 * time.Hour)
	s.Save(st)
	atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, Status: st.Status})
	entered := make(chan struct{})
	release := make(chan struct{})
	pruned := make(chan StorageRun, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		r, _ := s.pruneStorage(ctx, PruneOptions{IncludeWorktrees: true}, storageDeps{deleteSessions: func(context.Context, []State) error { close(entered); <-release; return nil }})
		pruned <- r
	}()
	<-entered
	transferred := make(chan error, 1)
	go func() { _, e := s.WorktreeFrom("new", st.Request, "old"); transferred <- e }()
	time.Sleep(80 * time.Millisecond)
	close(release)
	select {
	case r := <-pruned:
		if len(r.Failures) > 0 {
			t.Fatalf("prune/transfer lock inversion: %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("prune deadlocked")
	}
	select {
	case e := <-transferred:
		if e == nil {
			t.Fatal("transferred pruned worktree")
		}
	case <-time.After(time.Second):
		t.Fatal("worktree transfer stuck")
	}
}
func TestStorageAgyReceiptSurvivesHomePreparation(t *testing.T) {
	s := storageStore(t)
	home := "harness-homes/agy"
	p := filepath.Join(s.storageHome(), home, ".gemini/antigravity-cli/conversations/session.pb")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, nil, 0600)
	r, _ := s.openStorage()
	defer r.Close()
	if e := s.deletePrivateSessions(context.Background(), r, "agy", home, []string{"session"}, nil); e != nil {
		t.Fatal(e)
	}
	receipt := filepath.Join(s.storageHome(), "storage-receipts/agy.json")
	if _, e := os.Stat(receipt); e != nil {
		t.Fatal("receipt still in mirrored home", e)
	}
	os.RemoveAll(filepath.Join(s.storageHome(), home))
	os.MkdirAll(filepath.Join(s.storageHome(), home), 0700)
	if e := storageVerifyAgy(context.Background(), r, home, []string{"session"}); e != nil {
		t.Fatal("receipt lost after home replacement", e)
	}
}
func TestStorageOrphanTrashMarkerAgesOut(t *testing.T) {
	s := storageStore(t)
	os.MkdirAll(filepath.Join(s.storageHome(), "trash"), 0700)
	p := filepath.Join(s.storageHome(), "trash/ghost.json")
	atomicJSON(p, map[string]string{"path": "trash/ghost"})
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(p, old, old)
	plan, e := s.PlanStorage(context.Background(), PruneOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if i := storageItem(plan, "trash/ghost.json"); i == nil || i.Protected {
		t.Fatalf("orphan marker omitted from plan: %+v", i)
	}
	if _, e = s.PruneStorage(context.Background(), PruneOptions{}); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("orphan marker retained", e)
	}
}
func TestStorageOpencodeNeverStartedHasNoSessions(t *testing.T) {
	s := storageStore(t)
	st := storageJob(t, s, "failed-start", StatusFailed, time.Hour)
	st.Request.Harness = "opencode"
	s.Save(st)
	r, _ := s.openStorage()
	defer r.Close()
	if _, e := s.storageOpencodeKeep(r, "harness-homes/opencode/share", nil); e != nil {
		t.Fatal(e)
	}
}
func TestStorageKimiAliasedIndexHome(t *testing.T) {
	s := storageStore(t)
	home := "harness-homes/kimi"
	dir := filepath.Join(s.storageHome(), home, "sessions/ws/session")
	os.MkdirAll(dir, 0700)
	alias := filepath.Join(t.TempDir(), "home")
	os.Symlink(s.storageHome(), alias)
	row, _ := json.Marshal(map[string]string{"sessionId": "session", "sessionDir": filepath.Join(alias, home, "sessions/ws/session")})
	os.WriteFile(filepath.Join(s.storageHome(), home, "session_index.jsonl"), row, 0600)
	r, _ := s.openStorage()
	defer r.Close()
	paths, e := storageKimiSessions(r, home, []string{"session"})
	if e != nil || len(paths) != 1 {
		t.Fatalf("alias rejected: %v %v", paths, e)
	}
}

func TestStorageUnrelatedHomeDeltaIsNotReportedAsRemoved(t *testing.T) {
	s := storageStore(t)
	storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	p := filepath.Join(s.storageHome(), "harness-homes/agy/unrelated.data")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, make([]byte, 1000), 0600)
	run, e := s.pruneStorage(context.Background(), PruneOptions{}, storageDeps{deleteSessions: func(context.Context, []State) error { return os.Remove(p) }})
	if e != nil {
		t.Fatal(e)
	}
	if run.Removed[2].Bytes != 0 || run.Removed[2].Items != 0 {
		t.Fatalf("unrelated change attributed to cleanup: %+v", run.Removed[2])
	}
}
func TestStorageMissingCodexPreflightKeepsNativeSessions(t *testing.T) {
	s := storageStore(t)
	t.Setenv("PATH", t.TempDir())
	states := []State{}
	for _, h := range []string{"codex", "agy"} {
		st := storageJob(t, s, h, StatusSucceeded, 30*24*time.Hour)
		st.Request.Harness = h
		st.HarnessHome = filepath.Join(s.storageHome(), "harness-homes", h)
		s.Save(st)
		atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, Status: st.Status, SessionID: "session"})
		states = append(states, st)
	}
	p := filepath.Join(s.storageHome(), "harness-homes/agy/.gemini/antigravity-cli/conversations/session.pb")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("keep"), 0600)
	if e := s.deleteStorageSessions(context.Background(), states); e == nil {
		t.Fatal("codex absence accepted")
	}
	if _, e := os.Stat(p); e != nil {
		t.Fatal("native session lost before codex availability check")
	}
}

func TestStorageCachePassBoundsStateReads(t *testing.T) {
	s := storageStore(t)
	storageJob(t, s, "retained", StatusSucceeded, time.Hour)
	p := filepath.Join(s.storageHome(), "cache/go-build")
	os.MkdirAll(p, 0700)
	const entries = 2048
	for i := range entries {
		os.WriteFile(filepath.Join(p, fmt.Sprint(i)), make([]byte, 100), 0600)
	}
	reads := 0
	run, e := s.pruneStorage(context.Background(), PruneOptions{CacheMaxBytes: 1}, storageDeps{idleCheck: func(context.Context) (bool, error) { reads++; return s.storageIdle() }})
	if e != nil || len(run.Failures) > 0 {
		t.Fatalf("prune: %+v %v", run, e)
	}
	t.Logf("%d full state checks for %d cache entries", reads, entries)
	if reads == 0 || reads > 80 {
		t.Fatalf("%d full state reads for %d cache entries", reads, entries)
	}
	if run.Removed[1].Items != entries {
		t.Fatalf("cache not removed: %+v", run)
	}
}

func TestStorageNativeRemovalAccounting(t *testing.T) {
	s := storageStore(t)
	t.Setenv("PATH", t.TempDir())
	p := filepath.Join(s.storageHome(), "harness-homes/agy/.gemini/antigravity-cli/conversations/orphan.pb")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, make([]byte, 1234), 0600)
	run, e := s.PruneStorage(context.Background(), PruneOptions{})
	if e != nil || len(run.Failures) > 0 {
		t.Fatalf("%+v %v", run, e)
	}
	got := run.Removed[2]
	if got.Items != 1 || got.Sessions != 1 || got.Bytes != 1234 {
		t.Fatalf("native deletion accounting: %+v", got)
	}
	run, e = s.PruneStorage(context.Background(), PruneOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if run.Removed[2].Items != 0 || run.Removed[2].Bytes != 0 || run.Removed[2].Sessions != 0 {
		t.Fatalf("retry double-counted: %+v", run)
	}
}
func TestStorageIdleCheckHonorsCancellation(t *testing.T) {
	s := storageStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.storageIdleContext(ctx); e != context.Canceled {
		t.Fatalf("idle scan ignored cancellation: %v", e)
	}
}

func TestStorageAgyLegacyReceiptMigrates(t *testing.T) {
	s := storageStore(t)
	home := "harness-homes/agy"
	p := filepath.Join(s.storageHome(), home)
	os.MkdirAll(p, 0700)
	atomicJSON(filepath.Join(p, ".pactify-pruned-sessions.json"), map[string]bool{"deleted": true})
	r, _ := s.openStorage()
	defer r.Close()
	if e := storageVerifyAgy(context.Background(), r, home, []string{"deleted"}); e != nil {
		t.Fatal(e)
	}
	if e := s.deletePrivateSessions(context.Background(), r, "agy", home, []string{"deleted"}, nil); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Lstat(filepath.Join(home, ".pactify-pruned-sessions.json")); !os.IsNotExist(e) {
		t.Fatalf("legacy receipt retained after migration: %v", e)
	}
	var done map[string]bool
	if e := storageReadJSON(r, "storage-receipts/agy.json", &done); e != nil || !done["deleted"] {
		t.Fatalf("legacy receipt not migrated: %v %v", done, e)
	}
}

func TestStorageOpencodeSweepAccountingWithRealPathShape(t *testing.T) {
	for _, scenario := range []string{"storage-sweep", "storage-checkpoint"} {
		t.Run(scenario, func(t *testing.T) {
			testenv.InstallOpencodeServe(t)
			t.Setenv("COCKPIT_OC_SCENARIO", scenario)
			s := storageStore(t)
			db := filepath.Join(s.storageHome(), "harness-homes/opencode/share/opencode/opencode.db")
			os.MkdirAll(filepath.Dir(db), 0700)
			os.WriteFile(db, make([]byte, 2000), 0600)
			run, e := s.PruneStorage(context.Background(), PruneOptions{})
			if e != nil || len(run.Failures) > 0 {
				t.Fatalf("%+v %v", run, e)
			}
			got := run.Removed[2]
			if !got.DatabaseMeasured || got.DatabaseBytes != 2000 {
				t.Fatalf("database delta missing: %+v", got)
			}
			if scenario == "storage-sweep" {
				if got.Items != 1 || got.Sessions != 1 || got.Bytes != 2000 {
					t.Fatalf("deleted session not counted: %+v", got)
				}
			} else if got.Items != 0 || got.Sessions != 0 || got.Bytes != 0 {
				t.Fatalf("checkpoint counted as session removal: %+v", got)
			}
		})
	}
}

func TestStorageIdlePassDetectsNewJobImmediately(t *testing.T) {
	s := storageStore(t)
	storageJob(t, s, "finished", StatusSucceeded, time.Hour)
	r, _ := s.openStorage()
	defer r.Close()
	checks := 0
	pass := storageIdlePass{root: r, jobs: "jobs", check: func(ctx context.Context) (bool, error) { checks++; return s.storageIdleContext(ctx) }}
	if ok, e := pass.allow(context.Background()); e != nil || !ok {
		t.Fatal(e)
	}
	s.Create("queued", Request{Harness: "codex"})
	if ok, _ := pass.allow(context.Background()); ok || checks != 2 {
		t.Fatalf("creation generation ignored: allowed=%v checks=%d", ok, checks)
	}
}

func TestStorageRepeatedResumeSessionCountedOnce(t *testing.T) {
	s := storageStore(t)
	home := "harness-homes/agy"
	p := filepath.Join(s.storageHome(), home, ".gemini/antigravity-cli/conversations/session.pb")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, make([]byte, 100), 0600)
	r, _ := s.openStorage()
	defer r.Close()
	removed, e := s.deletePrivateSessionsMeasured(context.Background(), r, "agy", home, []string{"session", "session"}, nil)
	if e != nil || removed.Items != 1 || removed.Sessions != 1 || removed.Bytes != 100 {
		t.Fatalf("duplicate resume counted twice: %+v %v", removed, e)
	}
}

func TestStorageNonJobReferenceCannotBoundUnreadableState(t *testing.T) {
	for _, kind := range []string{"file", "crash-directory"} {
		t.Run(kind, func(t *testing.T) {
			s := storageStore(t)
			st := storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
			os.MkdirAll(s.Dir("bad"), 0700)
			os.WriteFile(filepath.Join(s.Dir("bad"), "state.json"), []byte(`{"resumeOf":"summary-index.json",`), 0600)
			if kind == "file" {
				os.WriteFile(filepath.Join(s.Root, "summary-index.json"), nil, 0600)
			} else {
				os.Mkdir(filepath.Join(s.Root, "summary-index.json"), 0700)
				old := time.Now().Add(-2 * time.Hour)
				os.Chtimes(filepath.Join(s.Root, "summary-index.json"), old, old)
			}
			p, e := s.PlanStorage(context.Background(), PruneOptions{})
			if e != nil {
				t.Fatal(e)
			}
			if i := storageItem(p, "jobs/old"); i == nil || !i.Protected {
				t.Fatalf("non-job reference bypassed conservative fallback: %+v", i)
			}
			r, _ := s.openStorage()
			defer r.Close()
			info, _ := r.Lstat("jobs/old")
			if e = s.storageChainUnchanged(r, []State{st}, map[string]os.FileInfo{"jobs/old": info}); e == nil {
				t.Fatal("non-job reference bypassed revalidation")
			}
		})
	}
}
