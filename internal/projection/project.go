// Package projection folds the event log into a read-model State and renders it.
package projection

import "github.com/agentjoey/pactify/internal/event"

type Seat struct {
	ID    string
	Roles []string
}

type Task struct {
	ID, Owner, Status, Reviewer, Spec string
	Evidence                          *string
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
				seat := Seat{ID: str(m["id"])}
				if rs, ok := m["roles"].([]any); ok {
					for _, r := range rs {
						seat.Roles = append(seat.Roles, str(r))
					}
				}
				st.Agents = append(st.Agents, seat)
			}
		}
		break
	}

	fIdx := map[string]int{}
	tIdx := map[string]map[string]int{}
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

	for _, e := range evs {
		switch e.EventType {
		case "assign":
			fi, ok := fIdx[e.Feature]
			if !ok {
				st.Features = append(st.Features, Feature{ID: e.Feature, Branch: str(e.Payload["branch"]), Status: "in_progress"})
				fi = len(st.Features) - 1
				fIdx[e.Feature] = fi
				tIdx[e.Feature] = map[string]int{}
			}
			tk := Task{ID: e.TaskID, Owner: str(e.Payload["owner"]), Reviewer: str(e.Payload["reviewer"]), Spec: str(e.Payload["spec"]), Status: "assigned"}
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
		}
	}
	return st
}
