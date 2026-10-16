package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubWorktreeUnbornRepositoryRejectedBeforeJob(t *testing.T) {
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	for _, access := range []string{"--read-only", "--write"} {
		t.Run(access, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("PACTIFY_HOME", home)
			dir := t.TempDir()
			cmd := exec.Command("git", "init", "-q")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
			out, code := runCLI(t, bin, dir, "task", "sub", "run", "fake", access, "--worktree")
			if code != 2 || !strings.Contains(out, "--worktree needs a base commit; commit once first (the repository has no commits yet)") {
				t.Errorf("code=%d out=%s", code, out)
			}
			entries, err := os.ReadDir(filepath.Join(home, "jobs"))
			if !os.IsNotExist(err) && (err != nil || len(entries) != 0) {
				t.Fatalf("rejected run created jobs: %v err=%v", entries, err)
			}
		})
	}
}
