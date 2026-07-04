package schedule

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type spawnCall struct {
	project, feature string
}

// fakeSpawn records calls and can report already-running for a project.
type fakeSpawn struct {
	calls   []spawnCall
	running map[string]bool // projects that report alreadyRunning
	err     map[string]error
}

func (f *fakeSpawn) fn(project, feature string) (bool, error) {
	f.calls = append(f.calls, spawnCall{project, feature})
	if f.err != nil {
		if e := f.err[project]; e != nil {
			return false, e
		}
	}
	return f.running[project], nil
}

func writeScheds(t *testing.T, path string, s ...Schedule) {
	t.Helper()
	if err := Save(path, s); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func TestRunnerFiresDueSchedule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	writeScheds(t, path, Schedule{ID: "s1", Project: "linx", Feature: "feat-x", Expr: "every:30m", Enabled: true})

	fs := &fakeSpawn{running: map[string]bool{}}
	r := &Runner{Path: path, Spawn: fs.fn}

	t0 := mustTime(t, "2026-07-05T02:00:00")
	// First tick arms the schedule (next = t0+30m) — must not fire.
	r.Tick(t0)
	if len(fs.calls) != 0 {
		t.Fatalf("first tick fired %d times, want 0 (arming only)", len(fs.calls))
	}

	// Before the interval elapses — still no fire.
	r.Tick(t0.Add(29 * time.Minute))
	if len(fs.calls) != 0 {
		t.Fatalf("early tick fired %d times, want 0", len(fs.calls))
	}

	// At/after the boundary — fires once.
	r.Tick(t0.Add(30 * time.Minute))
	if len(fs.calls) != 1 {
		t.Fatalf("boundary tick fired %d times, want 1", len(fs.calls))
	}
	if fs.calls[0] != (spawnCall{"linx", "feat-x"}) {
		t.Fatalf("spawn call = %+v, want {linx feat-x}", fs.calls[0])
	}

	// Next window: fires again after another 30m from the fire tick.
	r.Tick(t0.Add(59 * time.Minute))
	if len(fs.calls) != 1 {
		t.Fatalf("mid-window tick fired again (%d), want still 1", len(fs.calls))
	}
	r.Tick(t0.Add(60 * time.Minute))
	if len(fs.calls) != 2 {
		t.Fatalf("second window fired %d times, want 2", len(fs.calls))
	}
}

func TestRunnerSkipsAlreadyRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	writeScheds(t, path, Schedule{ID: "s1", Project: "busy", Expr: "every:10m", Enabled: true})

	var logs []string
	fs := &fakeSpawn{running: map[string]bool{"busy": true}}
	r := &Runner{Path: path, Spawn: fs.fn, Log: func(f string, a ...any) { logs = append(logs, f) }}

	t0 := mustTime(t, "2026-07-05T02:00:00")
	r.Tick(t0)                       // arm
	r.Tick(t0.Add(10 * time.Minute)) // due → spawn reports already-running

	if len(fs.calls) != 1 {
		t.Fatalf("spawn called %d times, want 1", len(fs.calls))
	}
	// Skip must be logged, not treated as an error.
	found := false
	for _, l := range logs {
		if strings.Contains(l, "already running") {
			found = true
		}
		if strings.Contains(l, "failed") {
			t.Fatalf("already-running logged as failure: %q", l)
		}
	}
	if !found {
		t.Fatalf("no already-running skip logged; logs=%v", logs)
	}
}

func TestRunnerDisabledNotFired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	writeScheds(t, path, Schedule{ID: "s1", Project: "off", Expr: "every:5m", Enabled: false})

	fs := &fakeSpawn{running: map[string]bool{}}
	r := &Runner{Path: path, Spawn: fs.fn}

	t0 := mustTime(t, "2026-07-05T02:00:00")
	r.Tick(t0)
	r.Tick(t0.Add(10 * time.Minute))
	if len(fs.calls) != 0 {
		t.Fatalf("disabled schedule fired %d times, want 0", len(fs.calls))
	}
}

func TestRunnerReloadsFileEachTick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	writeScheds(t, path) // empty

	fs := &fakeSpawn{running: map[string]bool{}}
	r := &Runner{Path: path, Spawn: fs.fn}

	t0 := mustTime(t, "2026-07-05T02:00:00")
	r.Tick(t0) // nothing to arm

	// Add a schedule after the runner started.
	writeScheds(t, path, Schedule{ID: "s1", Project: "late", Expr: "every:5m", Enabled: true})
	r.Tick(t0.Add(1 * time.Minute)) // sees + arms it (next = +5m from here)
	if len(fs.calls) != 0 {
		t.Fatalf("newly-added schedule fired on arming tick")
	}
	r.Tick(t0.Add(6 * time.Minute)) // now due
	if len(fs.calls) != 1 || fs.calls[0].project != "late" {
		t.Fatalf("reloaded schedule did not fire: calls=%+v", fs.calls)
	}
}
