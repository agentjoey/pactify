package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubAgyReadOnlyRefused(t *testing.T) {
	bin := buildPactify(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{
		{"agy", "--read-only"},
		{"agy", "--read-only", "--worktree"},
		{"agy", "--read-only", "--resume=old"},
		{"--resume=old"},
		{"--resume=old", "--read-only"},
		{"--resume=old", "--worktree"},
		{"agy", "--resume=old", "--write"},
		{"agy", "--resume"},
		{"agy", "--resume", "--worktree"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			repo := gitRepoForCLI(t)
			store, _ := sub.DefaultStore()
			st := resumeCLIJob(t, store, "old", repo, "agy", "session", sub.StatusSucceeded, 1)
			st.Request.Access = "read-only"
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			out, code := runCLI(t, bin, repo, "review", append([]string{"sub", "run"}, append(args, "--wait", "0s")...)...)
			if code != 2 || !strings.Contains(out, "Read-only access is not offered for agy") || !strings.Contains(out, "Use codex --read-only") {
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
