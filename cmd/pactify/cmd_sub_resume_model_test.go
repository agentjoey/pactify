package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubResumeModelsInheritBeforeRouterDefaults(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	cwd := gitRepoForCLI(t)
	store, _ := sub.DefaultStore()
	if err := os.WriteFile(filepath.Join(filepath.Dir(store.Root), "router.json"), []byte(`{"defaults":{"fake":{"model":"router-model","effort":"router-effort"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		flags         []string
		model, effort string
	}{{nil, "original", "high"}, {[]string{"--model", "explicit"}, "explicit", "high"}, {[]string{"--effort", "low"}, "original", "low"}, {[]string{"--model=", "--effort="}, "", ""}} {
		id := []string{"inherit", "model", "effort", "empty"}[i]
		st := resumeCLIJob(t, store, id, cwd, "fake", "session-"+id, sub.StatusSucceeded, 1)
		st.Request.Model = "original"
		st.Request.Effort = "high"
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"sub", "run", "--resume=" + id, "--wait", "10s", "--json"}, tc.flags...)
		out, code := runCLI(t, bin, cwd, "continue", args...)
		var result sub.Result
		if code != 0 || json.Unmarshal([]byte(out), &result) != nil {
			t.Fatalf("%d %s", code, out)
		}
		next, err := store.Load(result.JobID)
		if err != nil || next.Request.Model != tc.model || next.Request.Effort != tc.effort {
			t.Fatal(next, err)
		}
	}
	latestCwd := gitRepoForCLI(t)
	latest := resumeCLIJob(t, store, "latest-parent", latestCwd, "fake", "latest-session", sub.StatusSucceeded, 1)
	latest.Request.Model, latest.Request.Effort = "latest-model", "medium"
	if err := store.Save(latest); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, bin, latestCwd, "continue", "sub", "run", "fake", "--resume", "--wait", "10s", "--json")
	var result sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &result) != nil {
		t.Fatalf("%d %s", code, out)
	}
	next, err := store.Load(result.JobID)
	if err != nil || next.Request.Model != "latest-model" || next.Request.Effort != "medium" {
		t.Fatal(next, err)
	}
}
