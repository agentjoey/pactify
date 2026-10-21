package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubPreflightIgnoresFallbackHarness(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	t.Setenv("PACTIFY_SUB_FAKE_CODEX", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	if err := os.WriteFile(filepath.Join(os.Getenv("PACTIFY_HOME"), "router.json"), []byte(`{"fallback":{"codex":["kimi"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, bin, repo, "brief", "sub", "run", "codex", "--wait=10s", "--json")
	var result sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &result) != nil || result.Harness != "codex" {
		t.Fatalf("missing fallback must not prevent the primary run: code=%d out=%s", code, out)
	}
}

// These dispatch tests need an executable on PATH, but must never start a model.
func stubUnavailableHarness(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
}

func TestSubMissingHarnessBeforeJobAndWorktree(t *testing.T) {
	bin := buildPactify(t)
	for _, target := range []string{"codex", "@primary"} {
		for _, resume := range []string{"", "--resume", "--resume=previous"} {
			t.Run(target+resume, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("PACTIFY_HOME", home)
				t.Setenv("PACTIFY_SUB_FAKE", "")
				t.Setenv("PATH", "/usr/bin:/bin")
				repo := gitRepoForCLI(t)
				if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(`{"aliases":{"primary":{"harness":"codex"}}}`), 0600); err != nil {
					t.Fatal(err)
				}
				store, _ := sub.DefaultStore()
				if resume != "" {
					if _, err := store.CreateRun("previous", sub.Request{Harness: "codex", Cwd: repo, Access: "write"}, true, false); err != nil {
						t.Fatal(err)
					}
					if err := store.Finish("previous", &sub.Result{Status: sub.StatusSucceeded, SessionID: "session"}, "", time.Now()); err != nil {
						t.Fatal(err)
					}
				}
				before, _ := os.ReadDir(store.Root)
				worktrees := func() string {
					cmd := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain")
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatal(err)
					}
					return string(out)
				}
				beforeWT := worktrees()
				args := []string{"sub", "run", target, "--worktree", "--wait=0"}
				if resume != "" {
					args = append(args, resume)
				}
				out, code := runCLI(t, bin, repo, "brief", args...)
				if code != 2 || !strings.Contains(out, "codex") || !strings.Contains(out, "PATH") || !strings.Contains(out, "pactify sub doctor codex") {
					t.Errorf("missing CLI: code=%d output=%s", code, out)
				}
				after, _ := os.ReadDir(store.Root)
				if !reflect.DeepEqual(before, after) {
					t.Errorf("job directories changed: before=%v after=%v", before, after)
				}
				if afterWT := worktrees(); afterWT != beforeWT {
					t.Errorf("worktree left behind: %s", afterWT)
				}
			})
		}
	}
}
