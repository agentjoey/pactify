package main

import (
	"encoding/json"
	"github.com/agentjoey/pactify/internal/sub"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubDefaultsPriority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	t.Setenv("PACTIFY_SUB_FAKE_CODEX", "1")
	if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(`{"defaults":{"codex":{"model":"base","effort":"medium"}},"aliases":{"partial":{"harness":"codex","model":"alias"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	store, _ := sub.DefaultStore()
	for _, tc := range []struct {
		args          []string
		model, effort string
	}{
		{[]string{"codex"}, "base", "medium"},
		{[]string{"@partial"}, "alias", "medium"},
		{[]string{"@partial", "--model", "explicit", "--effort", "high"}, "explicit", "high"},
		{[]string{"@partial", "--model=", "--effort="}, "", ""},
	} {
		args := append([]string{"sub", "run"}, tc.args...)
		args = append(args, "--wait", "10s", "--json")
		out, code := runCLI(t, bin, repo, "brief", args...)
		var result sub.Result
		if code != 0 || json.Unmarshal([]byte(out), &result) != nil {
			t.Fatalf("%d %s", code, out)
		}
		st, err := store.Load(result.JobID)
		if err != nil || st.Request.Model != tc.model || st.Request.Effort != tc.effort {
			t.Fatalf("%+v %v", st, err)
		}
	}
	for _, flag := range []string{"", "--json"} {
		args := []string{"sub", "router"}
		if flag != "" {
			args = append(args, flag)
		}
		out, code := runCLI(t, bin, repo, "", args...)
		if code != 0 || !strings.Contains(out, "base") || !strings.Contains(out, "medium") {
			t.Fatalf("%d %s", code, out)
		}
	}
}
