package sub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStorageReviewHomeAndCreation(t *testing.T) {
	t.Run("linked home", func(t *testing.T) {
		s := storageStore(t)
		alias := filepath.Join(t.TempDir(), "home")
		if err := os.Symlink(s.storageHome(), alias); err != nil {
			t.Fatal(err)
		}
		if _, err := (Store{Root: filepath.Join(alias, "jobs")}).Create("new", Request{Harness: "codex"}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("gate bounded", func(t *testing.T) {
		s := storageStore(t)
		r, _ := s.openStorage()
		defer r.Close()
		release, err := storageLock(context.Background(), r, "storage-jobs.lock", syscall.LOCK_EX)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		done := make(chan error, 1)
		go func() { _, e := s.Create("new", Request{Harness: "codex"}); done <- e }()
		select {
		case e := <-done:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(2500 * time.Millisecond):
			t.Fatal("creation blocked by prune")
		}
	})
}
func TestStorageReviewUnrelatedDamageAndWorktreeNames(t *testing.T) {
	s := storageStore(t)
	storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	storageJob(t, s, "other", StatusSucceeded, 30*24*time.Hour)
	os.MkdirAll(s.Dir("broken"), 0700)
	os.WriteFile(filepath.Join(s.Dir("broken"), "state.json"), []byte(`{"resumeOf":"other",`), 0600)
	p, err := s.PlanStorage(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range p.Locations[0].Items {
		if filepath.Base(i.Path) == "old" && i.Protected {
			t.Fatalf("unrelated damage protects old job: %s", i.Reason)
		}
	}
}
func TestStorageReviewCredentialScope(t *testing.T) {
	for _, p := range []string{"jobs/job/tokenizer.go", "jobs/job/internal/credentiallinks", "cache/go-build/tokenizer"} {
		if storageCredential(p) {
			t.Errorf("not a credential: %s", p)
		}
	}
	if !storageCredential("harness-homes/kimi/logs/credentials/x") {
		t.Fatal("credential guard missing")
	}
}
func TestStorageReviewPolicyMinimum(t *testing.T) {
	for _, raw := range []string{`{"olderThan":"23h"}`, `{"cacheMaxBytes":999999999}`} {
		s := storageStore(t)
		os.WriteFile(filepath.Join(s.storageHome(), "storage.json"), []byte(raw), 0600)
		if _, err := s.PlanStorage(context.Background(), PruneOptions{}); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
func TestStorageReviewKimiIgnoresRegularWorkspaceEntries(t *testing.T) {
	s := storageStore(t)
	p := filepath.Join(s.storageHome(), "harness-homes/kimi/sessions")
	os.MkdirAll(p, 0700)
	os.WriteFile(filepath.Join(p, ".DS_Store"), nil, 0600)
	r, _ := s.openStorage()
	defer r.Close()
	if _, err := storageKimiSessions(r, "harness-homes/kimi", []string{"session"}); err != nil {
		t.Fatal(err)
	}
}

func TestStorageReviewPrivateHomeOwnership(t *testing.T) {
	s := storageStore(t)
	for _, h := range []string{"kimi", "opencode", "agy"} {
		rel := "harness-homes/" + h
		if h == "opencode" {
			rel += "/share"
		}
		home := filepath.Join(s.storageHome(), rel)
		os.MkdirAll(home, 0700)
		st := State{Request: Request{Harness: h}, HarnessHome: home}
		if _, e := s.privateHomeRel(st); e != nil {
			t.Fatal(e)
		}
		st.Request.WithUserConfig = true
		if _, e := s.privateHomeRel(st); e == nil {
			t.Fatal("accepted user config")
		}
		st.Request.WithUserConfig = false
		st.HarnessHome = t.TempDir()
		if _, e := s.privateHomeRel(st); e == nil {
			t.Fatal("accepted real/unknown home")
		}
	}
	st := storageJob(t, s, "retained", StatusSucceeded, time.Hour)
	st.Request.Harness = "opencode"
	st.HarnessHome = t.TempDir()
	s.Save(st)
	atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, Status: st.Status, SessionID: "keep"})
	r, _ := s.openStorage()
	defer r.Close()
	if _, e := s.storageOpencodeKeep(r, "harness-homes/opencode/share", nil); e == nil {
		t.Fatal("path mismatch dropped retained session")
	}
}
func TestStorageReviewStatelessDirectory(t *testing.T) {
	s := storageStore(t)
	os.MkdirAll(s.Dir("incomplete"), 0700)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(s.Dir("incomplete"), old, old)
	run, e := s.PruneStorage(context.Background(), PruneOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(s.Dir("incomplete")); !os.IsNotExist(e) {
		t.Fatalf("old incomplete directory retained: %+v %v", run, e)
	}
}
func TestStorageReviewJobChangedDuringSessionDeletion(t *testing.T) {
	for _, kind := range []string{"symlink", "swap", "new-resume"} {
		t.Run(kind, func(t *testing.T) {
			s := storageStore(t)
			storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
			outside := t.TempDir()
			os.WriteFile(filepath.Join(outside, "marker"), nil, 0600)
			run, e := s.pruneStorage(context.Background(), PruneOptions{}, storageDeps{deleteSessions: func(context.Context, []State) error {
				if kind == "new-resume" {
					st, err := s.Create("new", Request{Harness: "codex"})
					if err != nil {
						return err
					}
					st.ResumeOf = "old"
					return s.Save(st)
				}
				if err := os.Rename(s.Dir("old"), filepath.Join(s.storageHome(), "saved")); err != nil {
					return err
				}
				if kind == "symlink" {
					return os.Symlink(outside, s.Dir("old"))
				}
				return os.Mkdir(s.Dir("old"), 0700)
			}})
			if e != nil {
				t.Fatal(e)
			}
			if len(run.Failures) == 0 {
				t.Fatalf("changed chain deleted: %+v", run)
			}
			if _, e = os.Lstat(s.Dir("old")); e != nil {
				t.Fatalf("replacement deleted: %v", e)
			}
			if _, e = os.Stat(filepath.Join(outside, "marker")); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestStorageReviewSchedulerSkipsRecentAndDeduplicates(t *testing.T) {
	s := storageStore(t)
	storageJob(t, s, "trigger", StatusRunning, time.Hour)
	atomicJSON(filepath.Join(s.storageHome(), "storage-status.json"), StorageRun{StartedAt: time.Now()})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	run, e := s.PruneStorageAfterJob(ctx, "trigger")
	if e != nil || !run.StartedAt.IsZero() {
		t.Fatalf("not-due scheduler polled: %+v %v", run, e)
	}
	os.Remove(filepath.Join(s.storageHome(), "storage-status.json"))
	r, _ := s.openStorage()
	defer r.Close()
	release, e := storageLock(context.Background(), r, "storage-scheduler.lock", syscall.LOCK_EX)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if _, e = s.PruneStorageAfterJob(ctx2, "trigger"); e != nil {
		t.Fatalf("duplicate scheduler waited: %v", e)
	}
}

func TestStorageReviewTrashRecovery(t *testing.T) {
	s := storageStore(t)
	p := filepath.Join(s.storageHome(), "trash", "old-123")
	os.MkdirAll(p, 0700)
	os.WriteFile(filepath.Join(p, "artifact"), []byte("partial"), 0600)
	atomicJSON(p+".json", map[string]string{"path": "trash/old-123"})
	plan, err := s.PlanStorage(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	item := storageItem(plan, "trash/old-123")
	if item == nil || item.Protected || item.Bytes != 7 {
		t.Fatalf("trash recovery missing from preview: %+v", item)
	}
	run, e := s.PruneStorage(context.Background(), PruneOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(p); !os.IsNotExist(e) {
		t.Fatalf("partial deletion not retried: %+v %v", run, e)
	}
}

func TestStorageReviewWorktreeContainmentAndContent(t *testing.T) {
	for _, kind := range []string{"path", "owner", "content"} {
		t.Run(kind, func(t *testing.T) {
			s := storageStore(t)
			st, e := s.CreateRun("old", Request{Harness: "fake", Access: "write", Cwd: repo(t)}, true, false)
			if e != nil {
				t.Fatal(e)
			}
			st.Status = StatusSucceeded
			st.FinishedAt = time.Now().Add(-30 * 24 * time.Hour)
			outside := t.TempDir()
			if kind == "path" {
				outside = filepath.Join(outside, "other-worktree")
				if _, e := runGit(st.Worktree.OriginalRoot, "worktree", "add", "--detach", outside, "HEAD"); e != nil {
					t.Fatal(e)
				}
			}
			marker := filepath.Join(outside, "marker")
			os.WriteFile(marker, []byte("keep"), 0600)
			switch kind {
			case "path":
				st.Worktree.Path = outside
			case "owner":
				st.Worktree.Owner = "../jobs/old"
			case "content":
				os.MkdirAll(filepath.Join(st.Worktree.Path, "internal/credentiallinks"), 0700)
				os.WriteFile(filepath.Join(st.Worktree.Path, "tokenizer.go"), nil, 0600)
				os.Symlink("AGENTS.md", filepath.Join(st.Worktree.Path, "CLAUDE.md"))
			}
			s.Save(st)
			atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, Status: st.Status})
			run, e := s.PruneStorage(context.Background(), PruneOptions{IncludeWorktrees: true})
			if e != nil {
				t.Fatal(e)
			}
			if kind == "content" {
				if len(run.Failures) > 0 {
					t.Fatalf("ordinary source content blocked prune: %+v", run)
				}
				if _, e = os.Lstat(s.Dir(st.ID)); !os.IsNotExist(e) {
					t.Fatal("worktree job retained")
				}
			} else {
				r, err := s.openStorage()
				if err != nil {
					t.Fatal(err)
				}
				direct := StorageRun{Removed: []StorageRemoved{{}, {}, {}}}
				err = s.pruneStorageChain(context.Background(), r, []StorageItem{{Path: "jobs/old", Chain: "old"}}, PruneOptions{IncludeWorktrees: true}, storageDeps{deleteSessions: func(context.Context, []State) error {
					t.Error("unverified worktree reached session deletion")
					return nil
				}}, &direct)
				r.Close()
				if err == nil {
					t.Fatalf("unsafe worktree accepted: %+v", run)
				}
				if _, e = s.Load(st.ID); e != nil {
					t.Fatal("unsafe job removed")
				}
			}
			if _, e = os.Stat(marker); e != nil {
				t.Fatal("external worktree touched")
			}
		})
	}
}

func TestStorageReviewNewestFirstPartialFailure(t *testing.T) {
	s := storageStore(t)
	root := storageJob(t, s, "z-root", StatusSucceeded, 40*24*time.Hour)
	child := storageJob(t, s, "a-child", StatusSucceeded, 30*24*time.Hour)
	child.ResumeOf = root.ID
	child.FallbackFrom = root.ID
	root.FallbackTo = child.ID
	root.Worktree = &Worktree{Owner: root.ID, Holder: child.ID, Discarded: true}
	copy := *root.Worktree
	child.Worktree = &copy
	s.Save(root)
	s.Save(child)
	order := []string{}
	run, e := s.pruneStorage(context.Background(), PruneOptions{}, storageDeps{deleteSessions: func(context.Context, []State) error { return nil }, removeJob: func(r *os.Root, p string, info os.FileInfo) error {
		order = append(order, filepath.Base(p))
		if len(order) == 2 {
			return errors.New("injected removal failure")
		}
		return storageTrashJob(r, p, info)
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(order) != 2 || order[0] != child.ID {
		t.Fatalf("deletion order %v", order)
	}
	if len(run.Failures) == 0 {
		t.Fatal("failure not recorded")
	}
	if _, e = s.Load(root.ID); e != nil {
		t.Fatal("ancestor lost before child")
	}
	survivor, _ := s.Load(root.ID)
	if survivor.FallbackTo != "" || survivor.Worktree.Holder == child.ID {
		t.Fatal("survivor points at deleted descendant")
	}
	run, e = s.pruneStorage(context.Background(), PruneOptions{}, storageDeps{deleteSessions: func(context.Context, []State) error { return nil }})
	if e != nil || len(run.Failures) > 0 {
		t.Fatalf("retry failed: %+v %v", run, e)
	}
}

func TestStorageReviewResumeDoesNotBlockUnrelatedCreation(t *testing.T) {
	s := storageStore(t)
	st := storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	entered := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan StorageRun, 1)
	go func() {
		r, _ := s.pruneStorage(context.Background(), PruneOptions{}, storageDeps{deleteSessions: func(context.Context, []State) error { close(entered); <-resume; return nil }})
		done <- r
	}()
	<-entered
	start := time.Now()
	if _, e := s.Create("new", Request{Harness: "codex"}); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatal("network call held creation gate")
	}
	resumed := make(chan error, 1)
	go func() {
		_, e := s.withResumeLock(st, func(*SummaryIndex) (State, error) { t.Error("resumed pruning chain"); return State{}, nil })
		resumed <- e
	}()
	time.Sleep(40 * time.Millisecond)
	close(resume)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("resume and prune gate deadlocked")
	}
	select {
	case e := <-resumed:
		if e == nil {
			t.Fatal("resume accepted deleted chain")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("resume stuck")
	}
}

func TestStorageReviewPreflightsAllNativeHomesBeforeDeletion(t *testing.T) {
	for _, invalid := range []string{"kimi", "opencode"} {
		t.Run(invalid, func(t *testing.T) {
			s := storageStore(t)
			bin := t.TempDir()
			marker := filepath.Join(bin, "codex-called")
			os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\nprintf called > "+shellQuote(marker)+"\nexit 1\n"), 0700)
			t.Setenv("PATH", bin)
			states := []State{}
			for _, h := range []string{"codex", "agy", "kimi", "opencode"} {
				if invalid == "kimi" && h == "opencode" {
					continue
				}
				st := storageJob(t, s, h, StatusSucceeded, 30*24*time.Hour)
				st.Request.Harness = h
				rel := "harness-homes/" + h
				if h == "opencode" {
					rel += "/share"
				}
				st.HarnessHome = filepath.Join(s.storageHome(), rel)
				os.MkdirAll(st.HarnessHome, 0700)
				s.Save(st)
				atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, Status: st.Status, Harness: h, SessionID: "session"})
				states = append(states, st)
			}
			agy := filepath.Join(s.storageHome(), "harness-homes/agy/.gemini/antigravity-cli/conversations/session.pb")
			os.MkdirAll(filepath.Dir(agy), 0700)
			os.WriteFile(agy, []byte("native"), 0600)
			kimi := filepath.Join(s.storageHome(), "harness-homes/kimi/sessions/ws/session")
			os.MkdirAll(kimi, 0700)
			if invalid == "kimi" {
				os.WriteFile(filepath.Join(s.storageHome(), "harness-homes/kimi/session_index.jsonl"), []byte(`{"sessionId":"session","sessionDir":"/outside"}`), 0600)
			}
			if e := s.deleteStorageSessions(context.Background(), states); e == nil {
				t.Fatal("invalid local home accepted")
			}
			for _, p := range []string{agy, kimi} {
				if _, e := os.Lstat(p); e != nil {
					t.Fatalf("local session deleted before all preflights: %s %v", p, e)
				}
			}
			if _, e := os.Lstat(marker); !os.IsNotExist(e) {
				t.Fatalf("codex called before local validation: %v", e)
			}
		})
	}
}
func TestStorageReviewJSONRejectsFIFOWithoutBlocking(t *testing.T) {
	s := storageStore(t)
	p := filepath.Join(s.storageHome(), "storage.json")
	if e := syscall.Mkfifo(p, 0600); e != nil {
		t.Fatal(e)
	}
	r, _ := s.openStorage()
	defer r.Close()
	done := make(chan error, 1)
	go func() { done <- storageReadJSON(r, "storage.json", new(any)) }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(100 * time.Millisecond):
		// Unblock the reader on mutation failure so test cleanup remains bounded.
		f, _ := os.OpenFile(p, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if f != nil {
			f.Close()
		}
		t.Fatal("FIFO read blocked")
	}
}
func TestStorageReviewHomeAliasesAndOtherMajor(t *testing.T) {
	s := storageStore(t)
	home := filepath.Join(s.storageHome(), "harness-homes/opencode/share")
	os.MkdirAll(home, 0700)
	alias := filepath.Join(t.TempDir(), "alias")
	os.Symlink(home, alias)
	st := storageJob(t, s, "retained", StatusSucceeded, time.Hour)
	st.Request.Harness = "opencode"
	st.HarnessHome = alias
	s.Save(st)
	atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, Status: st.Status, SessionID: "keep"})
	if rel, e := s.privateHomeRel(st); e != nil || rel != "harness-homes/opencode/share" {
		t.Fatalf("alias rejected: %s %v", rel, e)
	}
	r, _ := s.openStorage()
	defer r.Close()
	keep, e := s.storageOpencodeKeep(r, "harness-homes/opencode/share", nil)
	if e != nil || !keep["keep"] {
		t.Fatalf("alias lost session: %v %v", keep, e)
	}
	keep, e = s.storageOpencodeKeep(r, "harness-homes/opencode2/share", nil)
	if e != nil || len(keep) != 0 {
		t.Fatalf("other major included: %v %v", keep, e)
	}
	st.Request.WithUserConfig = true
	st.HarnessHome = t.TempDir()
	s.Save(st)
	if _, e = s.storageOpencodeKeep(r, "harness-homes/opencode/share", nil); e != nil {
		t.Fatal("explicit user config should be excluded", e)
	}
}

func TestStorageReviewAgyPartialFailureRetry(t *testing.T) {
	s := storageStore(t)
	home := "harness-homes/agy"
	p := filepath.Join(s.storageHome(), home, ".gemini/antigravity-cli/conversations/session.pb")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, nil, 0600)
	r, _ := s.openStorage()
	defer r.Close()
	for range 2 {
		if e := s.deletePrivateSessions(context.Background(), r, "agy", home, []string{"session"}, nil); e != nil {
			t.Fatal("retry failed", e)
		}
	}
	if e := s.deletePrivateSessions(context.Background(), r, "agy", home, []string{"unknown"}, nil); e == nil {
		t.Fatal("unverified missing session accepted")
	}
}

func TestStorageReviewCodexProtocolPeer(t *testing.T) {
	capture := os.Getenv("AJ823_CODEX_CAPTURE")
	if capture == "" {
		return
	}
	f, e := os.OpenFile(capture, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		os.Exit(90)
	}
	defer f.Close()
	scanner := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(91)
		}
		if len(req.ID) == 0 {
			continue
		}
		if req.Method == "thread/delete" {
			f.Write(append(req.Params, '\n'))
		} else if req.Method != "initialize" {
			os.Exit(92)
		}
		out.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
	}
	os.Exit(0)
}
func TestStorageReviewCodexWholeChainExactIDs(t *testing.T) {
	s := storageStore(t)
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	bin := t.TempDir()
	capture := filepath.Join(bin, "requests")
	t.Setenv("AJ823_CODEX_CAPTURE", capture)
	os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\nexec "+shellQuote(binary)+" -test.run=^TestStorageReviewCodexProtocolPeer$ -- \"$@\"\n"), 0700)
	t.Setenv("PATH", bin)
	previous := ""
	priorSession := ""
	for i, id := range []string{"first", "second", "third"} {
		st := storageJob(t, s, id, StatusSucceeded, time.Duration(40-i)*24*time.Hour)
		st.ResumeOf = previous
		st.Request.Resume = priorSession
		s.Save(st)
		atomicJSON(s.ResultPath(id), Result{JobID: id, Status: st.Status, Harness: "codex", SessionID: "thread-" + id})
		previous = id
		priorSession = "thread-" + id
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, e := s.PruneStorage(ctx, PruneOptions{})
	if e != nil || len(run.Failures) > 0 {
		t.Fatalf("prune: %+v %v", run, e)
	}
	data, e := os.ReadFile(capture)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var p struct {
			ThreadID string `json:"threadId"`
		}
		if e = json.Unmarshal([]byte(line), &p); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, p.ThreadID)
	}
	if !reflect.DeepEqual(ids, []string{"thread-first", "thread-second", "thread-third"}) {
		t.Fatalf("wrong chain thread deletions: %v", ids)
	}
}

