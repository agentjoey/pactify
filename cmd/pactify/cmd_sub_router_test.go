package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func TestSubRouterEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	t.Setenv("PACTIFY_SUB_FAKE_CODEX", "1")
	config := `{"aliases":{"limited":{"harness":"codex","model":"fixture-quota"},"good":{"harness":"codex","model":"good","effort":"low"}},"fallback":{"codex":["@good"]}}`
	if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	out, code := runCLI(t, bin, repo, "brief", "sub", "run", "@limited", "--worktree", "--wait", "0s", "--json")
	var pending struct {
		JobID string `json:"jobId"`
	}
	if code != 3 || json.Unmarshal([]byte(out), &pending) != nil {
		t.Fatalf("%d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "wait", pending.JobID, "--timeout", "15s", "--json")
	var result sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &result) != nil || result.FallbackFrom != pending.JobID || result.Status != sub.StatusSucceeded {
		t.Fatalf("%d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "result", pending.JobID, "--json")
	var again sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &again) != nil || again.JobID != result.JobID {
		t.Fatalf("%d %s", code, out)
	}
	store, _ := sub.DefaultStore()
	old, _ := store.Load(pending.JobID)
	child, _ := store.Load(result.JobID)
	if old.FallbackTo != child.ID || child.Request.Model != "good" || child.Request.Effort != "low" || child.Worktree.Path != old.Worktree.Path {
		t.Fatalf("old=%+v child=%+v", old, child)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "status", pending.JobID)
	if code != 0 || !strings.Contains(out, "→ fallback to "+child.ID) {
		t.Fatalf("%d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "router", "--json")
	if code != 0 || !strings.Contains(out, "fixture-quota") || !strings.Contains(out, "exhaustedRoutes") {
		t.Fatalf("%d %s", code, out)
	}
	// Quota memory now skips the first route at startup without making another failed job.
	out, code = runCLI(t, bin, repo, "brief", "sub", "run", "@limited", "--wait", "10s", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &again) != nil || again.ModelRequested != "good" || !strings.Contains(strings.Join(again.Warnings, " "), "quota exhausted for") {
		t.Fatalf("%d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "brief", "sub", "run", "@good", "--model", "override", "--effort", "high", "--wait", "10s", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &again) != nil || again.ModelRequested != "override" {
		t.Fatalf("%d %s", code, out)
	}
	st, _ := store.Load(again.JobID)
	if st.Request.Effort != "high" {
		t.Fatalf("%+v", st)
	}
	if err := store.DiscardWorktree(child.ID); err != nil {
		t.Fatal(err)
	}
}
func TestSubRouterErrorsCreateNoJob(t *testing.T) {
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	t.Setenv("PACTIFY_SUB_FAKE_CODEX", "1")
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	for _, config := range []string{`{"fallback":{"codex":["@missing"]}}`, `{"aliases":{"cheap":{"harness":"bad"}}}`} {
		_ = os.WriteFile(filepath.Join(home, "router.json"), []byte(config), 0600)
		out, code := runCLI(t, bin, repo, "brief", "sub", "run", "codex", "--wait", "0s", "--json")
		if code != 2 || !strings.Contains(out, "router.json") {
			t.Fatalf("%d %s", code, out)
		}
	}
	_ = os.WriteFile(filepath.Join(home, "router.json"), []byte(`{}`), 0600)
	reset := time.Now().Add(time.Hour)
	q := map[string]sub.QuotaRoute{"codex": {Harness: "codex", RecordedAt: time.Now(), ResetAt: &reset}}
	data, _ := json.Marshal(q)
	_ = os.WriteFile(filepath.Join(home, "quota.json"), data, 0600)
	out, code := runCLI(t, bin, repo, "brief", "sub", "run", "codex", "--wait", "0s", "--json")
	if code != 2 || !strings.Contains(out, "earliest reset") {
		t.Fatalf("%d %s", code, out)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, "jobs")); len(entries) != 0 {
		t.Fatal(entries)
	}
}
func TestWorktreeFromFlagRejectsResume(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c := newSubCmd()
	run, _, _ := c.Find([]string{"run"})
	flag := run.Flags().Lookup("worktree-from")
	if flag == nil {
		t.Fatal("missing --worktree-from")
	}
	_ = run.Flags().Set("worktree-from", "old")
	code, err := subRun(run, "codex", "", "", ".", false, false, true, false, 0, false, "old")
	if code != 2 || err == nil || !strings.Contains(err.Error(), "--resume") {
		t.Fatalf("%d %v", code, err)
	}
}

func TestSubRouterAbsentKeepsQuotaBehavior(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	t.Setenv("PACTIFY_SUB_FAKE_CODEX", "1")
	store, _ := sub.DefaultStore()
	reset := time.Now().Add(time.Hour)
	data, _ := json.Marshal(map[string]sub.QuotaRoute{"codex": {Harness: "codex", RecordedAt: time.Now(), ResetAt: &reset}})
	if err := os.WriteFile(filepath.Join(filepath.Dir(store.Root), "quota.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	out, code := runCLI(t, bin, repo, "brief", "sub", "run", "codex", "--wait", "10s", "--json")
	var r sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || r.FallbackFrom != "" {
		t.Fatalf("%d %s", code, out)
	}
}
func TestSubWorktreeFromStartsNewSession(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	out, code := runCLI(t, bin, repo, "first", "sub", "run", "fake", "--worktree", "--wait", "10s", "--json")
	var old sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &old) != nil {
		t.Fatalf("%d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "second", "sub", "run", "fake", "--worktree-from", old.JobID, "--model", "changed", "--wait", "10s", "--json")
	var next sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &next) != nil || next.WorktreePath != old.WorktreePath || next.BaseCommit != old.BaseCommit {
		t.Fatalf("%d %s", code, out)
	}
	store, _ := sub.DefaultStore()
	st, _ := store.Load(next.JobID)
	if st.Request.Resume != "" || st.Request.Brief != "second" || st.Request.Model != "changed" {
		t.Fatalf("%+v", st)
	}
	if err := store.DiscardWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSubRouterTextAndJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	config := `{"aliases":{"mm":{"harness":"opencode","model":"mini"},"cheap":{"harness":"opencode","model":"flash","effort":"low"}},"fallback":{"codex":["@mm","kimi"],"agy":["@mm"]},"quotaGroups":{"argon":{"harness":"agy","models":["low","high"]}}}`
	if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	reset := time.Now().UTC().Add((6*24 + 18) * time.Hour).Truncate(time.Second)
	quotas := map[string]sub.QuotaRoute{"agy/high": {Harness: "agy", Model: "high", ResetAt: &reset, RecordedAt: time.Now()}, "codex": {Harness: "codex", RecordedAt: time.Now()}}
	data, err := json.Marshal(quotas)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, "quota.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	c := &cobra.Command{}
	var out bytes.Buffer
	c.SetOut(&out)
	code, err := subRouter(c, false)
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	text := out.String()
	for _, want := range []string{"Config path: " + filepath.Join(home, "router.json"), "\nAliases:\n", "@cheap → opencode flash [low]", "@mm → opencode mini", "\nFallback:\n", "agy → @mm", "codex → @mm, kimi", "\nWarnings:\n", "fallback.agy[0]: ignored route @mm", "fallback.codex[0]: ignored route @mm", "fallback.codex[1]: ignored route kimi", "\nQuota groups:\n", "argon → agy: low, high", "\nExhausted routes:\n", "agy/high", "resets in ", reset.Format(time.RFC3339), "codex", "reset time unknown", "memory expires at"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"aliases"`) || strings.Index(text, "@cheap →") > strings.Index(text, "@mm →") {
		t.Fatalf("not readable or not sorted:\n%s", text)
	}
	out.Reset()
	code, err = subRouter(c, true)
	var got struct {
		Path             string                 `json:"path"`
		Config           sub.Router             `json:"config"`
		ResolvedFallback map[string][]sub.Route `json:"resolvedFallback"`
		ExhaustedRoutes  []sub.QuotaRoute       `json:"exhaustedRoutes"`
		Warnings         []string               `json:"warnings"`
	}
	if code != 0 || err != nil || json.Unmarshal(out.Bytes(), &got) != nil {
		t.Fatalf("code=%d err=%v output=%s", code, err, out.String())
	}
	if got.Path != filepath.Join(home, "router.json") || got.Config.Aliases["cheap"].Effort != "low" || len(got.ResolvedFallback["codex"]) != 0 || len(got.ResolvedFallback["agy"]) != 0 || len(got.ExhaustedRoutes) != 2 || len(got.Warnings) != 3 {
		t.Fatalf("JSON contract changed: %s", out.String())
	}
}

func TestSubRouterTextWithoutConfig(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c := &cobra.Command{}
	var out bytes.Buffer
	c.SetOut(&out)
	code, err := subRouter(c, false)
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	for _, want := range []string{"Config path:", "routing disabled", "Aliases:\n  (none)", "Fallback:\n  (none)", "Quota groups:\n  (none)", "Exhausted routes:\n  (none)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, out.String())
		}
	}
}

func TestSubRouterStartupWarningOnStderrAndDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	t.Setenv("PACTIFY_SUB_FAKE_CODEX", "1")
	config := `{"aliases":{"preferred":{"harness":"codex","model":"high"},"good":{"harness":"codex","model":"good"}},"fallback":{"codex":["@good"]},"quotaGroups":{"family":{"harness":"codex","models":["low","high"]}}}`
	if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	reset := time.Now().Add((6*24 + 18) * time.Hour)
	quotas := map[string]sub.QuotaRoute{"codex/low": {Harness: "codex", Model: "low", RecordedAt: time.Now(), ResetAt: &reset}}
	data, err := json.Marshal(quotas)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, "quota.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, bin, "sub", "run", "@preferred", "--worktree", "--wait", "10s", "--json")
	command.Dir = repo
	command.Stdin = strings.NewReader("task")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err = command.Run(); err != nil {
		t.Fatalf("err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	warning := strings.TrimSpace(stderr.String())
	if !strings.HasPrefix(warning, "quota exhausted for codex/high; using codex/good (resets in ") {
		t.Fatalf("stderr=%q", warning)
	}
	var result sub.Result
	if err = json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Warnings, warning) {
		t.Fatalf("stdout lost warning: %s", stdout.String())
	}
	store, _ := sub.DefaultStore()
	data, err = os.ReadFile(store.ResultPath(result.JobID))
	if err != nil {
		t.Fatal(err)
	}
	var persisted sub.Result
	if err = json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(persisted.Warnings, warning) {
		t.Fatalf("disk lost warning: %s", data)
	}
	if err = store.DiscardWorktree(result.JobID); err != nil {
		t.Fatal(err)
	}
}
