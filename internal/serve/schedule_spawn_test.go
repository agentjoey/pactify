package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// scheduleSpawn resolves a project name and launches a run; a happy path returns
// (false, nil) and invokes the exec with an orchestrate arg vector.
func TestScheduleSpawnHappyPath(t *testing.T) {
	t.Setenv("PACT_AGENT_ID", "claude")
	root := t.TempDir()
	var gotArgs []string
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.execOrchestrate = func(dir string, args, env []string) error {
		gotArgs = args
		if dir != root || len(env) != 1 || env[0] != "PACT_AGENT_ID=claude" {
			t.Errorf("spawn dir=%q env=%v", dir, env)
		}
		return nil
	}

	already, err := srv.scheduleSpawn("p", "feat-x")
	if err != nil {
		t.Fatalf("scheduleSpawn: %v", err)
	}
	if already {
		t.Fatal("scheduleSpawn reported already-running on a fresh project")
	}
	if len(gotArgs) == 0 || gotArgs[0] != "orchestrate" {
		t.Fatalf("exec args = %v, want to start with 'orchestrate'", gotArgs)
	}
	if !hasArgPair(gotArgs, "--as", "claude") || !hasArgPair(gotArgs, "--feature", "feat-x") {
		t.Fatalf("exec args %v missing --feature feat-x", gotArgs)
	}
}

// launchd passes --seat without PACT_AGENT_ID; the flag must reach the spawn.
func TestScheduleSpawnUsesSetSeat(t *testing.T) {
	t.Setenv("PACT_AGENT_ID", "")
	root := t.TempDir()
	var gotArgs, gotEnv []string
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.execOrchestrate = func(_ string, args, env []string) error {
		gotArgs, gotEnv = args, env
		return nil
	}
	if _, err := srv.scheduleSpawn("p", ""); err != nil {
		t.Fatalf("scheduleSpawn: %v", err)
	}
	if !hasArgPair(gotArgs, "--as", "claude") || len(gotEnv) != 1 || gotEnv[0] != "PACT_AGENT_ID=claude" {
		t.Fatalf("args=%v env=%v", gotArgs, gotEnv)
	}
}

// A live orchestrate maps to alreadyRunning=true (a skip), not an error.
func TestScheduleSpawnAlreadyRunning(t *testing.T) {
	root := t.TempDir()
	// Seed a recent, not-done status file so orchestrateRunning() reports live.
	statusPath := filepath.Join(root, ".pact", "orchestrate", "status.json")
	os.MkdirAll(filepath.Dir(statusPath), 0o755)
	status := `{"done":false,"escalated":false,"updated_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	os.WriteFile(statusPath, []byte(status), 0o644)

	spawned := false
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.execOrchestrate = func(dir string, args, env []string) error {
		spawned = true
		return nil
	}

	already, err := srv.scheduleSpawn("p", "")
	if err != nil {
		t.Fatalf("scheduleSpawn: %v", err)
	}
	if !already {
		t.Fatal("scheduleSpawn should report already-running when a run is live")
	}
	if spawned {
		t.Fatal("exec must not run when an orchestrate is already live")
	}
}

func TestScheduleSpawnUnknownProject(t *testing.T) {
	srv := New(nil)
	if _, err := srv.scheduleSpawn("nope", ""); err == nil {
		t.Fatal("scheduleSpawn(unknown) = nil error, want error")
	}
}

func hasArgPair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}

// Scheduled runs retain derived kinds as roster hints; a role binding in the
// child must still take precedence over them.
func TestScheduleSpawnRosterKinds(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".pact"), 0755); err != nil {
		t.Fatal(err)
	}
	init := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude","role":"orchestrator","event_type":"init","payload":{"project":"p","seats":[{"id":"w","roles":["worker"],"entry":"AGENTS.md","kind":"opencode"},{"id":"opencode-worker","roles":["worker"],"entry":"AGENTS.md"}]}}`
	if err := os.WriteFile(filepath.Join(dir, ".pact", "log.jsonl"), []byte(init+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.execOrchestrate = func(_ string, args, _ []string) error {
		if !hasArgPair(args, "--roster-kind", "w=opencode") || !hasArgPair(args, "--roster-kind", "opencode-worker=opencode") {
			t.Errorf("missing derived roster kinds: %v", args)
		}
		for _, arg := range args {
			if arg == "--seat-kind" {
				t.Errorf("derived kind became an explicit override: %v", args)
			}
		}
		return nil
	}
	if already, err := srv.scheduleSpawn("p", ""); already || err != nil {
		t.Fatalf("spawn: %v %v", already, err)
	}
}

func TestScheduleConcurrentSpawnAndRetry(t *testing.T) {
	dir := t.TempDir()
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	entered, release := make(chan struct{}), make(chan struct{})
	srv.execOrchestrate = func(_ string, _, _ []string) error {
		close(entered)
		<-release
		return fmt.Errorf("start failed")
	}
	done := make(chan error, 1)
	go func() { _, err := srv.scheduleSpawn("p", ""); done <- err }()
	<-entered
	already, err := srv.scheduleSpawn("p", "")
	close(release)
	firstErr := <-done
	if !already || err != nil || firstErr == nil {
		t.Fatalf("conflict=%v err=%v firstErr=%v", already, err, firstErr)
	}
	srv.execOrchestrate = func(_ string, _, _ []string) error { return nil }
	if already, err := srv.scheduleSpawn("p", ""); already || err != nil {
		t.Fatalf("retry: %v %v", already, err)
	}
}