func TestStorageReviewDirectoryIdentityGuard(t *testing.T) {
	s := storageStore(t)
	st := storageJob(t, s, "old", StatusSucceeded, 30*24*time.Hour)
	r, e := s.openStorage()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	before, e := r.Lstat("jobs/old")
	if e != nil {
		t.Fatal(e)
	}
	os.Rename(s.Dir("old"), filepath.Join(s.storageHome(), "saved"))
	os.Mkdir(s.Dir("old"), 0700)
	s.Save(st)
	atomicJSON(s.ResultPath(st.ID), Result{JobID: st.ID, Status: st.Status})
	if e = s.storageChainUnchanged(r, []State{st}, map[string]os.FileInfo{"jobs/old": before}); e == nil {
		t.Fatal("same state in a different directory accepted")
	}
}

func TestStorageReviewLockRejectsSpecialFiles(t *testing.T) {
	s := storageStore(t)
	p := filepath.Join(s.storageHome(), "storage-jobs.lock")
	if e := syscall.Mkfifo(p, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := s.openStorage()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	release, e := storageLock(ctx, r, "storage-jobs.lock", syscall.LOCK_EX)
	if e == nil {
		release()
		t.Fatal("FIFO accepted as lifecycle lock")
	}
	if _, e = s.Create("new", Request{Harness: "codex"}); e != nil {
		t.Fatal("prune lock problem blocked creation", e)
	}
}

func TestStorageReviewReferencedStatelessDirectoryIsProtected(t *testing.T) {
	s := storageStore(t)
	os.MkdirAll(s.Dir("incomplete"), 0700)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(s.Dir("incomplete"), old, old)
	child := storageJob(t, s, "child", StatusSucceeded, 30*24*time.Hour)
	child.ResumeOf = "incomplete"
	s.Save(child)
	p, e := s.PlanStorage(context.Background(), PruneOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"jobs/incomplete", "jobs/child"} {
		i := storageItem(p, path)
		if i == nil || !i.Protected {
			t.Fatalf("uncertain chain member eligible: %+v", i)
		}
	}
}

func TestStorageReviewUnmarkedTrashIsProtected(t *testing.T) {
	for _, marker := range []string{"", "jobs/some-other-job"} {
		t.Run(marker, func(t *testing.T) {
			s := storageStore(t)
			p := filepath.Join(s.storageHome(), "trash/unknown")
			os.MkdirAll(p, 0700)
			os.WriteFile(filepath.Join(p, "artifact"), []byte("keep"), 0600)
			if marker != "" {
				atomicJSON(p+".json", map[string]string{"path": marker})
			}
			plan, e := s.PlanStorage(context.Background(), PruneOptions{})
			if e != nil {
				t.Fatal(e)
			}
			item := storageItem(plan, "trash/unknown")
			if item == nil || !item.Protected {
				t.Fatalf("unverified trash eligible: %+v", item)
			}
			if _, e = s.PruneStorage(context.Background(), PruneOptions{}); e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(filepath.Join(p, "artifact")); e != nil {
				t.Fatal("unmarked trash deleted")
			}
		})
	}
}

func TestStorageReviewTrashScanHonorsCancellation(t *testing.T) {
	s := storageStore(t)
	os.MkdirAll(filepath.Join(s.storageHome(), "trash/unknown"), 0700)
	r, e := s.openStorage()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := StoragePlan{Locations: []StorageLocation{{Name: "jobs"}}}
	scan := storageScanner{ctx, r, 10}
	s.planStorageTrash(&p, &scan)
	if len(p.Locations[0].Items) != 0 || len(p.Failures) == 0 {
		t.Fatalf("trash scan ignored cancellation: %+v", p)
	}
}
