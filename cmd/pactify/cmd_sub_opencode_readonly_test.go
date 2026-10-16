package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/agentjoey/pactify/internal/testenv"
)

func TestSubOpencodeReadOnlyRestored(t *testing.T) {
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
			testenv.InstallOpencodeServe(t)
			repo := gitRepoForCLI(t)
			store, _ := sub.DefaultStore()
			st := resumeCLIJob(t, store, "old", repo, "opencode", "sid", sub.StatusSucceeded, 1)
			st.Request.Access = "read-only"
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			seedOpencodeCLIData(t)
			readonly := strings.Contains(strings.Join(args, " "), "--read-only")
			if readonly {
				t.Setenv("COCKPIT_OC_SCENARIO", "readonly")
			} else {
				t.Setenv("COCKPIT_OC_SCENARIO", "")
			}
			out, code := runCLI(t, bin, repo, "review", append([]string{"sub", "run"}, append(args, "--model", "opencode/test-free", "--wait", "8s", "--json")...)...)
			joined := strings.Join(args, " ")
			invalidWorktree := strings.Contains(joined, "--resume") && strings.Contains(joined, "--worktree")
			entries, err := os.ReadDir(store.Root)
			if invalidWorktree {
				wantCode := 2
				if strings.Contains(joined, "--resume ") {
					wantCode = 1
				}
				if code != wantCode || !strings.Contains(out, "did not use a worktree") || err != nil || len(entries) != 1 {
					t.Fatalf("missing worktree constraint: code=%d out=%s entries=%v err=%v", code, out, entries, err)
				}
				return
			}
			var result sub.Result
			if code != 0 || json.Unmarshal([]byte(out), &result) != nil || result.Status != sub.StatusSucceeded || result.Sandboxed {
				t.Fatalf("read-only path rejected: code=%d out=%s", code, out)
			}
			if readonly && (result.Access != "read-only" || len(result.Diff.Files) != 0) {
				t.Fatalf("read-only result=%+v", result)
			}
			if err != nil || len(entries) != 2 {
				t.Fatalf("expected a new job: %v %v", entries, err)
			}
			got, _ := store.Load(result.JobID)
			if strings.Contains(joined, "--resume") && got.Request.Resume != "sid" {
				t.Fatalf("resume=%+v", got)
			}
		})
	}
}

func TestSubOpencodeOldReadOnlyWorktreeResumes(t *testing.T) {
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
			testenv.InstallOpencodeServe(t)
			repo := gitRepoForCLI(t)
			store, _ := sub.DefaultStore()
			st, err := store.CreateRun("old", sub.Request{Harness: "opencode", Cwd: repo, Access: "read-only", Brief: "review"}, true, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Finish(st.ID, &sub.Result{Status: sub.StatusSucceeded, SessionID: "sid"}, "", st.CreatedAt); err != nil {
				t.Fatal(err)
			}
			seedOpencodeCLIData(t)
			out, code := runCLI(t, bin, repo, "continue", append([]string{"sub", "run"}, append(args, "--model", "opencode/test-free", "--wait", "8s", "--json")...)...)
			var result sub.Result
			if code != 0 || json.Unmarshal([]byte(out), &result) != nil || result.Status != sub.StatusSucceeded {
				t.Fatalf("code=%d out=%s", code, out)
			}
			entries, err := os.ReadDir(store.Root)
			if err != nil || len(entries) != 2 {
				t.Fatalf("expected resumed job: %v %v", entries, err)
			}
			resumed, err := store.Load(result.JobID)
			if err != nil || resumed.Request.Resume != "sid" || resumed.Worktree == nil || resumed.Worktree.Path != st.Worktree.Path {
				t.Fatalf("resume lost isolation: %+v %v", resumed, err)
			}
			if _, err := os.Stat(st.Worktree.Cwd); err != nil {
				t.Fatalf("original worktree lost: %v", err)
			}
		})
	}
}

func TestOpencodeServePeer(t *testing.T) { testenv.OpencodeServePeer(t) }
func seedOpencodeCLIData(t *testing.T) {
	t.Helper()
	dir := filepath.Join(os.Getenv("PACTIFY_HOME"), "harness-homes/opencode/share/opencode")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.db"), []byte("0"), 0600); err != nil {
		t.Fatal(err)
	}
}
