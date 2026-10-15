package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func resumeCLIJob(t *testing.T, store sub.Store, id, cwd, harness, session string, status sub.Status, finished int64) sub.State {
	t.Helper()
	cwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Create(id, sub.Request{Harness: harness, Cwd: cwd, Brief: "Title " + id, Access: "write"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Terminal(status) {
		if err := store.Finish(id, &sub.Result{Status: status, SessionID: session}, "", time.Unix(finished, 0)); err != nil {
			t.Fatal(err)
		}
	} else {
		st.Status = status
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	st, _ = store.Load(id)
	return st
}

func TestSubResumeSpecifiedJob(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	store, _ := sub.DefaultStore()
	old := "20261015-010000-af34c8"
	resumeCLIJob(t, store, old, repo, "fake", "older-session", sub.StatusSucceeded, 1)
	resumeCLIJob(t, store, "20261015-020000-bb1234", repo, "fake", "newer-session", sub.StatusSucceeded, 2)
	for _, args := range [][]string{
		{"fake", "--resume=" + old},
		{"fake", "--resume", old, "--write"},
		{"--resume", "af34", "--write"},
		{"--resume=20261015-01"},
		{"--resume", "fake", "--write"}, // Existing positional harness after a bare flag.
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, code := runCLI(t, bin, repo, "continue", append(append([]string{"sub", "run"}, args...), "--wait", "10s", "--json")...)
			var r sub.Result
			if code != 0 || json.Unmarshal([]byte(out), &r) != nil {
				t.Fatalf("code=%d out=%s", code, out)
			}
			st, err := store.Load(r.JobID)
			if err != nil || st.Request.Harness != "fake" || st.ResumeOf == "" {
				t.Fatalf("state=%+v err=%v", st, err)
			}
			if !strings.Contains(strings.Join(args, " "), "--resume fake") && (st.ResumeOf != old || st.Request.Resume != "older-session") {
				t.Fatalf("wrong parent: %+v", st)
			}
		})
	}
}

func TestSubResumeValidation(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	store, _ := sub.DefaultStore()
	resumeCLIJob(t, store, "done", repo, "fake", "session", sub.StatusSucceeded, 1)
	resumeCLIJob(t, store, "running", repo, "fake", "", sub.StatusRunning, 0)
	resumeCLIJob(t, store, "queued", repo, "fake", "", sub.StatusQueued, 0)
	resumeCLIJob(t, store, "no-session", repo, "fake", "", sub.StatusSucceeded, 2)
	for _, id := range []string{"20261015-010000-abc111", "20261015-020000-abc222"} {
		resumeCLIJob(t, store, id, repo, "fake", "session", sub.StatusSucceeded, 3)
	}
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"--resume=missing"}, []string{"missing", "not found"}},
		{[]string{"codex", "--resume=done"}, []string{"done", "fake", "codex"}},
		{[]string{"--resume=running"}, []string{"running", "not finished"}},
		{[]string{"--resume=queued"}, []string{"queued", "not finished"}},
		{[]string{"--resume=no-session"}, []string{"no-session", "session id"}},
		{[]string{"--resume=abc"}, []string{"ambiguous", "20261015-010000-abc111", "20261015-020000-abc222"}},
		{[]string{"--resume=done", "--fresh"}, []string{"--resume and --fresh"}},
		{[]string{"--resume="}, []string{"job id"}},
		{[]string{"--resume=../done"}, []string{"invalid job id"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out, code := runCLI(t, bin, repo, "continue", append([]string{"sub", "run"}, tc.args...)...)
			if code != 2 {
				t.Fatalf("code=%d out=%s", code, out)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q: %s", want, out)
				}
			}
		})
	}
}

func TestSubResumeListsCandidatesOnStderr(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	store, _ := sub.DefaultStore()
	resumeCLIJob(t, store, "older", repo, "fake", "old-session", sub.StatusSucceeded, 1)
	resumeCLIJob(t, store, "newer", repo, "fake", "new-session", sub.StatusSucceeded, 2)
	resumeCLIJob(t, store, "other-cwd", t.TempDir(), "fake", "other-session", sub.StatusSucceeded, 3)
	resumeCLIJob(t, store, "other-harness", repo, "codex", "other-session", sub.StatusSucceeded, 4)
	for _, wantList := range []bool{true, false} {
		c := exec.Command(bin, "sub", "run", "fake", "--resume", "--write", "--wait", "10s", "--json")
		c.Dir, c.Stdin = repo, strings.NewReader("continue")
		var out, stderr bytes.Buffer
		c.Stdout, c.Stderr = &out, &stderr
		if err := c.Run(); err != nil {
			t.Fatalf("run: %v %s %s", err, out.String(), stderr.String())
		}
		var r sub.Result
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err, out.String())
		}
		st, _ := store.Load(r.JobID)
		if wantList {
			if st.ResumeOf != "newer" {
				t.Fatalf("selected %+v", st)
			}
			for _, want := range []string{"older", "Title older", "1970-01-01T00:00:01Z", "newer", "Title newer", "1970-01-01T00:00:02Z", "Selected newer", "--resume <job-id>"} {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr missing %q: %s", want, stderr.String())
				}
			}
			if strings.Contains(stderr.String(), "other-") {
				t.Errorf("listed unrelated jobs: %s", stderr.String())
			}
		} else if stderr.Len() != 0 {
			t.Errorf("single candidate: %s", stderr.String())
		}
		// Leave only the newly finished job eligible for the second invocation.
		for _, id := range []string{"older", "newer"} {
			if err := os.RemoveAll(store.Dir(id)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSubResumeOlderWorktreeFromAnotherDirectory(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo, err := filepath.EvalSymlinks(gitRepoForCLI(t))
	if err != nil {
		t.Fatal(err)
	}
	store, _ := sub.DefaultStore()
	var jobs []sub.State
	for _, id := range []string{"older-wt", "newer-wt"} {
		st, err := store.CreateRun(id, sub.Request{Harness: "fake", Cwd: repo, Brief: id, Access: "write"}, true, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(id, &sub.Result{Status: sub.StatusSucceeded, SessionID: id + "-session"}, "", time.Now()); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, st)
	}
	out, code := runCLI(t, bin, t.TempDir(), "rework older", "sub", "run", "--resume", "older", "--write", "--worktree", "--wait", "10s", "--json")
	var r sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil {
		t.Fatalf("resume: %d %s", code, out)
	}
	next, err := store.Load(r.JobID)
	if err != nil || next.ResumeOf != "older-wt" || next.Request.Resume != "older-wt-session" || next.Request.Cwd != repo || r.WorktreePath != jobs[0].Worktree.Path {
		t.Fatalf("wrong target: %+v %+v %v", next, r, err)
	}
	if _, err := os.Stat(filepath.Join(jobs[1].Worktree.Path, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("newer worktree was modified: %v", err)
	}
	for _, st := range jobs {
		if err := store.DiscardWorktree(st.ID); err != nil {
			t.Fatal(err)
		}
	}
	out, code = runCLI(t, bin, repo, "continue", "sub", "run", "--resume=older-wt")
	if code != 2 || !strings.Contains(out, "discarded") {
		t.Fatalf("discarded: %d %s", code, out)
	}
}
