// Package projection folds the event log into a read-model State and renders it.
package projection

import "github.com/agentjoey/pactify/internal/event"

type Seat struct {
	ID    string
	Roles []string
	Entry string
	Kind  string // agent kind (from init/add-seat payload); "" = shell/legacy
}

type Task struct {
	ID, Owner, Status, Reviewer, Spec string
	Evidence                          *string
	// Deps holds task dependencies (additive v1; see protocol addendum). It is
	// non-nil ONLY when the assign event carried a non-empty deps array, so the
	// renderer can omit the `deps:` line entirely for deps-free tasks and keep
	// STATE.yml byte-identical to the bash reference.
	Deps []string
}

type Feature struct {
	ID, Branch, Status string
	Tasks              []Task
}

type State struct {
	Project  string
	Agents   []Seat
	Features []Feature
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// taskInFeature returns the feature's task with the given id, or nil.
func taskInFeature(f *Feature, taskID string) *Task {
	for ti := range f.Tasks {
		if f.Tasks[ti].ID == taskID {
			return &f.Tasks[ti]
		}
	}
	return nil
}

// seatFromPayload builds a Seat from an init/add-seat payload map (id/roles/entry/kind).
func seatFromPayload(m map[string]any) Seat {
	seat := Seat{ID: str(m["id"]), Entry: str(m["entry"]), Kind: str(m["kind"])}
	if rs, ok := m["roles"].([]any); ok {
		for _, r := range rs {
			seat.Roles = append(seat.Roles, str(r))
		}
	}
	return seat
}

// Folder is a resumable projection accumulator. It holds the PRE-retirement state
// (cancelled tasks and withdrawn features are still present here — the retire step
// runs only in finalize) plus the bookkeeping a fold needs to apply more events.
// Storing this — rather than the finalized State — is what lets the ledger snapshot
// resume folding at a byte offset and produce a result byte-for-byte identical to a
// full Project() fold, even for pathological logs (e.g. an assign that re-uses a
// cancelled task id, which a finalized-state resume would get wrong because the
// cancelled set is sticky). Fields are unexported; snapshot.go (same package)
// serializes st/cancelled/withdrawn and rebuilds fIdx/tIdx on restore.
type Folder struct {
	st        State
	fIdx      map[string]int
	tIdx      map[string]map[string]int
	cancelled map[string]bool // feature\x00task — retired task, excluded from the projection
	withdrawn map[string]bool // feature — retired feature, excluded from the projection
}

// newFolder seeds a fresh accumulator from the log's init event(s): it takes the
// LAST init's project name and seats, mirroring the historical first-scan. Only
// the seed comes from here; every other event is replayed by apply.
func newFolder(evs []event.Event) *Folder {
	f := &Folder{
		st:        State{Project: "unknown"},
		fIdx:      map[string]int{},
		tIdx:      map[string]map[string]int{},
		cancelled: map[string]bool{},
		withdrawn: map[string]bool{},
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EventType != "init" {
			continue
		}
		p := evs[i].Payload
		if v := str(p["project"]); v != "" {
			f.st.Project = v
		}
		if seats, ok := p["seats"].([]any); ok {
			for _, s := range seats {
				m, _ := s.(map[string]any)
				f.st.Agents = append(f.st.Agents, seatFromPayload(m))
			}
		}
		break
	}
	return f
}

func (f *Folder) find(feature, task string) *Task {
	fi, ok := f.fIdx[feature]
	if !ok {
		return nil
	}
	ti, ok := f.tIdx[feature][task]
	if !ok {
		return nil
	}
	return &f.st.Features[fi].Tasks[ti]
}

// apply replays events onto the accumulator. It handles every event type EXCEPT
// init (whose seat/project seeding lives in newFolder); an incremental replay
// therefore must never contain an init in its tail (the snapshot reader guards
// this by falling back to a full fold when a tail init appears).
//
// Known limitation (parity): for a checkpoint/accept/changes_requested/merge that
// references a task/feature never created by an `assign`, this fold skips the
// event, whereas the bash reference autovivifies a null-field stub. This only
// differs on malformed / out-of-order logs — a well-formed log always has assign
// precede its task's other events. See M1.2 design.
func (f *Folder) apply(evs []event.Event) {
	st := &f.st
	for _, e := range evs {
		switch e.EventType {
		case "add-seat":
			seat := seatFromPayload(e.Payload)
			exists := false
			for _, a := range st.Agents {
				if a.ID == seat.ID {
					exists = true
				}
			}
			if !exists {
				st.Agents = append(st.Agents, seat)
			}
		case "assign":
			fi, ok := f.fIdx[e.Feature]
			if !ok {
				st.Features = append(st.Features, Feature{ID: e.Feature, Branch: str(e.Payload["branch"]), Status: "in_progress"})
				fi = len(st.Features) - 1
				f.fIdx[e.Feature] = fi
				f.tIdx[e.Feature] = map[string]int{}
			}
			tk := Task{ID: e.TaskID, Owner: str(e.Payload["owner"]), Reviewer: str(e.Payload["reviewer"]), Spec: str(e.Payload["spec"]), Status: "assigned"}
			if raw, ok := e.Payload["deps"].([]any); ok && len(raw) > 0 {
				tk.Deps = make([]string, 0, len(raw))
				for _, d := range raw {
					tk.Deps = append(tk.Deps, str(d))
				}
			}
			if ti, ok := f.tIdx[e.Feature][e.TaskID]; ok {
				st.Features[fi].Tasks[ti] = tk
			} else {
				st.Features[fi].Tasks = append(st.Features[fi].Tasks, tk)
				f.tIdx[e.Feature][e.TaskID] = len(st.Features[fi].Tasks) - 1
			}
		case "join":
			// Seat-scoped cold-start signal: lift ONLY the seat's first actionable
			// owned task — assigned with every dep accepted — mirroring the branch
			// Join itself checks out (engine JoinWithClient). Flipping every
			// assigned task the seat owns corrupted the board: dep-blocked and
			// never-started tasks showed in_progress, and the join gate lost its
			// startable set. Task-scoped `start` covers the orchestrate path.
			lifted := false
			for fi := range st.Features {
				if lifted {
					break
				}
				fe := &st.Features[fi]
				for ti := range fe.Tasks {
					t := &fe.Tasks[ti]
					if t.Owner != e.AgentID || t.Status != "assigned" || f.cancelled[fe.ID+"\x00"+t.ID] {
						continue
					}
					ready := true
					for _, d := range t.Deps {
						// A dep cancelled so far in the log (or absent) can never
						// reach accepted; it does not hold this task back.
						dep := taskInFeature(fe, d)
						if dep != nil && !f.cancelled[fe.ID+"\x00"+d] && dep.Status != "accepted" {
							ready = false
							break
						}
					}
					if ready {
						t.Status = "in_progress"
						lifted = true
						break
					}
				}
			}
		case "start":
			// Task-scoped "working" fact recorded by the orchestrate driver when it
			// launches the task's owner (join is seat-scoped and worker-reported,
			// which headless workers skip). Only lifts a task out of `assigned` —
			// never rewinds checkpoint/review outcomes.
			if t := f.find(e.Feature, e.TaskID); t != nil && t.Status == "assigned" {
				t.Status = "in_progress"
			}
		case "checkpoint":
			if t := f.find(e.Feature, e.TaskID); t != nil {
				t.Status = "awaiting_review"
				ev := str(e.Payload["evidence"])
				t.Evidence = &ev
			}
		case "accept":
			if t := f.find(e.Feature, e.TaskID); t != nil {
				t.Status = "accepted"
			}
		case "changes_requested":
			if t := f.find(e.Feature, e.TaskID); t != nil {
				t.Status = "changes_requested"
			}
		case "merge":
			if fi, ok := f.fIdx[e.Feature]; ok {
				st.Features[fi].Status = "shipped"
			}
		case "cancel":
			f.cancelled[e.Feature+"\x00"+e.TaskID] = true
		case "withdraw":
			f.withdrawn[e.Feature] = true
		}
	}
}

// finalize projects the accumulator into the public State, retiring cancelled
// tasks and withdrawn features. It is NON-destructive: f.st (the pre-retirement
// accumulator) is left intact so the same Folder can be both snapshotted and
// resumed later. A cancel/withdraw anywhere in the log removes the target.
func (f *Folder) finalize() State {
	st := f.st
	if len(f.cancelled) == 0 && len(f.withdrawn) == 0 {
		return st
	}
	kept := make([]Feature, 0, len(st.Features))
	for _, ft := range st.Features {
		if f.withdrawn[ft.ID] {
			continue
		}
		tasks := make([]Task, 0, len(ft.Tasks))
		for _, t := range ft.Tasks {
			if !f.cancelled[ft.ID+"\x00"+t.ID] {
				tasks = append(tasks, t)
			}
		}
		ft.Tasks = tasks
		kept = append(kept, ft)
	}
	st.Features = kept
	return st
}

// Project folds events into State, preserving first-assign order for
// features/tasks and init order for seats. Unknown event_types are ignored.
func Project(evs []event.Event) State {
	st, _ := Fold(evs)
	return st
}

// Fold folds events into the finalized public State AND returns the resumable
// accumulator behind it (pre-retirement state + cancelled/withdrawn sets). The
// snapshot writer keeps the Folder; every other caller uses only the State.
func Fold(evs []event.Event) (State, *Folder) {
	f := newFolder(evs)
	f.apply(evs)
	return f.finalize(), f
}
