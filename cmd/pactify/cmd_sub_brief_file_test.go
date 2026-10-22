package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

// Hosts whose command rules only match simple commands cannot pass a brief
// through a heredoc or redirection, so the brief can come from a file.
func TestSubRunBriefFile(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	t.Setenv("PACTIFY_SUB_FAKE_CODEX", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	dir := t.TempDir()
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte("Brief from file\nGoals: none\n"), 0600); err != nil {
		t.Fatal(err)
	}

	out, code := runCLI(t, bin, repo, "stdin is ignored", "sub", "run", "codex", "--brief-file", brief, "--wait=10s", "--json")
	var result sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &result) != nil || result.JobID == "" {
		t.Fatalf("brief file run failed: code=%d out=%s", code, out)
	}
	store, _ := sub.DefaultStore()
	st, err := store.Load(result.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Request.Brief != "Brief from file\nGoals: none\n" {
		t.Fatalf("brief = %q", st.Request.Brief)
	}

	empty := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(empty, []byte(" \n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(dir, "missing.md"), "empty": empty} {
		out, code := runCLI(t, bin, repo, "stdin is ignored", "sub", "run", "codex", "--brief-file", path, "--wait=0", "--json")
		if code != 2 || !strings.Contains(out, "brief") {
			t.Fatalf("%s brief file: code=%d out=%s", name, code, out)
		}
	}
}
