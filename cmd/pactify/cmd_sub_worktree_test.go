package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

// Exercises the actual CLI, detached worker, git worktree and apply/discard path.
func TestSubWorktreeCLI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("user edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(resume bool) sub.Result {
		t.Helper()
		args := []string{"sub", "run", "fake", "--worktree", "--wait", "10s", "--json"}
		if resume {
			args = append(args, "--resume")
		}
		out, code := runCLI(t, bin, repo, "do it", args...)
		if code != 0 {
			t.Fatalf("run: %d %s", code, out)
		}
		var result sub.Result
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := run(false)
	store, _ := sub.DefaultStore()
	data, err := os.ReadFile(filepath.Join(store.Dir(first.JobID), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Worktree struct{ Path, BaseCommit string }
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	wt := state.Worktree.Path
	if wt != filepath.Join(store.Dir(first.JobID), "wt") || state.Worktree.BaseCommit == "" {
		t.Fatalf("state=%s", data)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "a.txt")); string(b) != "hi\n" {
		t.Fatalf("dirty source leaked: %q", b)
	}
	if _, err := os.Stat(filepath.Join(repo, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("worker changed source: %v", err)
	}
	out, code := runCLI(t, bin, repo, "", "sub", "result", first.JobID)
	if code != 0 || !strings.Contains(strings.SplitN(out, "\n", 2)[0], "The original workspace has uncommitted changes; they were not included in the worktree") || !strings.Contains(out, wt) {
		t.Fatalf("summary: %d %s", code, out)
	}
	// A second turn overwrites an untracked file from the first turn. Its full
	// worktree diff must still include that file even if the content is unchanged.
	second := run(true)
	if len(second.Diff.Files) != 1 || second.Diff.Files[0].Path != "out.txt" {
		t.Fatalf("resumed diff=%+v", second.Diff)
	}
	data, _ = os.ReadFile(filepath.Join(store.Dir(second.JobID), "state.json"))
	if !strings.Contains(string(data), wt) {
		t.Fatalf("resume created a different worktree: %s", data)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "apply", second.JobID)
	if code != 0 || !strings.Contains(out, "discard") {
		t.Fatalf("apply: %d %s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "out.txt")); string(b) != "hi\n" {
		t.Fatalf("apply new file: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "user edit\n" {
		t.Fatalf("apply clobbered user: %q", b)
	}
	c := exec.Command("git", "diff", "--cached", "--exit-code")
	c.Dir = repo
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("apply changed index: %s %v", b, err)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "discard", first.JobID)
	if code != 0 {
		t.Fatalf("discard: %d %s", code, out)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree remains: %v", err)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "status", second.JobID, "--json")
	if code != 0 || !strings.Contains(out, `"discarded": true`) {
		t.Fatalf("discard state: %d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "again", "sub", "run", "fake", "--resume", "--worktree")
	if code == 0 || !strings.Contains(out, "discard") {
		t.Fatalf("resumed discarded worktree: %d %s", code, out)
	}
}

func TestSubRunStoreFailureKeepsExitOne(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(home, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	out, code := runCLI(t, bin, gitRepoForCLI(t), "task", "sub", "run", "fake")
	if code != 1 {
		t.Fatalf("store failure exit changed: %d %s", code, out)
	}
}

func TestSubWorktreeRunningDiscardRequiresCancel(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	out, code := runCLI(t, bin, repo, "task", "sub", "run", "fake", "--worktree", "--wait", "0s", "--json")
	if code != 3 {
		t.Fatalf("start: %d %s", code, out)
	}
	var pending struct{ JobID string }
	if err := json.Unmarshal([]byte(out), &pending); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runCLI(t, bin, repo, "", "sub", "cancel", pending.JobID) })
	out, code = runCLI(t, bin, repo, "", "sub", "discard", pending.JobID)
	if code != 1 || !strings.Contains(out, "cancel") {
		t.Fatalf("discard running: %d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "cancel", pending.JobID)
	if code != 0 {
		t.Fatalf("cancel: %d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "discard", pending.JobID)
	if code != 0 {
		t.Fatalf("discard canceled: %d %s", code, out)
	}
}
