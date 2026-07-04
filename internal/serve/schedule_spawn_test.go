package serve

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// scheduleSpawn resolves a project name and launches a run; a happy path returns
// (false, nil) and invokes the exec with an orchestrate arg vector.
func TestScheduleSpawnHappyPath(t *testing.T) {
	root := seedGuardRepo(t, "main")
	var gotArgs []string
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetExecOrchestrate(func(dir string, args, env []string) error {
		gotArgs = args
		return nil
	})

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
	if !hasArgPair(gotArgs, "--feature", "feat-x") {
		t.Fatalf("exec args %v missing --feature feat-x", gotArgs)
	}
}

// An orchestrate already live for the project maps the 409 conflict to
// alreadyRunning=true (a skip), not an error.
func TestScheduleSpawnAlreadyRunning(t *testing.T) {
	root := seedGuardRepo(t, "main")
	// Seed a recent, not-done status file so orchestrateRunning() reports live.
	statusPath := orchestrateStatusPath(root)
	os.MkdirAll(filepath.Dir(statusPath), 0o755)
	status := `{"done":false,"escalated":false,"updated_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	os.WriteFile(statusPath, []byte(status), 0o644)

	spawned := false
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetExecOrchestrate(func(dir string, args, env []string) error {
		spawned = true
		return nil
	})

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
	srv.SetSeat("claude")
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
