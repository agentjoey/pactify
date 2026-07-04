package schedule

import (
	"context"
	"sync"
	"time"
)

// SpawnFunc launches an orchestrate run for a project (by registry name);
// feature "" means the driver picks. It returns alreadyRunning=true when the
// existing conflict guard skipped the spawn (an orchestrate is already live for
// that project), and a non-nil err only for a real failure.
type SpawnFunc func(project, feature string) (alreadyRunning bool, err error)

// Runner is a minute-granularity scheduler: on each Tick it (re)reads the
// schedules file and fires spawn for every enabled schedule whose next fire time
// has passed. It holds no wall-clock dependency — Start injects a clock and the
// tests drive Tick directly with a controlled `now`.
type Runner struct {
	Path  string                           // schedules.json path
	Spawn SpawnFunc                        // injected orchestrate launcher
	Now   func() time.Time                 // clock (Start defaults to time.Now)
	Log   func(format string, args ...any) // optional log sink

	mu   sync.Mutex
	next map[string]time.Time // schedule id -> computed next fire time
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
	}
}

// Tick evaluates all schedules against now, firing any that are due. A schedule
// seen for the first time is scheduled forward (it does not fire on the tick that
// first observes it), so a serve restart never triggers an immediate run. Removed
// or disabled schedules are forgotten so re-enabling re-arms cleanly.
func (r *Runner) Tick(now time.Time) {
	scheds, err := Load(r.Path)
	if err != nil {
		r.logf("schedule: load %s: %v", r.Path, err)
		return
	}

	r.mu.Lock()
	if r.next == nil {
		r.next = map[string]time.Time{}
	}
	seen := map[string]bool{}
	var due []Schedule
	for _, s := range scheds {
		if !s.Enabled {
			continue
		}
		seen[s.ID] = true
		nf, tracked := r.next[s.ID]
		if !tracked {
			// First sighting: arm forward, do not fire now.
			if t, err := NextFire(now, s.Expr); err == nil {
				r.next[s.ID] = t
			} else {
				r.logf("schedule %s: bad expr %q: %v", s.ID, s.Expr, err)
			}
			continue
		}
		if !now.Before(nf) {
			due = append(due, s)
			if t, err := NextFire(now, s.Expr); err == nil {
				r.next[s.ID] = t
			}
		}
	}
	for id := range r.next {
		if !seen[id] {
			delete(r.next, id)
		}
	}
	r.mu.Unlock()

	for _, s := range due {
		if r.Spawn == nil {
			continue
		}
		already, err := r.Spawn(s.Project, s.Feature)
		switch {
		case err != nil:
			r.logf("schedule %s: spawn %s failed: %v", s.ID, s.Project, err)
		case already:
			r.logf("schedule %s: %s already running — skipped", s.ID, s.Project)
		default:
			r.logf("schedule %s: triggered orchestrate for %s", s.ID, s.Project)
		}
	}
}

// Start runs the minute-granularity ticker until ctx is cancelled. It performs
// one initial Tick (which only arms schedules) and then fires on the wall clock.
func (r *Runner) Start(ctx context.Context) {
	if r.Now == nil {
		r.Now = time.Now
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	r.Tick(r.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Tick(r.Now())
		}
	}
}
