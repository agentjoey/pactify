package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubAgyReadOnlyRestored(t *testing.T) {
	bin := buildPactify(t)
	t.Setenv("PATH", "/Library/Developer/CommandLineTools/usr/bin:/usr/bin:/bin")
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
			installAgyV2CLI(t)
			repo := gitRepoForCLI(t)
			store, _ := sub.DefaultStore()
			st := resumeCLIJob(t, store, "old", repo, "agy", "sid", sub.StatusSucceeded, 1)
			st.Request.Access = "read-only"
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}

			readonly := strings.Contains(strings.Join(args, " "), "--read-only")
			out, code := runCLI(t, bin, repo, "review", append([]string{"sub", "run"}, append(args, "--model", "gemini-3.8-flash-low", "--wait", "8s", "--json")...)...)
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

func installAgyV2CLI(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := `#!/bin/sh
case "$1" in
 --version) echo 'agy 1.4.0';exit 0;;
 models) printf 'gemini-3.8-flash-low\tFlash\n';exit 0;;
 -p) echo '{"status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"gemini-weekly","window":"weekly","remaining_fraction":0.9,"reset_time":"2026-10-23T00:40:01Z"}]}]}}}';exit 0;;
esac
echo '{"event":"init","conversation_id":"sid","init":{"model":"gemini-3.8-flash-low"}}'
while IFS= read -r line; do
 echo '{"event":"step_update","step_update":{"step_index":1,"step_type":"agent_response","state":"DONE","text_delta":"Reviewed","usage":{"input_tokens":10,"output_tokens":1}}}'
 echo '{"event":"result","result":{"status":"SUCCESS","conversation_id":"sid","response":"Reviewed"}}'
done
`
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	token := filepath.Join(os.Getenv("HOME"), ".gemini/antigravity-cli/antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(token), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
}
