package sub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageAutomaticIntervalAndPolicy(t *testing.T) {
	s := storageStore(t)
	storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	r, err := s.PruneStorageDue(context.Background())
	if err != nil || r.StartedAt.IsZero() {
		t.Fatalf("%+v %v", r, err)
	}
	before, err := os.ReadFile(filepath.Join(s.storageHome(), "storage-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.PruneStorageDue(context.Background())
	if err != nil || !r.StartedAt.IsZero() {
		t.Fatalf("run twice: %+v %v", r, err)
	}
	after, _ := os.ReadFile(filepath.Join(s.storageHome(), "storage-status.json"))
	if string(before) != string(after) {
		t.Fatal("skipped run rewrote status")
	}
	os.WriteFile(filepath.Join(s.storageHome(), "storage.json"), []byte(`{"olderThan":"48h","cacheMaxBytes":1000000000,"interval":"1ms"}`), 0600)
	time.Sleep(3 * time.Millisecond)
	p, err := s.PlanStorage(context.Background(), PruneOptions{})
	if err != nil || p.Policy.CacheMaxBytes != 1000000000 {
		t.Fatalf("%+v %v", p, err)
	}
	r, err = s.PruneStorageDue(context.Background())
	if err != nil || r.StartedAt.IsZero() {
		t.Fatalf("configured interval ignored: %+v %v", r, err)
	}
}
func TestStorageDoctorReportsStaleAndLastRun(t *testing.T) {
	s := storageStore(t)
	st := storageJob(t, s, "stale", StatusSucceeded, 30*24*time.Hour)
	st.Worktree = &Worktree{Owner: st.ID}
	s.Save(st)
	s.PruneStorage(context.Background(), PruneOptions{})
	checks := doctorStorageChecks(s, func(string) (uint64, error) { return 10 << 30, nil })
	names := map[string]DoctorCheck{}
	for _, c := range checks {
		names[c.Name] = c
	}
	if names["Stale worktrees"].Status != "WARN" || names["Last prune"].Reason == "" || names["Jobs"].Reason == "" || names["Harness homes"].Reason == "" {
		t.Fatalf("checks=%+v", checks)
	}
}

func TestStorageAutomaticDefersSharedCleanupUntilIdle(t *testing.T) {
	s := storageStore(t)
	st := storageJob(t, s, "active", StatusRunning, 30*24*time.Hour)
	run, err := s.PruneStorageDue(context.Background())
	if err != nil || !run.StartedAt.IsZero() {
		t.Fatalf("busy automatic run consumed interval: %+v %v", run, err)
	}
	if _, err = os.Lstat(filepath.Join(s.storageHome(), "storage-status.json")); !os.IsNotExist(err) {
		t.Fatalf("busy run recorded status: %v", err)
	}
	st.Status = StatusSucceeded
	s.Save(st)
	run, err = s.PruneStorageDue(context.Background())
	if err != nil || run.StartedAt.IsZero() {
		t.Fatalf("idle run not attempted: %+v %v", run, err)
	}
}

func TestStorageFailedPlanStillRecordsRun(t *testing.T) {
	s := storageStore(t)
	os.WriteFile(filepath.Join(s.storageHome(), "storage.json"), []byte(`{"olderThan":"invalid"}`), 0600)
	run, err := s.PruneStorage(context.Background(), PruneOptions{})
	if err == nil || run.StartedAt.IsZero() || run.EndedAt.IsZero() || len(run.Failures) == 0 {
		t.Fatalf("failed run was not recorded: %+v %v", run, err)
	}
	if _, err = os.Lstat(filepath.Join(s.storageHome(), "storage-status.json")); err != nil {
		t.Fatal(err)
	}
}

func TestStorageManualEmptyHomeRecordsRun(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "missing", "jobs")}
	run, err := s.PruneStorage(context.Background(), PruneOptions{})
	if err != nil || run.StartedAt.IsZero() || run.EndedAt.IsZero() {
		t.Fatalf("empty home run missing: %+v %v", run, err)
	}
	if _, err = os.Lstat(filepath.Join(s.storageHome(), "storage-status.json")); err != nil {
		t.Fatal(err)
	}
}

func TestStorageScheduledRunStopsWhenTriggerDisappears(t *testing.T) {
	s := storageStore(t)
	storageJob(t, s, "trigger", StatusSucceeded, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.PruneStorageAfterJob(ctx, "trigger"); done <- err }()
	time.Sleep(30 * time.Millisecond)
	if err := os.RemoveAll(s.storageHome()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("scheduled cleanup outlived its triggering job")
	}
	if _, err := os.Lstat(s.storageHome()); !os.IsNotExist(err) {
		t.Fatalf("scheduled cleanup recreated removed storage: %v", err)
	}
}

func TestStorageMaintenanceRejectsLinkedMetadata(t *testing.T) {
	for _, kind := range []string{"job", "state", "sibling"} {
		t.Run(kind, func(t *testing.T) {
			s := storageStore(t)
			st := storageJob(t, s, "trigger", StatusSucceeded, time.Hour)
			id := st.ID
			if kind == "sibling" {
				id = "linked"
				st = storageJob(t, s, id, StatusSucceeded, time.Hour)
			}
			outside := t.TempDir()
			atomicJSON(filepath.Join(outside, "state.json"), st)
			if kind == "job" {
				os.RemoveAll(s.Dir(id))
				os.Symlink(outside, s.Dir(id))
			} else {
				os.Remove(filepath.Join(s.Dir(id), "state.json"))
				os.Symlink(filepath.Join(outside, "state.json"), filepath.Join(s.Dir(id), "state.json"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := s.PruneStorageAfterJob(ctx, "trigger")
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("maintenance followed linked metadata: %v", err)
			}
		})
	}
}
