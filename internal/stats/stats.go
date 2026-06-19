// Package stats derives per-task and per-agent work statistics from the event
// log — WITHOUT touching the core projection (it's a read-only derived view).
// D1 covers duration; code volume (added/deleted) and tokens are best-effort
// fields filled in later increments (0 when unknown).
package stats

import (
	"sort"
	"time"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/projection"
)

// TaskStat is one task's work statistics. Added/Deleted/Tokens are best-effort
// (0 when unknown) until the LOC + token increments land.
type TaskStat struct {
	TaskID      string `json:"task_id"`
	Feature     string `json:"feature"`
	Owner       string `json:"owner"`
	Reviewer    string `json:"reviewer"`
	Status      string `json:"status"`
	DurationSec int64  `json:"duration_sec"`
	Added       int    `json:"added"`
	Deleted     int    `json:"deleted"`
	Tokens      int    `json:"tokens"`
}

// AgentStat rolls a seat's owned tasks up into totals.
type AgentStat struct {
	Seat        string `json:"seat"`
	Tasks       int    `json:"tasks"`
	DurationSec int64  `json:"duration_sec"`
	Added       int    `json:"added"`
	Deleted     int    `json:"deleted"`
	Tokens      int    `json:"tokens"`
}

// Stats is the full derived view: every task + a per-owner rollup.
type Stats struct {
	Tasks  []TaskStat  `json:"tasks"`
	Agents []AgentStat `json:"agents"`
}

type timing struct {
	assign         time.Time
	lastCheckpoint time.Time
	accept         time.Time
}

// Compute derives stats from the ordered event log. `now` is the clock used to
// time still-running tasks (injected for testability). Duration is measured from
// a task's first `assign` to its terminal moment: accept (accepted), the last
// checkpoint (awaiting_review / changes_requested), else `now` (assigned /
// in_progress).
func Compute(events []event.Event, now time.Time) Stats {
	state := projection.Project(events)

	tm := map[string]*timing{}
	for _, e := range events {
		if e.TaskID == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.TS)
		if err != nil {
			continue
		}
		t := tm[e.TaskID]
		if t == nil {
			t = &timing{}
			tm[e.TaskID] = t
		}
		switch e.EventType {
		case "assign":
			if t.assign.IsZero() {
				t.assign = ts
			}
		case "checkpoint":
			t.lastCheckpoint = ts
		case "accept":
			t.accept = ts
		}
	}

	out := Stats{Tasks: []TaskStat{}, Agents: []AgentStat{}}
	roll := map[string]*AgentStat{}
	for _, f := range state.Features {
		for _, t := range f.Tasks {
			ts := TaskStat{
				TaskID:      t.ID,
				Feature:     f.ID,
				Owner:       t.Owner,
				Reviewer:    t.Reviewer,
				Status:      t.Status,
				DurationSec: durationSec(tm[t.ID], t.Status, now),
			}
			out.Tasks = append(out.Tasks, ts)

			if t.Owner != "" {
				a := roll[t.Owner]
				if a == nil {
					a = &AgentStat{Seat: t.Owner}
					roll[t.Owner] = a
				}
				a.Tasks++
				a.DurationSec += ts.DurationSec
				a.Added += ts.Added
				a.Deleted += ts.Deleted
				a.Tokens += ts.Tokens
			}
		}
	}

	for _, a := range roll {
		out.Agents = append(out.Agents, *a)
	}
	sort.Slice(out.Agents, func(i, j int) bool { return out.Agents[i].Seat < out.Agents[j].Seat })
	return out
}

// WithTaskLOC fills per-task code volume from a taskID→(added,deleted) provider
// (the diff of the task's OWN checkpoint commits, excluding .pact bookkeeping)
// and adds it to the per-agent rollup, summed across the seat's owned tasks.
// Unlike a feature-branch diff — which makes every task on one branch show the
// same branch total — this attributes each task only the lines its own commits
// changed. Mirrors WithTokens' per-task shape.
func (s Stats) WithTaskLOC(loc func(taskID string) (added, deleted int)) Stats {
	loc2 := map[string][2]int{}
	for i := range s.Tasks {
		a, d := loc(s.Tasks[i].TaskID)
		s.Tasks[i].Added, s.Tasks[i].Deleted = a, d
		loc2[s.Tasks[i].TaskID] = [2]int{a, d}
	}
	roll := map[string]*AgentStat{}
	for i := range s.Agents {
		cp := s.Agents[i]
		cp.Added, cp.Deleted = 0, 0
		roll[s.Agents[i].Seat] = &cp
	}
	for _, t := range s.Tasks {
		if a := roll[t.Owner]; a != nil {
			v := loc2[t.TaskID]
			a.Added += v[0]
			a.Deleted += v[1]
		}
	}
	out := s
	out.Agents = make([]AgentStat, 0, len(s.Agents))
	for i := range s.Agents {
		out.Agents = append(out.Agents, *roll[s.Agents[i].Seat])
	}
	return out
}

// WithTokens fills per-task token usage from a taskID→tokens provider and adds
// it to the per-agent rollup (summed across the seat's owned tasks).
func (s Stats) WithTokens(get func(taskID string) int) Stats {
	tok := map[string]int{}
	for i := range s.Tasks {
		n := get(s.Tasks[i].TaskID)
		s.Tasks[i].Tokens = n
		tok[s.Tasks[i].TaskID] = n
	}
	roll := map[string]*AgentStat{}
	for i := range s.Agents {
		cp := s.Agents[i]
		cp.Tokens = 0
		roll[s.Agents[i].Seat] = &cp
	}
	for _, t := range s.Tasks {
		if a := roll[t.Owner]; a != nil {
			a.Tokens += tok[t.TaskID]
		}
	}
	out := s
	out.Agents = make([]AgentStat, 0, len(s.Agents))
	for i := range s.Agents {
		out.Agents = append(out.Agents, *roll[s.Agents[i].Seat])
	}
	return out
}

func durationSec(t *timing, status string, now time.Time) int64 {
	if t == nil || t.assign.IsZero() {
		return 0
	}
	var end time.Time
	switch status {
	case "accepted":
		end = t.accept
	case "awaiting_review", "changes_requested":
		end = t.lastCheckpoint
	default: // assigned / in_progress / anything ongoing
		end = now
	}
	if end.IsZero() {
		end = now
	}
	if d := end.Sub(t.assign); d > 0 {
		return int64(d.Seconds())
	}
	return 0
}
