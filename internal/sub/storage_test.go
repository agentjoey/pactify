package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agentjoey/pactify/internal/procx"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func storageJob(t *testing.T, s Store, id string, status Status, age time.Duration) State {
	t.Helper()
	st, err := s.Create(id, Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = status
	st.FinishedAt = time.Now().Add(-age)
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(s.ResultPath(id), Result{JobID: id, Status: status}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStorageConcurrentGateInitialization(t *testing.T) {
	for range 10 {
		s := storageStore(t)
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				release, err := s.storageGate(context.Background())
				if err != nil {
					info, e := os.Lstat(filepath.Join(s.storageHome(), "storage-jobs.lock"))
					t.Errorf("gate: %v; after error lock=%v stat=%v", err, info, e)
					return
				}
				release()
			})
		}
		wg.Wait()
	}
}
func storageStore(t *testing.T) Store { return Store{Root: filepath.Join(t.TempDir(), "jobs")} }
func storageItem(p StoragePlan, path string) *StorageItem {
	for _, l := range p.Locations {
		for _, i := range l.Items {
			if i.Path == path {
				return &i
			}
		}
	}
	return nil
}
func TestStorageRetention(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		status                   Status
		age                      time.Duration
		pending, include, remove bool
	}{
		{"old", StatusSucceeded, 15 * 24 * time.Hour, false, false, true},
		{"failed", StatusFailed, 15 * 24 * time.Hour, false, false, true},
		{"canceled", StatusCanceled, 15 * 24 * time.Hour, false, false, true},
		{"running", StatusRunning, 30 * 24 * time.Hour, false, true, false},
		{"queued", StatusQueued, 30 * 24 * time.Hour, false, true, false},
		{"recent", StatusSucceeded, 13 * 24 * time.Hour, false, false, false},
		{"pending", StatusSucceeded, 30 * 24 * time.Hour, true, false, false},
		{"explicit", StatusSucceeded, 30 * 24 * time.Hour, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := storageStore(t)
			st := storageJob(t, s, "job", tc.status, tc.age)
			if tc.pending {
				st.Worktree = &Worktree{Owner: st.ID, Path: filepath.Join(s.Dir(st.ID), "wt")}
				if err := s.Save(st); err != nil {
					t.Fatal(err)
				}
			}
			p, err := s.PlanStorage(context.Background(), PruneOptions{IncludeWorktrees: tc.include})
			if err != nil {
				t.Fatal(err)
			}
			i := storageItem(p, "jobs/job")
			if i == nil || i.Protected == tc.remove {
				t.Fatalf("item=%+v", i)
			}
			if tc.pending && !tc.include && len(p.StaleWorktrees) != 1 {
				t.Fatalf("stale=%+v", p.StaleWorktrees)
			}
		})
	}
}
func TestStorageAgeBoundaryAndResumeChain(t *testing.T) {
	s := storageStore(t)
	old := storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	next := storageJob(t, s, "next", StatusSucceeded, time.Hour)
	next.ResumeOf = old.ID
	s.Save(next)
	p, err := s.PlanStorage(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if i := storageItem(p, "jobs/old"); i == nil || !i.Protected {
		t.Fatalf("old chain member=%+v", i)
	}
	now := time.Now()
	old.FinishedAt = now.Add(-14 * 24 * time.Hour)
	if reason := storageJobReason(old, now, 14*24*time.Hour, false); reason == "" {
		t.Fatal("age boundary must be kept")
	}
}
func TestStorageCacheLRUAndReadonlyModules(t *testing.T) {
	s := storageStore(t)
	home := filepath.Dir(s.Root)
	for i, name := range []string{"cache/go-mod/example@v1/file", "cache/go-build/new"} {
		p := filepath.Join(home, name)
		os.MkdirAll(filepath.Dir(p), 0700)
		os.WriteFile(p, []byte(strings.Repeat("x", 60)), 0400)
		when := time.Now().Add(-time.Duration(10-i) * time.Hour)
		os.Chtimes(p, when, when)
	}
	os.Chmod(filepath.Join(home, "cache/go-mod/example@v1"), 0500)
	p, err := s.PlanStorage(context.Background(), PruneOptions{CacheMaxBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if p.Locations[1].Reclaimable != 60 {
		t.Fatalf("cache=%+v", p.Locations[1])
	}
	r, err := s.PruneStorage(context.Background(), PruneOptions{CacheMaxBytes: 100})
	if err != nil || len(r.Failures) > 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if _, err := os.Lstat(filepath.Join(home, "cache/go-mod/example@v1")); !os.IsNotExist(err) {
		t.Fatalf("old module retained: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, "cache/go-build/new")); err != nil {
		t.Fatal(err)
	}
}
func TestStorageSymlinksCredentialsAndActiveProtection(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(string(rune('0'+boolInt(active))), func(t *testing.T) {
			s := storageStore(t)
			home := filepath.Dir(s.Root)
			outside := t.TempDir()
			os.WriteFile(filepath.Join(outside, "keep"), []byte("secret"), 0600)
			dirs := []string{"cache/go-build", "harness-homes/kimi/credentials", "harness-homes/kimi/logs"}
			for _, dir := range dirs {
				os.MkdirAll(filepath.Join(home, dir), 0700)
			}
			os.WriteFile(filepath.Join(home, dirs[0], "old"), []byte("cache"), 0600)
			os.WriteFile(filepath.Join(home, dirs[1], "keep"), []byte("secret"), 0600)
			os.Symlink(outside, filepath.Join(home, dirs[0], "external"))
			os.Symlink(outside, filepath.Join(home, "harness-homes/kimi/oauth"))
			log := filepath.Join(home, dirs[2], "old.log")
			os.WriteFile(log, []byte("log"), 0600)
			old := time.Now().Add(-30 * 24 * time.Hour)
			os.Chtimes(log, old, old)
			if active {
				storageJob(t, s, "active", StatusRunning, 30*24*time.Hour)
			}
			r, err := s.PruneStorage(context.Background(), PruneOptions{CacheMaxBytes: 1})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{filepath.Join(outside, "keep"), filepath.Join(home, dirs[1], "keep"), filepath.Join(home, "harness-homes/kimi/oauth"), filepath.Join(home, dirs[0], "external")} {
				if _, e := os.Lstat(p); e != nil {
					t.Fatal(e)
				}
			}
			if active {
				if _, e := os.Lstat(log); e != nil {
					t.Fatalf("running job home touched: %v; %+v", e, r)
				}
			}
		})
	}
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func TestStorageSessionFailureKeepsChainAndRecordsStatus(t *testing.T) {
	s := storageStore(t)
	st := storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, SessionID: "thread-old"})
	r, err := s.pruneStorage(context.Background(), PruneOptions{}, storageDeps{deleteSessions: func(context.Context, []State) error { return errors.New("fake server unavailable") }})
	if err != nil || len(r.Failures) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := s.Load(st.ID); err != nil {
		t.Fatal("failed session deletion lost job", err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(s.Root), "storage-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored StorageRun
	if err := json.Unmarshal(data, &stored); err != nil || stored.EndedAt.IsZero() || len(stored.Failures) != 1 {
		t.Fatalf("status=%s %v", data, err)
	}
}

func TestStorageCreationHasBoundedGateAndStateIsRechecked(t *testing.T) {
	s := storageStore(t)
	storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	root, err := s.openStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	release, err := storageLock(context.Background(), root, "storage-jobs.lock", syscall.LOCK_EX)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Create("new", Request{Harness: "codex"}); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2500 * time.Millisecond):
		release()
		t.Fatal("creation blocked by prune")
	}
	release()
	release, err = storageLock(context.Background(), root, "jobs/old/state.lock", syscall.LOCK_EX)
	if err != nil {
		t.Fatal(err)
	}
	cleaned := make(chan StorageRun, 1)
	go func() { r, _ := s.PruneStorage(context.Background(), PruneOptions{}); cleaned <- r }()
	// The planner sees a terminal state, but startup wins before its lock is free.
	time.Sleep(80 * time.Millisecond)
	st, _ := s.Load("old")
	st.Status = StatusRunning
	s.Save(st)
	release()
	r := <-cleaned
	if _, err = s.Load("old"); err != nil {
		t.Fatalf("running job deleted: %+v %v", r, err)
	}
}
func TestStorageCredentialInsideOldLogsIsProtected(t *testing.T) {
	s := storageStore(t)
	p := filepath.Join(s.storageHome(), "harness-homes/kimi/logs/credentials/old.log")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("keep"), 0600)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(p, old, old)
	r, err := s.PruneStorage(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(p); err != nil {
		t.Fatalf("credential deleted: %+v %v", r, err)
	}
}

func TestStorageResumeCycleAndUnreadableOwnership(t *testing.T) {
	for _, kind := range []string{"cycle", "corrupt", "symlink-state"} {
		t.Run(kind, func(t *testing.T) {
			s := storageStore(t)
			a := storageJob(t, s, "a", StatusSucceeded, 30*24*time.Hour)
			b := storageJob(t, s, "b", StatusSucceeded, 30*24*time.Hour)
			switch kind {
			case "cycle":
				a.ResumeOf = b.ID
				b.ResumeOf = a.ID
				s.Save(a)
				s.Save(b)
			case "corrupt":
				a.ResumeOf = b.ID
				s.Save(a)
				os.WriteFile(filepath.Join(s.Dir(b.ID), "state.json"), []byte("bad"), 0600)
			case "symlink-state":
				a.ResumeOf = b.ID
				s.Save(a)
				os.Remove(filepath.Join(s.Dir(b.ID), "state.json"))
				os.Symlink(filepath.Join(s.Dir(a.ID), "state.json"), filepath.Join(s.Dir(b.ID), "state.json"))
			}
			p, err := s.PlanStorage(context.Background(), PruneOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if i := storageItem(p, "jobs/a"); i == nil || !i.Protected {
				t.Fatalf("unsafe chain eligible: %+v", i)
			}
		})
	}
}
func TestStoragePrivateSessionFallbacksAndOrphanPlan(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := storageStore(t)
	home := s.storageHome()
	for _, path := range []string{"harness-homes/kimi/sessions/ws/orphan/state.json", "harness-homes/agy/.gemini/antigravity-cli/conversations/orphan.pb"} {
		p := filepath.Join(home, path)
		os.MkdirAll(filepath.Dir(p), 0700)
		os.WriteFile(p, []byte("session"), 0600)
	}
	db := filepath.Join(home, "harness-homes/opencode/share/opencode/opencode.db")
	os.MkdirAll(filepath.Dir(db), 0700)
	os.WriteFile(db, []byte("fake database"), 0600)
	p, err := s.PlanStorage(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	native := storageItem(p, "harness-homes/opencode/share/opencode/opencode.db")
	if native == nil || native.Protected {
		t.Fatalf("orphan native sweep missing: %+v", native)
	}
	r, err := s.PruneStorage(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"harness-homes/kimi/sessions/ws/orphan", "harness-homes/agy/.gemini/antigravity-cli/conversations/orphan.pb"} {
		if _, err = os.Lstat(filepath.Join(home, path)); !os.IsNotExist(err) {
			t.Fatalf("session not removed: %s %+v %v", path, r, err)
		}
	}
	if len(r.Failures) == 0 {
		t.Fatal("missing opencode must be reported")
	}
}

func TestStorageExplicitWorktreeUsesDiscard(t *testing.T) {
	s := storageStore(t)
	source := repo(t)
	st, err := s.CreateRun("worktree", Request{Harness: "fake", Access: "write", Cwd: source}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusSucceeded
	st.FinishedAt = time.Now().Add(-30 * 24 * time.Hour)
	s.Save(st)
	atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, Status: st.Status})
	run, err := s.PruneStorage(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(st.Worktree.Path); err != nil {
		t.Fatalf("automatic worktree deletion: %+v %v", run, err)
	}
	run, err = s.PruneStorage(context.Background(), PruneOptions{IncludeWorktrees: true})
	if err != nil || len(run.Failures) > 0 {
		t.Fatalf("%+v %v", run, err)
	}
	if _, err = os.Lstat(s.Dir(st.ID)); !os.IsNotExist(err) {
		t.Fatalf("job not removed: %v", err)
	}
	list, err := runGit(source, "worktree", "list", "--porcelain")
	if err != nil || strings.Contains(string(list), st.Worktree.Path) {
		t.Fatalf("discard did not unregister worktree: %s %v", list, err)
	}
}

func TestStorageTransferredWorktreeUsesCurrentHolder(t *testing.T) {
	s := storageStore(t)
	st, err := s.CreateRun("a-owner", Request{Harness: "fake", Access: "write", Cwd: repo(t)}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusSucceeded
	st.FinishedAt = time.Now().Add(-30 * 24 * time.Hour)
	s.Save(st)
	atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, Status: st.Status})
	next, err := s.WorktreeFrom("z-holder", st.Request, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	next.Status = StatusSucceeded
	next.FinishedAt = st.FinishedAt
	s.Save(next)
	atomicJSON(s.ResultPath(next.ID), Result{JobID: next.ID, Status: next.Status})
	run, err := s.PruneStorage(context.Background(), PruneOptions{IncludeWorktrees: true})
	if err != nil || len(run.Failures) > 0 || run.Removed[0].Items != 2 {
		t.Fatalf("%+v %v", run, err)
	}
}

func TestStoragePolicyFailsClosed(t *testing.T) {
	for _, data := range []string{`null`, `{"cacheMaxByte":12}`, `{"olderThan":"0h"}`, `{"interval":"bad"}`, `{"cacheMaxBytes":-1}`} {
		t.Run(data, func(t *testing.T) {
			s := storageStore(t)
			os.WriteFile(filepath.Join(s.storageHome(), "storage.json"), []byte(data), 0600)
			if _, err := s.PlanStorage(context.Background(), PruneOptions{}); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}

func TestStorageStatusBudgetDoesNotLimitExplicitPlans(t *testing.T) {
	s := storageStore(t)
	storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	for i := range 12 {
		os.WriteFile(filepath.Join(s.Dir("old"), fmt.Sprintf("artifact-%d", i)), []byte("data"), 0600)
	}
	bounded, err := s.PlanStorage(context.Background(), PruneOptions{scanLimit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded.Failures) == 0 {
		t.Fatal("bounded status scan did not report its limit")
	}
	plan, err := s.PlanStorage(context.Background(), PruneOptions{})
	if err != nil || len(plan.Failures) > 0 {
		t.Fatalf("explicit plan inherited polling limit: %+v %v", plan, err)
	}
	if item := storageItem(plan, "jobs/old"); item == nil || item.Protected {
		t.Fatalf("full plan did not authorize old job: %+v", item)
	}
}
func TestStorageSizeBudgetStillListsAllStaleWorktrees(t *testing.T) {
	s := storageStore(t)
	for _, id := range []string{"aa-large", "zz-stale"} {
		st := storageJob(t, s, id, StatusSucceeded, 30*24*time.Hour)
		st.Worktree = &Worktree{Owner: id, Path: filepath.Join(s.Dir(id), "wt")}
		s.Save(st)
	}
	for i := range 12 {
		os.WriteFile(filepath.Join(s.Dir("aa-large"), fmt.Sprintf("artifact-%d", i)), []byte("data"), 0600)
	}
	plan, err := s.PlanStorage(context.Background(), PruneOptions{scanLimit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.StaleWorktrees) != 2 {
		t.Fatalf("size scan hid stale worktrees: %+v", plan.StaleWorktrees)
	}
}

func TestStorageRecentTerminalProcessProtectsSharedData(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			s := storageStore(t)
			st := storageJob(t, s, "terminal", StatusSucceeded, time.Hour)
			if unknown {
				st.ProcessTrackingError = "process identity could not be determined"
			} else {
				identity, err := procx.Inspect(os.Getpid())
				if err != nil {
					t.Fatal(err)
				}
				st.Worker = &identity
			}
			s.Save(st)
			cache := filepath.Join(s.storageHome(), "cache/go-build/object")
			log := filepath.Join(s.storageHome(), "harness-homes/kimi/logs/old.log")
			for _, path := range []string{cache, log} {
				os.MkdirAll(filepath.Dir(path), 0700)
				os.WriteFile(path, []byte("keep"), 0600)
				old := time.Now().Add(-30 * 24 * time.Hour)
				os.Chtimes(path, old, old)
			}
			run, err := s.PruneStorage(context.Background(), PruneOptions{CacheMaxBytes: 1})
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{cache, log} {
				if _, err = os.Lstat(path); err != nil {
					t.Fatalf("live or uncertain terminal process lost shared data: %+v %v", run, err)
				}
			}
		})
	}
}

func TestStorageQueuedJobProtectsNativeSessionHomes(t *testing.T) {
	s := storageStore(t)
	st := storageJob(t, s, "old-native", StatusSucceeded, 30*24*time.Hour)
	st.Request.Harness = "kimi"
	st.HarnessHome = filepath.Join(s.storageHome(), "harness-homes/kimi")
	s.Save(st)
	atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, SessionID: "old-session"})
	storageJob(t, s, "queued", StatusQueued, time.Hour)
	called := false
	run, err := s.pruneStorage(context.Background(), PruneOptions{}, storageDeps{deleteSessions: func(context.Context, []State) error { called = true; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatalf("queued job's shared home was touched: %+v", run)
	}
	if _, err = s.Load(st.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStorageSessionIDCannotSelectAnotherHarnessCleanup(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := storageStore(t)
	path := filepath.Join(s.storageHome(), "harness-homes/kimi/sessions/ws/@orphan")
	os.MkdirAll(path, 0700)
	os.WriteFile(filepath.Join(path, "state.json"), []byte("session"), 0600)
	run, err := s.PruneStorage(context.Background(), PruneOptions{})
	if err != nil || len(run.Failures) > 0 {
		t.Fatalf("session id changed cleanup mechanism: %+v %v", run, err)
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("session was not deleted through its own harness: %v", err)
	}
}

func TestStorageOpencodeMajorMismatchNeverStartsServer(t *testing.T) {
	for _, tc := range []struct{ home, version string }{{"opencode", "2.1.0"}, {"opencode2", "1.19.0"}} {
		t.Run(tc.home, func(t *testing.T) {
			outside := t.TempDir()
			t.Setenv("PACTIFY_HOME", outside)
			s := storageStore(t)
			home := filepath.Join("harness-homes", tc.home, "share")
			db := filepath.Join(s.storageHome(), home, "opencode/opencode.db")
			os.MkdirAll(filepath.Dir(db), 0700)
			os.WriteFile(db, []byte("database"), 0600)
			bin := t.TempDir()
			marker := filepath.Join(bin, "server-started")
			script := "#!/bin/sh\nif [ \"$1\" = --version ]; then printf '%s\\n' " + shellQuote(tc.version) + "; exit 0; fi\nprintf started > " + shellQuote(marker) + "\n/bin/sleep 0.1\nexit 1\n"
			os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0700)
			t.Setenv("PATH", bin)
			root, err := s.openStorage()
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err = s.deletePrivateSessions(ctx, root, "opencode", home, []string{"ses_owned"}, nil); err == nil {
				t.Fatal("mismatched installed major accepted")
			}
			if _, err = os.Lstat(marker); !os.IsNotExist(err) {
				t.Fatalf("wrong binary was allowed to open a private database: %v", err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) > 0 {
				t.Fatalf("version probe touched another store: %v %v", entries, err)
			}
			matching := "1.19.0"
			if tc.home == "opencode2" {
				matching = "2.1.0"
			}
			os.WriteFile(filepath.Join(bin, "opencode"), []byte(strings.Replace(script, shellQuote(tc.version), shellQuote(matching), 1)), 0700)
			if err = s.verifyOpencodeStorage(ctx, root, home); err != nil {
				t.Fatalf("matching major rejected: %v", err)
			}
			os.Remove(db)
			os.Mkdir(db, 0700)
			if err = s.verifyOpencodeStorage(ctx, root, home); err == nil || !strings.Contains(err.Error(), "regular") {
				t.Fatalf("non-file database accepted: %v", err)
			}
		})
	}
}

func TestStorageResultOwnershipMustMatchJob(t *testing.T) {
	for _, result := range []Result{{JobID: "other", SessionID: "foreign-thread"}, {SessionID: "foreign-thread"}, {JobID: "old", Harness: "kimi", SessionID: "foreign-thread"}} {
		t.Run(fmt.Sprint(result), func(t *testing.T) {
			s := storageStore(t)
			storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
			atomicJSON(s.ResultPath("old"), result)
			called := false
			run, err := s.pruneStorage(context.Background(), PruneOptions{}, storageDeps{deleteSessions: func(context.Context, []State) error { called = true; return nil }})
			if err != nil {
				t.Fatal(err)
			}
			if called {
				t.Fatalf("unowned session deletion was attempted: %+v", run)
			}
			if _, err = s.Load("old"); err != nil {
				t.Fatal("unverified job was removed", err)
			}
		})
	}
}

func TestStorageJobSessionBytesAndDoctorSymlink(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := storageStore(t)
	st := storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	st.Request.Harness = "kimi"
	st.HarnessHome = filepath.Join(s.storageHome(), "harness-homes/kimi")
	s.Save(st)
	atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, SessionID: "old-session"})
	path := filepath.Join(st.HarnessHome, "sessions/ws/old-session/state.json")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("session-data"), 0600)
	r, err := s.PruneStorage(context.Background(), PruneOptions{})
	if err != nil || len(r.Failures) > 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Removed[2].Bytes < 12 || r.Removed[2].Items < 1 {
		t.Fatalf("session cleanup missing from status: %+v", r)
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "file"), []byte("external"), 0600)
	os.Symlink(outside, filepath.Join(s.storageHome(), "cache"))
	if _, err := cacheSize(filepath.Join(s.storageHome(), "cache")); err == nil {
		t.Fatal("doctor followed symlinked cache root")
	}
}
