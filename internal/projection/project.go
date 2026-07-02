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

// Project folds events into State, preserving first-assign order for
// features/tasks and init order for seats. Unknown event_types are ignored.
func Project(evs []event.Event) State {
	st := State{Project: "unknown"}

	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EventType != "init" {
			continue
		}
		p := evs[i].Payload
		if v := str(p["project"]); v != "" {
			st.Project = v
		}
		if seats, ok := p["seats"].([]any); ok {
			for _, s := range seats {
				m, _ := s.(map[string]any)
				st.Agents = append(st.Agents, seatFromPayload(m))
			}
		}
		break
	}

	fIdx := map[string]int{}
	tIdx := map[string]map[string]int{}
	cancelled := map[string]bool{} // feature\x00task — retired task, excluded from the projection
	withdrawn := map[string]bool{} // feature — retired feature, excluded from the projection
	find := func(feature, task string) *Task {
		fi, ok := fIdx[feature]
		if !ok {
			return nil
		}
		ti, ok := tIdx[feature][task]
		if !ok {
			return nil
		}
		return &st.Features[fi].Tasks[ti]
	}

	// Known limitation (parity): for a checkpoint/accept/changes_requested/merge
	// that references a task/feature never created by an `assign`, this fold skips
	// the event, whereas the bash reference autovivifies a null-field stub. This
	// only differs on malformed / out-of-order logs — a well-formed log (and the
	// git-merge interleaving of distinct features, which preserves each feature's
	// intra-order) always has assign precede its task's other events. Reconcile if
	// concurrent-feature support ever makes dangling events real. See M1.2 design.
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
			fi, ok := fIdx[e.Feature]
			if !ok {
				st.Features = append(st.Features, Feature{ID: e.Feature, Branch: str(e.Payload["branch"]), Status: "in_progress"})
				fi = len(st.Features) - 1
				fIdx[e.Feature] = fi
				tIdx[e.Feature] = map[string]int{}
			}
			tk := Task{ID: e.TaskID, Owner: str(e.Payload["owner"]), Reviewer: str(e.Payload["reviewer"]), Spec: str(e.Payload["spec"]), Status: "assigned"}
			if raw, ok := e.Payload["deps"].([]any); ok && len(raw) > 0 {
				tk.Deps = make([]string, 0, len(raw))
				for _, d := range raw {
					tk.Deps = append(tk.Deps, str(d))
				}
			}
			if ti, ok := tIdx[e.Feature][e.TaskID]; ok {
				st.Features[fi].Tasks[ti] = tk
			} else {
				st.Features[fi].Tasks = append(st.Features[fi].Tasks, tk)
				tIdx[e.Feature][e.TaskID] = len(st.Features[fi].Tasks) - 1
			}
		case "join":
			for fi := range st.Features {
				for ti := range st.Features[fi].Tasks {
					t := &st.Features[fi].Tasks[ti]
					if t.Owner == e.AgentID && t.Status == "assigned" {
						t.Status = "in_progress"
					}
				}
			}
		case "start":
			// Task-scoped "working" fact recorded by the orchestrate driver when it
			// launches the task's owner (join is seat-scoped and worker-reported,
			// which headless workers skip). Only lifts a task out of `assigned` —
			// never rewinds checkpoint/review outcomes.
			if t := find(e.Feature, e.TaskID); t != nil && t.Status == "assigned" {
				t.Status = "in_progress"
			}
		case "checkpoint":
			if t := find(e.Feature, e.TaskID); t != nil {
				t.Status = "awaiting_review"
				ev := str(e.Payload["evidence"])
				t.Evidence = &ev
			}
		case "accept":
			if t := find(e.Feature, e.TaskID); t != nil {
				t.Status = "accepted"
			}
		case "changes_requested":
			if t := find(e.Feature, e.TaskID); t != nil {
				t.Status = "changes_requested"
			}
		case "merge":
			if fi, ok := fIdx[e.Feature]; ok {
				st.Features[fi].Status = "shipped"
			}
		case "cancel":
			cancelled[e.Feature+"\x00"+e.TaskID] = true
		case "withdraw":
			withdrawn[e.Feature] = true
		}
	}

	// Retire cancelled tasks and withdrawn features. Excluding them here (rather
	// than mutating during the fold) keeps the event handlers simple and order-
	// independent: a cancel/withdraw anywhere in the log removes the target.
	if len(cancelled) > 0 || len(withdrawn) > 0 {
		kept := st.Features[:0]
		for _, f := range st.Features {
			if withdrawn[f.ID] {
				continue
			}
			tasks := f.Tasks[:0]
			for _, t := range f.Tasks {
				if !cancelled[f.ID+"\x00"+t.ID] {
					tasks = append(tasks, t)
				}
			}
			f.Tasks = tasks
			kept = append(kept, f)
		}
		st.Features = kept
	}
	return st
}
