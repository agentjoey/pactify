package sub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// agy 1.4.x stores conversations as <session>.db; a retained job's database must
// be kept and an expired job's database must be deleted with the job.
func TestStorageAgyConversationDatabases(t *testing.T) {
	s := storageStore(t)
	t.Setenv("PATH", t.TempDir())
	dir := filepath.Join(s.storageHome(), "harness-homes/agy/.gemini/antigravity-cli/conversations")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	agyJob := func(id, session string, age time.Duration) {
		st := storageJob(t, s, id, StatusSucceeded, age)
		st.Request.Harness = "agy"
		st.HarnessHome = filepath.Join(s.storageHome(), "harness-homes", "agy")
		if e := s.Save(st); e != nil {
			t.Fatal(e)
		}
		if e := atomicJSON(s.ResultPath(id), Result{JobID: id, Status: st.Status, SessionID: session}); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(dir, session+".db"), []byte(id), 0600); e != nil {
			t.Fatal(e)
		}
	}
	agyJob("retained", "6ce3d634-bde2-4d6e-a24f-11abdbe70432", time.Hour)
	agyJob("expired", "f9592e9c-5ee8-446f-be12-9f8f8e486101", 30*24*time.Hour)
	plan, e := s.PlanStorage(context.Background(), PruneOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for _, l := range plan.Locations {
		for _, i := range l.Items {
			if filepath.Base(i.Path) == "6ce3d634-bde2-4d6e-a24f-11abdbe70432.db" && !i.Protected {
				t.Fatalf("retained agy conversation planned for removal: %+v", i)
			}
		}
	}
	run, e := s.PruneStorage(context.Background(), PruneOptions{})
	if e != nil || len(run.Failures) > 0 {
		t.Fatalf("prune: %+v %v", run, e)
	}
	if _, e := os.Stat(filepath.Join(dir, "6ce3d634-bde2-4d6e-a24f-11abdbe70432.db")); e != nil {
		t.Fatalf("retained agy conversation deleted: %v", e)
	}
	if _, e := os.Stat(filepath.Join(dir, "f9592e9c-5ee8-446f-be12-9f8f8e486101.db")); !os.IsNotExist(e) {
		t.Fatalf("expired agy conversation kept: %v", e)
	}
}

func TestStorageCacheKeepsToolLockFiles(t *testing.T) {
	s := storageStore(t)
	for _, p := range []string{"cache/go-mod/cache/lock", "cache/npm/_locks/x.lock", "cache/go-build/00/entry"} {
		full := filepath.Join(s.storageHome(), p)
		if e := os.MkdirAll(filepath.Dir(full), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(full, make([]byte, 100), 0600); e != nil {
			t.Fatal(e)
		}
	}
	run, e := s.PruneStorage(context.Background(), PruneOptions{CacheMaxBytes: 1})
	if e != nil || len(run.Failures) > 0 {
		t.Fatalf("prune: %+v %v", run, e)
	}
	for _, p := range []string{"cache/go-mod/cache/lock", "cache/npm/_locks/x.lock"} {
		if _, e := os.Stat(filepath.Join(s.storageHome(), p)); e != nil {
			t.Fatalf("lock file %s evicted: %v", p, e)
		}
	}
	if _, e := os.Stat(filepath.Join(s.storageHome(), "cache/go-build/00/entry")); !os.IsNotExist(e) {
		t.Fatalf("ordinary cache entry kept: %v", e)
	}
}
