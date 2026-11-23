package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestStorageAgyAnnotationCleanup(t *testing.T) {
	s := storageStore(t)
	t.Setenv("PATH", t.TempDir())
	agyBase := filepath.Join(s.storageHome(), "harness-homes/agy/.gemini/antigravity-cli")
	convDir := filepath.Join(agyBase, "conversations")
	annDir := filepath.Join(agyBase, "annotations")
	for _, d := range []string{convDir, annDir} {
		if e := os.MkdirAll(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	writeFile := func(path string, data []byte) {
		t.Helper()
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	agyJob := func(id, session string, age time.Duration) {
		t.Helper()
		st := storageJob(t, s, id, StatusSucceeded, age)
		st.Request.Harness = "agy"
		st.HarnessHome = filepath.Join(s.storageHome(), "harness-homes", "agy")
		if e := s.Save(st); e != nil {
			t.Fatal(e)
		}
		if e := atomicJSON(s.ResultPath(id), Result{JobID: id, Status: st.Status, SessionID: session}); e != nil {
			t.Fatal(e)
		}
	}

	const (
		orphanBothID  = "1605b9e8-e635-4a6f-af75-a0e97e809641"
		orphanAnnID   = "2605b9e8-e635-4a6f-af75-a0e97e809642"
		retainedID    = "3605b9e8-e635-4a6f-af75-a0e97e809643"
		expiredBothID = "4605b9e8-e635-4a6f-af75-a0e97e809644"
		expiredAnnID  = "5605b9e8-e635-4a6f-af75-a0e97e809645"
		dirAnnID      = "6605b9e8-e635-4a6f-af75-a0e97e809646"
		symAnnID      = "7605b9e8-e635-4a6f-af75-a0e97e809647"
	)

	// Orphan with both conversation and annotation.
	writeFile(filepath.Join(convDir, orphanBothID+".db"), make([]byte, 300))
	writeFile(filepath.Join(annDir, orphanBothID+".pbtxt"), make([]byte, 70))

	// Orphan with annotation only (e.g. left behind by earlier cleanup).
	writeFile(filepath.Join(annDir, orphanAnnID+".pbtxt"), make([]byte, 55))

	// Retained job with both conversation and annotation.
	agyJob("retained", retainedID, time.Hour)
	writeFile(filepath.Join(convDir, retainedID+".db"), make([]byte, 200))
	writeFile(filepath.Join(annDir, retainedID+".pbtxt"), make([]byte, 40))

	// Expired job with both conversation and annotation.
	agyJob("expired-both", expiredBothID, 30*24*time.Hour)
	writeFile(filepath.Join(convDir, expiredBothID+".db"), make([]byte, 400))
	writeFile(filepath.Join(annDir, expiredBothID+".pbtxt"), make([]byte, 80))

	// Expired job with annotation only.
	agyJob("expired-ann", expiredAnnID, 30*24*time.Hour)
	writeFile(filepath.Join(annDir, expiredAnnID+".pbtxt"), make([]byte, 65))

	// Unrecognized entries in annotations/ must never be treated as sessions or removed.
	ignoredFiles := []string{
		"not-a-uuid.pbtxt",
		"8605B9E8-e635-4a6f-af75-a0e97e809648.pbtxt", // uppercase hex
		"8605b9e8-e635-4a6f-af75-a0e97e809648.txt",
		"8605b9e8-e635-4a6f-af75-a0e97e809648.pbtxt.bak",
		"8605b9e8-e635-4a6f-af75-a0e97e809648",
		"8605b9e8e6354a6faf75a0e97e809648.pbtxt",
	}
	for _, name := range ignoredFiles {
		writeFile(filepath.Join(annDir, name), []byte("keep"))
	}
	// Subdirectory named like <uuid>.pbtxt and a regular nested subdir.
	writeFile(filepath.Join(annDir, dirAnnID+".pbtxt", "nested.txt"), []byte("keep"))
	writeFile(filepath.Join(annDir, "subdir", orphanBothID+".pbtxt"), []byte("keep"))

	// Symlink named like <uuid>.pbtxt pointing outside storage.
	outsideTarget := filepath.Join(t.TempDir(), "outside.pbtxt")
	writeFile(outsideTarget, []byte("outside"))
	if e := os.Symlink(outsideTarget, filepath.Join(annDir, symAnnID+".pbtxt")); e != nil {
		t.Fatal(e)
	}

	plan, e := s.PlanStorage(context.Background(), PruneOptions{})
	if e != nil {
		t.Fatal(e)
	}
	const annRelPrefix = "harness-homes/agy/.gemini/antigravity-cli/annotations/"
	const convRelPrefix = "harness-homes/agy/.gemini/antigravity-cli/conversations/"

	for _, tc := range []struct {
		path    string
		session string
	}{
		{convRelPrefix + orphanBothID + ".db", orphanBothID},
		{annRelPrefix + orphanBothID + ".pbtxt", orphanBothID},
		{annRelPrefix + orphanAnnID + ".pbtxt", orphanAnnID},
	} {
		it := storageItem(plan, tc.path)
		if it == nil || it.Protected || it.Session != tc.session || it.Reason != "orphan session in private harness home" {
			t.Fatalf("expected reclaimable orphan %s: %+v", tc.path, it)
		}
	}
	for _, path := range []string{
		convRelPrefix + retainedID + ".db",
		annRelPrefix + retainedID + ".pbtxt",
		convRelPrefix + expiredBothID + ".db",
		annRelPrefix + expiredBothID + ".pbtxt",
		annRelPrefix + expiredAnnID + ".pbtxt",
	} {
		it := storageItem(plan, path)
		if it == nil || !it.Protected || it.Reason != "session referenced by an existing job" {
			t.Fatalf("expected job-referenced item protected %s: %+v", path, it)
		}
	}
	for _, name := range ignoredFiles {
		it := storageItem(plan, annRelPrefix+name)
		if it == nil || !it.Protected || it.Session != "" {
			t.Fatalf("unrecognized annotation entry should stay protected without session: %s -> %+v", name, it)
		}
	}
	if it := storageItem(plan, annRelPrefix+symAnnID+".pbtxt"); it == nil || !it.Protected || it.Session != "" || it.Reason != "symlink protected" {
		t.Fatalf("symlink annotation should be protected: %+v", it)
	}
	if it := storageItem(plan, annRelPrefix+dirAnnID+".pbtxt/nested.txt"); it == nil || !it.Protected || it.Session != "" {
		t.Fatalf("directory annotation should not be recognized as session: %+v", it)
	}
	if it := storageItem(plan, annRelPrefix+"subdir/"+orphanBothID+".pbtxt"); it == nil || !it.Protected || it.Session != "" {
		t.Fatalf("nested annotation should not be recognized as session: %+v", it)
	}

	run, e := s.PruneStorage(context.Background(), PruneOptions{})
	if e != nil || len(run.Failures) > 0 {
		t.Fatalf("prune failed: %+v %v", run, e)
	}

	// Deleted files: orphanBoth (conv+ann), orphanAnn (ann), expiredBoth (conv+ann), expiredAnn (ann)
	// => 6 files, 4 sessions, 300+70+55+400+80+65 = 970 bytes.
	got := run.Removed[2]
	if got.Items != 6 || got.Sessions != 4 || got.Bytes != 970 {
		t.Fatalf("unexpected harness-homes accounting: %+v", got)
	}

	for _, removed := range []string{
		filepath.Join(convDir, orphanBothID+".db"),
		filepath.Join(annDir, orphanBothID+".pbtxt"),
		filepath.Join(annDir, orphanAnnID+".pbtxt"),
		filepath.Join(convDir, expiredBothID+".db"),
		filepath.Join(annDir, expiredBothID+".pbtxt"),
		filepath.Join(annDir, expiredAnnID+".pbtxt"),
	} {
		if _, e := os.Lstat(removed); !os.IsNotExist(e) {
			t.Fatalf("expected removed %s, stat err=%v", removed, e)
		}
	}
	for _, kept := range []string{
		filepath.Join(convDir, retainedID+".db"),
		filepath.Join(annDir, retainedID+".pbtxt"),
		filepath.Join(annDir, dirAnnID+".pbtxt", "nested.txt"),
		filepath.Join(annDir, "subdir", orphanBothID+".pbtxt"),
		filepath.Join(annDir, symAnnID+".pbtxt"),
		outsideTarget,
	} {
		if _, e := os.Lstat(kept); e != nil {
			t.Fatalf("expected kept %s, stat err=%v", kept, e)
		}
	}
	for _, name := range ignoredFiles {
		if _, e := os.Lstat(filepath.Join(annDir, name)); e != nil {
			t.Fatalf("ignored annotation %s removed: %v", name, e)
		}
	}
}

func TestStorageAgyAnnotationsProtectedWhenBusy(t *testing.T) {
	s := storageStore(t)
	t.Setenv("PATH", t.TempDir())
	agyBase := filepath.Join(s.storageHome(), "harness-homes/agy/.gemini/antigravity-cli")
	convPath := filepath.Join(agyBase, "conversations", "1605b9e8-e635-4a6f-af75-a0e97e809641.db")
	annPath := filepath.Join(agyBase, "annotations", "1605b9e8-e635-4a6f-af75-a0e97e809641.pbtxt")
	annOnlyPath := filepath.Join(agyBase, "annotations", "2605b9e8-e635-4a6f-af75-a0e97e809642.pbtxt")
	for _, p := range []string{convPath, annPath, annOnlyPath} {
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte("busy"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	storageJob(t, s, "active", StatusRunning, 0)

	plan, e := s.PlanStorage(context.Background(), PruneOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for _, rel := range []string{
		"harness-homes/agy/.gemini/antigravity-cli/conversations/1605b9e8-e635-4a6f-af75-a0e97e809641.db",
		"harness-homes/agy/.gemini/antigravity-cli/annotations/1605b9e8-e635-4a6f-af75-a0e97e809641.pbtxt",
		"harness-homes/agy/.gemini/antigravity-cli/annotations/2605b9e8-e635-4a6f-af75-a0e97e809642.pbtxt",
	} {
		it := storageItem(plan, rel)
		if it == nil || !it.Protected || it.Reason != "running or queued jobs protect shared caches and homes" {
			t.Fatalf("expected busy protection for %s: %+v", rel, it)
		}
	}
	run, e := s.PruneStorage(context.Background(), PruneOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{convPath, annPath, annOnlyPath} {
		if _, e := os.Lstat(p); e != nil {
			t.Fatalf("busy prune removed %s: %v (run=%+v)", p, e, run)
		}
	}
}

func TestStorageAgyAnnotationVerificationAndSafety(t *testing.T) {
	s := storageStore(t)
	home := "harness-homes/agy"
	agyBase := filepath.Join(s.storageHome(), home, ".gemini", "antigravity-cli")
	if e := os.MkdirAll(filepath.Join(agyBase, "conversations"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(agyBase, "annotations"), 0700); e != nil {
		t.Fatal(e)
	}
	r, e := s.openStorage()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()

	// Annotation-only ID passes verification and deletes cleanly with receipt.
	const annOnlyID = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	annOnlyFile := filepath.Join(agyBase, "annotations", annOnlyID+".pbtxt")
	if e := os.WriteFile(annOnlyFile, make([]byte, 42), 0600); e != nil {
		t.Fatal(e)
	}
	if e := storageVerifyAgy(context.Background(), r, home, []string{annOnlyID}); e != nil {
		t.Fatalf("annotation-only preflight failed: %v", e)
	}
	removed, e := s.deletePrivateSessionsMeasured(context.Background(), r, "agy", home, []string{annOnlyID}, nil)
	if e != nil || removed.Items != 1 || removed.Sessions != 1 || removed.Bytes != 42 {
		t.Fatalf("annotation-only delete accounting: %+v %v", removed, e)
	}
	// Retry succeeds via receipt after files are gone.
	if e := storageVerifyAgy(context.Background(), r, home, []string{annOnlyID}); e != nil {
		t.Fatalf("annotation-only receipt retry verify failed: %v", e)
	}
	if e := s.deletePrivateSessions(context.Background(), r, "agy", home, []string{annOnlyID}, nil); e != nil {
		t.Fatalf("annotation-only receipt retry delete failed: %v", e)
	}

	// Missing UUID with no files and no receipt still fails both preflight and deletion.
	const missingID = "b1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	if e := storageVerifyAgy(context.Background(), r, home, []string{missingID}); e == nil || !strings.Contains(e.Error(), "has no verified private session files") {
		t.Fatalf("missing session preflight error: %v", e)
	}
	if e := s.deletePrivateSessions(context.Background(), r, "agy", home, []string{missingID}, nil); e == nil || !strings.Contains(e.Error(), "has no verified private session files") {
		t.Fatalf("missing session delete error: %v", e)
	}

	// Symlink at annotations/<uuid>.pbtxt fails closed before deleting conversation.
	const symID = "c1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	convFile := filepath.Join(agyBase, "conversations", symID+".db")
	if e := os.WriteFile(convFile, []byte("conv"), 0600); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(t.TempDir(), "secret.pbtxt")
	if e := os.WriteFile(target, []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, filepath.Join(agyBase, "annotations", symID+".pbtxt")); e != nil {
		t.Fatal(e)
	}
	if e := storageVerifyAgy(context.Background(), r, home, []string{symID}); e == nil || !strings.Contains(e.Error(), "symlink protected") {
		t.Fatalf("symlink annotation preflight should fail: %v", e)
	}
	if _, e := s.deletePrivateSessionsMeasured(context.Background(), r, "agy", home, []string{symID}, nil); e == nil || !strings.Contains(e.Error(), "symlink protected") {
		t.Fatalf("symlink annotation delete should fail: %v", e)
	}
	if _, e := os.Stat(convFile); e != nil {
		t.Fatalf("conversation deleted despite symlink annotation: %v", e)
	}
	if _, e := os.Stat(target); e != nil {
		t.Fatalf("symlink target deleted: %v", e)
	}

	// Subdirectory at annotations/<uuid>.pbtxt does not count as a verified session file and is never deleted.
	const dirOnlyID = "d1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	dirAnn := filepath.Join(agyBase, "annotations", dirOnlyID+".pbtxt")
	if e := os.MkdirAll(dirAnn, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dirAnn, "keep.txt"), []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := storageVerifyAgy(context.Background(), r, home, []string{dirOnlyID}); e == nil || !strings.Contains(e.Error(), "has no verified private session files") {
		t.Fatalf("directory annotation should not verify as session file: %v", e)
	}
	if _, e := os.Stat(filepath.Join(dirAnn, "keep.txt")); e != nil {
		t.Fatalf("directory annotation modified: %v", e)
	}
}
