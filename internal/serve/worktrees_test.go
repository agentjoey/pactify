package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

func TestGitWorktreesParsesPorcelain(t *testing.T) {
	orig := execGitWorktree
	defer func() { execGitWorktree = orig }()
	execGitWorktree = func(repoPath string) ([]byte, error) {
		return []byte("worktree /repo/main\nHEAD abc\nbranch refs/heads/main\n\n" +
			"worktree /repo/wt-fe\nHEAD def\nbranch refs/heads/feat-fe\n\n"), nil
	}
	got := gitWorktrees("/repo/main")
	if len(got) != 2 {
		t.Fatalf("want 2 worktrees, got %d", len(got))
	}
	if !got[0].Primary || got[0].Branch != "main" {
		t.Errorf("primary = %+v, want primary main", got[0])
	}
	if got[1].Branch != "feat-fe" || got[1].Primary {
		t.Errorf("second = %+v, want feat-fe non-primary", got[1])
	}
}

func TestGitWorktreesFallbackOnError(t *testing.T) {
	orig := execGitWorktree
	defer func() { execGitWorktree = orig }()
	execGitWorktree = func(string) ([]byte, error) { return nil, http.ErrAbortHandler }
	got := gitWorktrees("/x")
	if len(got) != 1 || !got[0].Primary || got[0].Path != "/x" {
		t.Fatalf("fallback = %+v, want single primary /x", got)
	}
}

func TestWorktreesEndpoint(t *testing.T) {
	orig := execGitWorktree
	defer func() { execGitWorktree = orig }()
	execGitWorktree = func(string) ([]byte, error) {
		return []byte("worktree /repo/main\nbranch refs/heads/main\n\n"), nil
	}
	s := New([]registry.Project{{Name: "p1", Path: "/repo/main"}})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/projects/p1/worktrees")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var wts []WorktreeDTO
	_ = json.NewDecoder(resp.Body).Decode(&wts)
	if len(wts) != 1 || wts[0].Branch != "main" {
		t.Fatalf("wts = %+v", wts)
	}
}

func TestStateUnknownWorktreeIs400(t *testing.T) {
	orig := execGitWorktree
	defer func() { execGitWorktree = orig }()
	execGitWorktree = func(string) ([]byte, error) {
		return []byte("worktree /repo/main\nbranch refs/heads/main\n\n"), nil
	}
	s := New([]registry.Project{{Name: "p1", Path: t.TempDir()}})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/projects/p1/state?wt=nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 for unknown worktree", resp.StatusCode)
	}
}

// pact's INTERNAL run worktrees (the sandbox/parallel trees under
// .pact/orchestrate/, and park branches) must not surface as selectable project
// boards — they hold transient, divergent ledger state mid-run. Only the primary
// and real user worktrees are listed.
func TestGitWorktreesFiltersPactInternal(t *testing.T) {
	orig := execGitWorktree
	defer func() { execGitWorktree = orig }()
	execGitWorktree = func(string) ([]byte, error) {
		return []byte("worktree /repo\nHEAD a\nbranch refs/heads/pact-sandbox-park\n\n" +
			"worktree /repo/.pact/orchestrate/sandbox\nHEAD b\nbranch refs/heads/feat/web-store\n\n" +
			"worktree /repo/.pact/orchestrate/wt/feat-x\nHEAD c\nbranch refs/heads/feat/x\n\n" +
			"worktree /elsewhere/park\nHEAD d\nbranch refs/heads/pact-parallel-park\n\n" +
			"worktree /repo-deploy\nHEAD e\n\n" +
			"worktree /repo-wt/real\nHEAD f\nbranch refs/heads/feat/real\n"), nil
	}
	got := gitWorktrees("/repo")

	var paths, branches []string
	for _, w := range got {
		paths = append(paths, w.Path)
		branches = append(branches, w.Branch)
	}
	// primary kept (even though parked on pact-sandbox-park), plus the real user worktrees.
	if !got[0].Primary || got[0].Path != "/repo" {
		t.Fatalf("primary must be /repo: %+v", got[0])
	}
	for _, w := range got {
		if strings.Contains(w.Path, "/.pact/orchestrate/") {
			t.Fatalf("internal orchestrate worktree leaked: %q", w.Path)
		}
		if w.Branch == "pact-parallel-park" {
			t.Fatalf("internal park worktree leaked: %q", w.Path)
		}
	}
	// the real ones survive
	joined := strings.Join(paths, ",")
	if !strings.Contains(joined, "/repo-deploy") || !strings.Contains(joined, "/repo-wt/real") {
		t.Fatalf("real user worktrees dropped: %v", paths)
	}
	_ = branches
}
