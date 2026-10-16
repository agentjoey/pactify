package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubOpencodeReadOnlyRefused(t *testing.T) {
	bin := buildPactify(t)
	t.Setenv("PATH", "/Library/Developer/CommandLineTools/usr/bin:/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{
		{"opencode", "--read-only"},
		{"opencode", "--read-only", "--worktree"},
		{"opencode", "--read-only", "--resume=old"},
		{"--resume=old"},
		{"--resume=old", "--read-only"},
		{"--resume=old", "--worktree"},
		{"opencode", "--resume=old", "--write"},
		{"opencode", "--resume"},
		{"opencode", "--resume", "--worktree"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			repo := gitRepoForCLI(t)
			store, _ := sub.DefaultStore()
			st := resumeCLIJob(t, store, "old", repo, "opencode", "session", sub.StatusSucceeded, 1)
			st.Request.Access = "read-only"
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			out, code := runCLI(t, bin, repo, "review", append([]string{"sub", "run"}, append(args, "--wait", "0s")...)...)
			if code != 2 || !strings.Contains(out, "Read-only access is not offered for opencode") || !strings.Contains(out, "Use codex --read-only") {
				t.Fatalf("code=%d out=%s", code, out)
			}
			entries, err := os.ReadDir(store.Root)
			if err != nil || len(entries) != 1 {
				t.Fatalf("refusal created a job: %v %v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("PACTIFY_HOME"), "harness-homes")); !os.IsNotExist(err) {
				t.Fatalf("refusal prepared harness home: %v", err)
			}
		})
	}
}

func TestSubOpencodeOldReadOnlyWorktreeRefused(t *testing.T) {
	bin := buildPactify(t)
	t.Setenv("PATH", "/Library/Developer/CommandLineTools/usr/bin:/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{
		{"--resume=old"},
		{"--resume=old", "--worktree"},
		{"opencode", "--resume"},
		{"opencode", "--resume", "--worktree"},
		{"opencode", "--resume=old", "--write"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			repo := gitRepoForCLI(t)
			store, _ := sub.DefaultStore()
			st, err := store.CreateRun("old", sub.Request{Harness: "opencode", Cwd: repo, Access: "read-only", Brief: "review"}, true, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Finish(st.ID, &sub.Result{Status: sub.StatusSucceeded, SessionID: "session"}, "", st.CreatedAt); err != nil {
				t.Fatal(err)
			}
			out, code := runCLI(t, bin, repo, "continue", append([]string{"sub", "run"}, append(args, "--wait", "0s")...)...)
			if code != 2 || !strings.Contains(out, "Read-only access is not offered for opencode") || !strings.Contains(out, "Use codex --read-only") {
				t.Fatalf("code=%d out=%s", code, out)
			}
			entries, err := os.ReadDir(store.Root)
			if err != nil || len(entries) != 1 {
				t.Fatalf("refusal created a job: %v %v", entries, err)
			}
			if _, err := os.Stat(st.Worktree.Cwd); err != nil {
				t.Fatalf("original worktree lost: %v", err)
			}
		})
	}
}
