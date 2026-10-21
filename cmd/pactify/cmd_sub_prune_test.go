package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubPruneDryRunAndExecute(t *testing.T) {
	bin := buildPactify(t)
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	store := sub.Store{Root: filepath.Join(home, "jobs")}
	st, err := store.Create("old", sub.Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = sub.StatusSucceeded
	st.FinishedAt = time.Now().Add(-30 * 24 * time.Hour)
	store.Save(st)
	os.WriteFile(store.ResultPath(st.ID), []byte(`{"sessionId":""}`), 0600)
	output, code := runCLI(t, bin, home, "", "sub", "prune", "--json")
	if code != 0 || !strings.Contains(output, "reclaimable") {
		t.Fatalf("code=%d %s", code, output)
	}
	if _, err := store.Load("old"); err != nil {
		t.Fatal("dry run removed job", err)
	}
	output, code = runCLI(t, bin, home, "", "sub", "prune", "--yes", "--json")
	var run sub.StorageRun
	if code != 0 || json.Unmarshal([]byte(output), &run) != nil || run.Removed[0].Items != 1 {
		t.Fatalf("code=%d %s", code, output)
	}
	if _, err := store.Load("old"); !os.IsNotExist(err) {
		t.Fatalf("not removed: %v", err)
	}
	db := filepath.Join(home, "harness-homes/opencode/share/opencode/opencode.db")
	os.MkdirAll(filepath.Dir(db), 0700)
	os.WriteFile(db, []byte("fake"), 0600)
	output, code = runCLI(t, bin, home, "", "sub", "prune")
	if code != 0 || !strings.Contains(output, "SWEEP SESSIONS") || strings.Contains(output, "REMOVE harness-homes/opencode/share/opencode/opencode.db") {
		t.Fatalf("native cleanup was presented as database removal: code=%d %s", code, output)
	}
	for _, flags := range [][]string{{"--cache-max", "garbage"}, {"--older-than", "-1h"}, {"--include-worktrees", "--automatic"}} {
		output, code = runCLI(t, bin, home, "", append([]string{"sub", "prune"}, flags...)...)
		if code != 2 {
			t.Fatalf("%v: code=%d %s", flags, code, output)
		}
	}
}

func TestSubPruneSummarizedOutput(t *testing.T) {
	bin := buildPactify(t)
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	for i := 0; i < 30; i++ {
		p := filepath.Join(home, "cache/go-build", fmt.Sprint(i))
		os.MkdirAll(filepath.Dir(p), 0700)
		os.WriteFile(p, make([]byte, 2048), 0600)
	}
	output, code := runCLI(t, bin, home, "", "sub", "prune")
	if code != 0 || len(strings.Split(output, "\n")) > 15 || !strings.Contains(output, "KEEP") || !strings.Contains(output, "KB") {
		t.Fatalf("not summarized: %d %s", code, output)
	}
	output, code = runCLI(t, bin, home, "", "sub", "prune", "--verbose")
	if code != 0 || !strings.Contains(output, "cache/go-build") {
		t.Fatalf("verbose: %d %s", code, output)
	}
}

func TestPruneItems(t *testing.T) {
	for n, want := range map[int]string{0: "0 items", 1: "1 item", 2: "2 items", 2708: "2,708 items"} {
		if got := pruneItems(n); got != want {
			t.Fatalf("pruneItems(%d) = %q, want %q", n, got, want)
		}
	}
}
