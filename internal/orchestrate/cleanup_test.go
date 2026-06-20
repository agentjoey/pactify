package orchestrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/projection"
	"github.com/agentjoey/pactify/internal/sessions"
)

// sessionList is the table an opencode `session list` returns in these tests.
const sessionList = "Session ID  Title     Updated\n" +
	"────────────────────────────\n" +
	"ses_a1  pact:dev  11:00\n" +
	"ses_b2  pact:rev  10:00\n" +
	"ses_c3  unrelated  09:00\n"

// fakeSessionRun returns the list on `session list` and records delete ids.
func fakeSessionRun(deletes *[]string) func(string, string, ...string) (string, error) {
	return func(_, _ string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "session" && args[1] == "list" {
			return sessionList, nil
		}
		if len(args) >= 3 && args[0] == "session" && args[1] == "delete" {
			*deletes = append(*deletes, args[2])
			return "Session " + args[2] + " deleted", nil
		}
		return "", nil
	}
}

func has(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestCleanupTaskSessions_OpencodeDeletesOwnerAndReviewer(t *testing.T) {
	var deletes []string
	notif := &recNotify{}
	opts := Options{
		SessionRun: fakeSessionRun(&deletes),
		SeatKind:   func(string) string { return "opencode" },
		Notify:     notif,
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "dev", Reviewer: "rev"})

	if !has(deletes, "ses_a1") || !has(deletes, "ses_b2") {
		t.Fatalf("deletes = %v, want ses_a1 (owner) + ses_b2 (reviewer)", deletes)
	}
	if has(deletes, "ses_c3") {
		t.Errorf("deleted unrelated session ses_c3: %v", deletes)
	}
	if len(notif.msgs) == 0 {
		t.Error("expected a Notify message reporting the cleanup")
	}
}

// opencode (and other session CLIs) scope their session store to the cwd, so
// cleanup MUST run the CLI in opts.Dir — the worktree in parallel runs, the repo
// in serial. If it runs in the wrong dir it lists the wrong project's sessions and
// deletes nothing (the worktree-session leak). Assert opts.Dir is threaded through.
func TestCleanupTaskSessions_RunsInOptsDir(t *testing.T) {
	var gotDir string
	opts := Options{
		Dir: "/tmp/feature-worktree",
		SessionRun: func(dir, _ string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "session" && args[1] == "list" {
				gotDir = dir
				return sessionList, nil
			}
			return "", nil
		},
		SeatKind: func(string) string { return "opencode" },
		Notify:   &recNotify{},
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "dev", Reviewer: "rev"})
	if gotDir != "/tmp/feature-worktree" {
		t.Fatalf("session CLI ran in %q, want opts.Dir %q", gotDir, "/tmp/feature-worktree")
	}
}

// kimi has no list/delete CLI, so its sessions are cleaned by file ops: the
// accepted task's kimi seat → delete the on-disk session dirs the briefing tagged.
func TestCleanupTaskSessions_KimiClosesSessionFiles(t *testing.T) {
	home := t.TempDir()
	mine := filepath.Join(home, "sessions", "wd_a", "session_u1")
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(mine, "state.json"),
		[]byte("{\"title\":\"# pact worker — seat `kimi-worker` (roles: worker)\"}"), 0o644)

	orig := sessions.KimiHome
	sessions.KimiHome = func() string { return home }
	defer func() { sessions.KimiHome = orig }()

	opts := Options{
		Dir:        "/repo",
		SessionRun: func(_, _ string, _ ...string) (string, error) { return "", nil }, // enables cleanup
		SeatKind:   func(string) string { return "kimi-cli" },
		Notify:     &recNotify{},
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "kimi-worker", Reviewer: "claude"})

	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Fatal("kimi seat's session dir was not removed by cleanup")
	}
}

func TestCleanupTaskSessions_DisabledWhenRunnerNil(t *testing.T) {
	// SessionRun nil = cleanup disabled (test/default): never touches a CLI.
	opts := Options{
		SeatKind: func(string) string { return "opencode" },
		Notify:   &recNotify{},
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "dev", Reviewer: "rev"})
	// No panic, no run — nothing to assert beyond not crashing with a nil runner.
}

func TestCleanupTaskSessions_NonCleanupKindSkipped(t *testing.T) {
	var deletes []string
	called := false
	opts := Options{
		SessionRun: func(_, _ string, _ ...string) (string, error) { called = true; return "", nil },
		SeatKind:   func(string) string { return "claude-code" }, // no list+delete support
		Notify:     &recNotify{},
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "dev", Reviewer: "rev"})
	if called || len(deletes) != 0 {
		t.Errorf("non-cleanup kind should not invoke the session runner")
	}
}

func TestCleanupTaskSessions_SameOwnerReviewerDedup(t *testing.T) {
	var deletes []string
	opts := Options{
		SessionRun: fakeSessionRun(&deletes),
		SeatKind:   func(string) string { return "opencode" },
		Notify:     &recNotify{},
	}
	// Owner == Reviewer (solo seat): the seat is cleaned once, not twice.
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "dev", Reviewer: "dev"})
	count := 0
	for _, d := range deletes {
		if d == "ses_a1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("ses_a1 deleted %d times, want 1 (dedup owner==reviewer)", count)
	}
}
