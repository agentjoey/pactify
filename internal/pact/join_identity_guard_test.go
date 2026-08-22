package pact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
)

func countLogLines(t *testing.T, repo string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, ".pact", "log.jsonl"))
	if err != nil {
		t.Fatalf("countLogLines: %v", err)
	}
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func setupJoinGuardRepo(t *testing.T) string {
	t.Helper()
	repo := newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "claude")
	t.Setenv("PACT_DIR", "")
	orch := pact.At(repo).As("claude")
	seats := []string{
		"claude:orchestrator,reviewer:CLAUDE.md",
		"agy-w:worker:AGENTS.md",
	}
	if err := orch.Init("pactify", seats); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := orch.Assign("join-identity-guard", "kind-ux", "feat/kind-ux-fixes", "agy-w", "claude", "", nil); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	return repo
}

func TestJoinIdentityMismatch_Actor(t *testing.T) {
	repo := setupJoinGuardRepo(t)
	initLines := countLogLines(t, repo)
	initBranch, _ := gitx.CurrentBranch(repo)

	// Acting as "claude" but trying to join as "agy-w"
	err := pact.At(repo).As("claude").Join("agy-w", "worker")
	if err == nil {
		t.Fatal("expected error on join seat mismatch with actor override, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "agy-w") {
		t.Errorf("error %q should mention target seat %q", errStr, "agy-w")
	}
	if !strings.Contains(errStr, "claude") {
		t.Errorf("error %q should mention current identity %q", errStr, "claude")
	}
	if !strings.Contains(errStr, "actor") {
		t.Errorf("error %q should mention source 'actor'", errStr)
	}

	// Invariant: zero new log events written
	if gotLines := countLogLines(t, repo); gotLines != initLines {
		t.Errorf("log lines changed: before=%d, after=%d (must be zero new events)", initLines, gotLines)
	}

	// Invariant: branch not changed
	if curBranch, _ := gitx.CurrentBranch(repo); curBranch != initBranch {
		t.Errorf("branch changed: before=%q, after=%q", initBranch, curBranch)
	}
}

func TestJoinIdentityMismatch_Env(t *testing.T) {
	repo := setupJoinGuardRepo(t)
	initLines := countLogLines(t, repo)
	initBranch, _ := gitx.CurrentBranch(repo)

	// PACT_AGENT_ID is "claude", trying to join "agy-w"
	t.Setenv("PACT_AGENT_ID", "claude")
	err := pact.At(repo).Join("agy-w", "worker")
	if err == nil {
		t.Fatal("expected error on join seat mismatch with env source, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "agy-w") {
		t.Errorf("error %q should mention target seat %q", errStr, "agy-w")
	}
	if !strings.Contains(errStr, "claude") {
		t.Errorf("error %q should mention current identity %q", errStr, "claude")
	}
	if !strings.Contains(errStr, "env") {
		t.Errorf("error %q should mention source 'env'", errStr)
	}
	if !strings.Contains(errStr, "export PACT_AGENT_ID=agy-w") {
		t.Errorf("error %q should provide actionable remediation advice (export PACT_AGENT_ID=agy-w)", errStr)
	}

	if gotLines := countLogLines(t, repo); gotLines != initLines {
		t.Errorf("log lines changed: before=%d, after=%d (must be zero new events)", initLines, gotLines)
	}

	if curBranch, _ := gitx.CurrentBranch(repo); curBranch != initBranch {
		t.Errorf("branch changed: before=%q, after=%q", initBranch, curBranch)
	}
}

func TestJoinIdentityMismatch_File(t *testing.T) {
	repo := setupJoinGuardRepo(t)
	initLines := countLogLines(t, repo)
	initBranch, _ := gitx.CurrentBranch(repo)

	// Unset env, write .pact/seat = claude
	t.Setenv("PACT_AGENT_ID", "")
	if err := os.WriteFile(filepath.Join(repo, ".pact", "seat"), []byte("claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := pact.At(repo).Join("agy-w", "worker")
	if err == nil {
		t.Fatal("expected error on join seat mismatch with file source, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "agy-w") {
		t.Errorf("error %q should mention target seat %q", errStr, "agy-w")
	}
	if !strings.Contains(errStr, "claude") {
		t.Errorf("error %q should mention current identity %q", errStr, "claude")
	}
	if !strings.Contains(errStr, "file") {
		t.Errorf("error %q should mention source 'file'", errStr)
	}
	if !strings.Contains(errStr, "agy-w") {
		t.Errorf("error %q should provide actionable remediation for agy-w", errStr)
	}

	if gotLines := countLogLines(t, repo); gotLines != initLines {
		t.Errorf("log lines changed: before=%d, after=%d (must be zero new events)", initLines, gotLines)
	}

	if curBranch, _ := gitx.CurrentBranch(repo); curBranch != initBranch {
		t.Errorf("branch changed: before=%q, after=%q", initBranch, curBranch)
	}
}

func TestJoinIdentityMatch_Succeeds(t *testing.T) {
	repo := setupJoinGuardRepo(t)
	initLines := countLogLines(t, repo)

	// Matching identity via env
	t.Setenv("PACT_AGENT_ID", "agy-w")
	err := pact.At(repo).Join("agy-w", "worker")
	if err != nil {
		t.Fatalf("join with matching identity must succeed, got %v", err)
	}

	if gotLines := countLogLines(t, repo); gotLines != initLines+1 {
		t.Errorf("expected 1 new log event, got %d (before: %d)", gotLines, initLines)
	}

	// Assert branch switched to feature branch feat/kind-ux-fixes
	curBranch, err := gitx.CurrentBranch(repo)
	if err != nil || curBranch != "feat/kind-ux-fixes" {
		t.Errorf("expected branch 'feat/kind-ux-fixes', got %q (%v)", curBranch, err)
	}
}
