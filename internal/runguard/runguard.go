// Package runguard answers one question from a repo's orchestrate runtime
// files: is a driver live right now, and what is it driving?
//
// It exists so the staleness rule lives in exactly one place. serve already
// needed it to serialize driver spawns; the checkpoint guard needs the same
// answer to keep a manual checkpoint from sweeping another run's in-flight
// files into this task's commit (Checkpoint commits the whole tree).
package runguard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StaleAfter is how long a run's status may go un-restamped before it counts as
// dead. The driver heartbeats status.json every 3 minutes, so this window
// tolerates two missed beats. A crashed driver leaves done=false forever, and
// without this window it would wedge the repo shut.
const StaleAfter = 10 * time.Minute

// Run is a live driver detected from the repo's runtime files.
type Run struct {
	Feature string `json:"feature"`
	Task    string `json:"task"`
	Seat    string `json:"seat"`
	// Source is the runtime file this run was read from, relative to the repo
	// root, so an error message can point at the evidence.
	Source string `json:"-"`
}

// status is the subset of orchestrate.Status this package needs. It is
// deliberately structural (not an import of internal/orchestrate) so the
// engine/CLI side can depend on runguard without pulling in the driver.
type status struct {
	Feature   string `json:"feature"`
	Task      string `json:"task"`
	Seat      string `json:"seat"`
	Done      bool   `json:"done"`
	Escalated bool   `json:"escalated"`
	UpdatedAt string `json:"updated_at"`
}

func orchestrateDir(dir string) string { return filepath.Join(dir, ".pact", "orchestrate") }

// live reads one status file and reports the run it describes, if that run is
// still live. Missing, corrupt, finished, escalated and stale all mean "no live
// run" — never an error, because no evidence of a run must never block work.
func live(path, source string) (Run, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Run{}, false
	}
	var st status
	if json.Unmarshal(b, &st) != nil {
		return Run{}, false
	}
	if st.Done || st.Escalated {
		return Run{}, false
	}
	t, err := time.Parse(time.RFC3339, st.UpdatedAt)
	if err != nil || time.Since(t) >= StaleAfter {
		return Run{}, false
	}
	return Run{Feature: st.Feature, Task: st.Task, Seat: st.Seat, Source: source}, true
}

// Serial reports the run in .pact/orchestrate/status.json, which only the
// serial driver writes.
func Serial(dir string) (Run, bool) {
	return live(filepath.Join(orchestrateDir(dir), "status.json"), filepath.Join(".pact", "orchestrate", "status.json"))
}

// Live returns every run currently driving dir: the serial status.json plus one
// entry per in-flight feature under parallel/. A parallel driver never writes
// status.json, so reading the serial file alone misses concurrent runs.
func Live(dir string) []Run {
	var runs []Run
	if r, ok := Serial(dir); ok {
		runs = append(runs, r)
	}
	parallel := filepath.Join(orchestrateDir(dir), "parallel")
	entries, err := os.ReadDir(parallel)
	if err != nil {
		return runs
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		source := filepath.Join(".pact", "orchestrate", "parallel", e.Name())
		if r, ok := live(filepath.Join(parallel, e.Name()), source); ok {
			runs = append(runs, r)
		}
	}
	return runs
}

// CheckpointBlocked describes the runs that make a manual checkpoint of task
// unsafe, or "" when the checkpoint is safe. Callers append their own escape
// hint (the CLI has --force; other surfaces point at it).
func CheckpointBlocked(dir, task string) string {
	blocking := BlocksCheckpoint(dir, task)
	if len(blocking) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("an orchestrate run is in flight in this repo:")
	for _, r := range blocking {
		b.WriteString("\n  - ")
		if r.Feature != "" {
			fmt.Fprintf(&b, "feature %s, ", r.Feature)
		}
		fmt.Fprintf(&b, "task %s", r.Task)
		if r.Seat != "" {
			fmt.Fprintf(&b, ", seat %s", r.Seat)
		}
		fmt.Fprintf(&b, " (%s)", r.Source)
	}
	fmt.Fprintf(&b, "\ncheckpoint commits the whole worktree, so it would sweep that run's in-flight files into %s.", task)
	return b.String()
}

// BlocksCheckpoint returns the live runs that make a manual checkpoint of task
// unsafe — every live run except one already driving task itself.
//
// The exemption is load-bearing: the briefing tells each worker to finish with
// `pactify checkpoint <task>` (brief.go), so blocking a run's own task would
// break every orchestrated handoff. What stays blocked is the dangerous case:
// checkpointing task A while a driver is mid-stint on task B, which commits B's
// half-written files under A's name.
func BlocksCheckpoint(dir, task string) []Run {
	var blocking []Run
	for _, r := range Live(dir) {
		if r.Task == task {
			continue
		}
		blocking = append(blocking, r)
	}
	return blocking
}
