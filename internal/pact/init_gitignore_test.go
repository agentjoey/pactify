package pact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// init scaffolds a starter .gitignore when the repo has none (2026-07-19
// cross-vendor dogfood: a worker's checkpoint `git add -A` swept __pycache__
// into history because the scratch repo had no ignore file at all). Common
// tool junk only — pactify has no business dictating a project's real ignore
// policy, so an EXISTING .gitignore is never touched.
func TestInitScaffoldsGitignoreWhenAbsent(t *testing.T) {
	repo := newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv("PACT_DIR", "")
	if err := pact.At(repo).As("orch").Init("p", []string{"orch:orchestrator:CLAUDE.md"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil {
		t.Fatalf("init must scaffold .gitignore when the repo has none: %v", err)
	}
	for _, want := range []string{"__pycache__/", "node_modules/", ".DS_Store"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf(".gitignore must cover %q:\n%s", want, b)
		}
	}
}

func TestInitLeavesExistingGitignoreUntouched(t *testing.T) {
	repo := newGitRepo(t)
	own := "# mine\n/dist\n"
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv("PACT_DIR", "")
	if err := pact.At(repo).As("orch").Init("p", []string{"orch:orchestrator:CLAUDE.md"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != own {
		t.Fatalf("existing .gitignore must be preserved byte-for-byte, got:\n%s", b)
	}
}
