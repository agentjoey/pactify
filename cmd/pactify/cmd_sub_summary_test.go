package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func TestSubStatusSummaryFiltersAndCompatibility(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	now := time.Now()
	for _, id := range []string{"older", "newer"} {
		store, st := statusFixture(t, id, "codex", now, &sub.Result{Status: sub.StatusFailed, Warnings: []string{"warning"}})
		st, _ = store.Load(id)
		st.Request.Brief = "# **Example-657** `摘要`"
		st.Worktree = &sub.Worktree{Path: "/jobs/" + id + "/wt", Cwd: "/jobs/" + id + "/wt/nested", OriginalRoot: "/repos/" + id, Owner: id}
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	c := &cobra.Command{}
	c.SetContext(context.Background())
	c.Flags().Bool("all", true, "")
	c.Flags().String("repo", "/repos/newer", "")
	var out bytes.Buffer
	c.SetOut(&out)
	code, err := subStatus(c, nil, true)
	if code != 0 || err != nil {
		t.Fatalf("status: %d %v", code, err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["jobId"] != "newer" || rows[0]["title"] != "Example-657 摘要" || rows[0]["cwd"] != "/workspace/newer" || rows[0]["runCwd"] != "/jobs/newer/wt/nested" {
		t.Fatalf("filtered summary: %s", out.String())
	}
	// List cwd retains its historical meaning; runCwd exposes the actual location.
	single, code, err := captureStatus(t, []string{"newer"}, true)
	if code != 0 || err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(single), &row); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"request", "updatedAt", "finishedAt", "title", "attention", "repo", "harness", "duration"} {
		if _, ok := row[key]; !ok {
			t.Errorf("single missing %s: %s", key, single)
		}
	}
	w := row["worktree"].(map[string]any)
	if w["originalRoot"] != "/repos/newer" || w["status"] != "pending" {
		t.Errorf("worktree compatibility: %v", w)
	}
	human, code, err := captureStatus(t, nil, false)
	if code != 0 || err != nil || !strings.Contains(human, "Example-657 摘要") || !strings.Contains(human, "⚠") {
		t.Fatalf("human: %d %v %s", code, err, human)
	}
}

func TestSubStatusAllAndRepoFlags(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	for i := 0; i < 12; i++ {
		statusFixture(t, time.Now().Add(time.Duration(i)*time.Second).Format("150405"), "codex", time.Now(), nil)
	}
	bin := buildPactify(t)
	out, code := runCLI(t, bin, t.TempDir(), "", "sub", "status", "--all", "--json")
	var rows []map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 12 {
		t.Fatalf("all: %d %s", code, out)
	}
	out, code = runCLI(t, bin, t.TempDir(), "", "sub", "status", "--repo", t.TempDir(), "--json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("repo: %d %s", code, out)
	}
}

func TestSubStatusRepoRootAndSummaryBeyondLimit(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	root := gitRepoForCLI(t)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	store, st := statusFixture(t, "nested", "codex", time.Now(), &sub.Result{Status: sub.StatusSucceeded, InputTokens: 9})
	st, _ = store.Load(st.ID)
	st.Request.Cwd = nested
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	c := &cobra.Command{}
	c.SetContext(context.Background())
	c.Flags().String("repo", root, "")
	var out bytes.Buffer
	c.SetOut(&out)
	code, err := subStatus(c, nil, false)
	if code != 0 || err != nil || !strings.Contains(out.String(), "nested") || !strings.Contains(out.String(), "in 9") {
		t.Fatalf("root: %d %v %s", code, err, out.String())
	}
}

func TestSubStatusSingleRepoMismatch(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	statusFixture(t, "single", "codex", time.Now(), nil)
	for _, asJSON := range []bool{false, true} {
		c := &cobra.Command{}
		c.SetContext(context.Background())
		c.SetOut(&bytes.Buffer{})
		c.Flags().String("repo", "/different", "")
		code, err := subStatus(c, []string{"single"}, asJSON)
		if code != 2 || err == nil || !strings.Contains(err.Error(), "repository") {
			t.Errorf("mismatch: %d %v", code, err)
		}
	}
}
