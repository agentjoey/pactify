package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubRunHelpNamesDefaultHarness(t *testing.T) {
	c := newSubCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetArgs([]string{"run", "--help"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"run [harness]", "default: codex"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q:\n%s", want, out.String())
		}
	}
}

func TestSubRunDefaultAndExplicitCodex(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	// Registering fake must not change which harness an omitted argument selects.
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	// Exercise the detached worker without starting a real coding agent.
	t.Setenv("PATH", "/usr/bin:/bin")
	for _, tc := range []struct {
		name, access string
		harness      []string
	}{
		{"default-write", "write", nil},
		{"default-read-only", "read-only", nil},
		{"explicit-codex", "write", []string{"codex"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"sub", "run"}, tc.harness...)
			if tc.access == "read-only" {
				args = append(args, "--read-only")
			}
			out, code := runCLI(t, bin, repo, "inspect this repo", append(args, "--wait", "0s", "--json")...)
			var pending struct {
				JobID string `json:"jobId"`
			}
			if code != 3 || json.Unmarshal([]byte(out), &pending) != nil || pending.JobID == "" {
				t.Fatalf("start: code=%d out=%s", code, out)
			}
			out, code = runCLI(t, bin, repo, "", "sub", "wait", pending.JobID, "--json")
			var r sub.Result
			if code != 1 || json.Unmarshal([]byte(out), &r) != nil || r.Harness != "codex" || r.Access != tc.access || r.Status != sub.StatusFailed || !strings.Contains(strings.Join(r.Warnings, " "), "codex") {
				t.Fatalf("missing codex must fail the selected job: code=%d out=%s", code, out)
			}
		})
	}
	out, code := runCLI(t, bin, repo, "continue", "sub", "run", "--resume")
	if code != 2 || !strings.Contains(out, "no codex session to resume") {
		t.Fatalf("default resume: code=%d out=%s", code, out)
	}
	for _, args := range [][]string{{"codex", "kimi"}, {"nope"}, {"fake", "codex"}} {
		out, code := runCLI(t, bin, repo, "brief", append([]string{"sub", "run"}, args...)...)
		if code != 2 {
			t.Errorf("%v: code=%d out=%s", args, code, out)
		}
	}
}

func TestSubFallbackWarningsAndFailure(t *testing.T) {
	for _, name := range []string{"codex", "kimi", "agy", "opencode"} {
		t.Run(name, func(t *testing.T) {
			store := sub.Store{Root: t.TempDir()}
			st, err := store.Create("j1", sub.Request{Harness: name, Cwd: t.TempDir(), Access: "write", Brief: "task"})
			if err != nil {
				t.Fatal(err)
			}
			st.Status, st.Error, st.FinishedAt = sub.StatusFailed, "worker panic", time.Now().UTC()
			if err := subSaveFallback(store, st); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(store.Dir("j1"), "result.json"))
			if err != nil {
				t.Fatal(err)
			}
			var r sub.Result
			if err := json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			want := "worker panic"
			if name == "agy" {
				want = name + " is experimental: no OS sandbox for writes; results vary by model; " + want
			}
			if name == "opencode" {
				want = "opencode has no OS sandbox for writes; file-tool directory guard only; shell is not confined; " + want
			}
			if r.Status != sub.StatusFailed || strings.Join(r.Warnings, "; ") != want || !strings.HasPrefix(r.Markdown(), "⚠️ "+want) {
				t.Fatalf("fallback result=%+v; want warnings %q", r, want)
			}
		})
	}
}
