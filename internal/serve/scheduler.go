package serve

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/ledger"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/runguard"
	"github.com/agentjoey/pactify/internal/schedule"
)

func seatKindsFromInitEvents(evs []event.Event) map[string]string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EventType != "init" {
			continue
		}
		seats, ok := evs[i].Payload["seats"].([]any)
		if !ok {
			break
		}
		out := map[string]string{}
		for _, s := range seats {
			m, _ := s.(map[string]any)
			id, _ := m["id"].(string)
			kind, _ := m["kind"].(string)
			if id != "" && kind != "" {
				out[id] = kind
			}
		}
		if len(out) > 0 {
			return out
		}
		break
	}
	return nil
}

// resolveSeatKinds keeps derived configuration in the child's roster channel,
// below role bindings. It never becomes an explicit --seat-kind override.
func resolveSeatKinds(dir string) map[string]string {
	evs, _ := ledger.Read(dir)
	kinds := seatKindsFromInitEvents(evs)
	if kinds == nil {
		kinds = map[string]string{}
	}
	if st, err := pact.At(dir).StateProjection(); err == nil {
		for _, a := range st.Agents {
			if kinds[a.ID] == "" && a.Kind != "" {
				kinds[a.ID] = a.Kind
			}
		}
		known := agent.Kinds()
		for _, a := range st.Agents {
			if kinds[a.ID] == "" {
				if kind := agent.InferKindFrom(a.ID, known); kind != "" {
					kinds[a.ID] = kind
				}
			}
		}
	}
	return kinds
}

// orchRunning tracks project dirs with a driver spawned by this process that
// has not exited yet. The child takes seconds before its first status write,
// so the status.json check alone cannot serialize concurrent scheduler calls. Keyed by dir (not a
// Server field) so any two spawners into the same project exclude each other.
var orchRunning = struct {
	sync.Mutex
	dirs map[string]bool
}{dirs: map[string]bool{}}

// orchMarkRunning claims dir for a new driver; false means one is already live.
func orchMarkRunning(dir string) bool {
	orchRunning.Lock()
	defer orchRunning.Unlock()
	if orchRunning.dirs[dir] {
		return false
	}
	orchRunning.dirs[dir] = true
	return true
}

func orchClearRunning(dir string) {
	orchRunning.Lock()
	delete(orchRunning.dirs, dir)
	orchRunning.Unlock()
}

func (s *Server) orchestrateRunning(dir string) bool {
	// Delegates to runguard so this spawn guard and the checkpoint guard can
	// never drift into two different answers about whether a run is alive.
	// Semantics are unchanged: serial status.json only (a parallel driver is
	// spawned through a different path), done/escalated/missing/corrupt/stale
	// all mean "not running" — a crashed run leaves done=false forever and must
	// not block new ones.
	_, running := runguard.Serial(dir)
	return running
}

func (s *Server) defaultExecOrchestrate(dir string, args, env []string) error {
	bin, err := exec.LookPath("pactify")
	if err != nil {
		return fmt.Errorf("pactify not in PATH: %w", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	logf := driverLogFile(dir)
	if logf != nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
	}
	if err := cmd.Start(); err != nil {
		if logf != nil {
			logf.Close()
		}
		return err
	}
	// Reap the child: serve is a long-lived daemon, so an un-Waited driver
	// lingers as a zombie. On failure leave a trace — a driver that dies before
	// its first status.json write otherwise reports nothing anywhere.
	go func() {
		werr := cmd.Wait()
		if logf != nil {
			logf.Close()
		}
		orchClearRunning(dir)
		if werr != nil {
			writeDriverError(dir, werr)
		}
	}()
	return nil
}

// driverLogFile opens .pact/orchestrate/driver.log (truncated per run) for the
// child's stdout/stderr; nil means the output is discarded as before.
func driverLogFile(dir string) *os.File {
	p := filepath.Join(dir, ".pact", "orchestrate", "driver.log")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil
	}
	f, err := os.Create(p)
	if err != nil {
		return nil
	}
	return f
}

func writeDriverError(dir string, err error) {
	p := filepath.Join(dir, ".pact", "orchestrate", "driver-error.log")
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	msg := time.Now().UTC().Format(time.RFC3339) + " orchestrate driver exited: " + err.Error() + " (output in driver.log)\n"
	_ = os.WriteFile(p, []byte(msg), 0o644)
}

// sortedKeys returns m's keys in a stable order, so the child's argv is
// deterministic (map range order is not) — one spawn's command line is
// comparable to the next's, in logs and in tests.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// spawnScheduled claims the project before checking the on-disk run guard, so
// concurrent ticks cannot spawn two drivers before status.json is written.
func (s *Server) spawnScheduled(dir, feature string) (bool, error) {
	if !orchMarkRunning(dir) {
		return true, nil
	}
	if s.orchestrateRunning(dir) {
		orchClearRunning(dir)
		return true, nil
	}
	args := []string{"orchestrate", "--as", s.seat}
	derived := resolveSeatKinds(dir)
	for _, seat := range sortedKeys(derived) {
		args = append(args, "--roster-kind", seat+"="+derived[seat])
	}
	if feature != "" {
		args = append(args, "--feature", feature)
	}
	execFn := s.execOrchestrate
	injected := execFn != nil
	if !injected {
		execFn = s.defaultExecOrchestrate
	}
	if err := execFn(dir, args, []string{"PACT_AGENT_ID=" + s.seat}); err != nil {
		orchClearRunning(dir)
		return false, err
	}
	// The real child's reaper clears its marker; test runners have no child.
	if injected {
		orchClearRunning(dir)
	}
	return false, nil
}

// scheduleSpawn resolves the registry name and launches a serial run. A live
// driver maps to alreadyRunning=true, so the scheduler logs a skip.
func (s *Server) scheduleSpawn(project, feature string) (bool, error) {
	p, ok := s.projects[project]
	if !ok {
		return false, fmt.Errorf("unknown project %q", project)
	}
	return s.spawnScheduled(p.Path, feature)
}

// StartScheduler launches the minute-granularity schedule runner in the calling
// goroutine (cmd_serve runs it as `go srv.StartScheduler(ctx)`). It reads
// ~/.pactify/schedules.json each tick and spawns orchestrate for due enabled
// schedules; the existing conflict guard skips any project already running.
func (s *Server) StartScheduler(ctx context.Context) {
	r := &schedule.Runner{
		Path:  schedule.DefaultPath(),
		Spawn: s.scheduleSpawn,
		Log: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "pactify serve: "+format+"\n", args...)
		},
	}
	r.Start(ctx)
}
