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
	// Reviewers / Quorum implement opt-in multi-reviewer quorum review (spec
	// review-runtime-deepening §2). They are populated ONLY from a quorum assign
	// (payload `reviewers[]` + `quorum`); a legacy single-`reviewer` assign leaves
	// Reviewers nil and Quorum 0, so its projection, STATE.yml render, snapshot and
	// review guards stay byte-identical to the pre-feature behavior. When set,
	// Reviewer carries the FIRST reviewer for back-compat consumers of the single
	// field.
	Reviewers []string
	Quorum    int
	// Accepts is the CURRENT review round's distinct reviewer seats that have
	// accepted (since the most recent checkpoint). It is the quorum tally: the fold
	// marks the task accepted once len(Accepts) ≥ Quorum. A new checkpoint or any
	// reviewer's changes_requested clears it (re-checkpoint → all reviewers re-vote).
	// nil for legacy single-reviewer tasks (never touched → byte-identical).
	Accepts []string
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

// intFromPayload coerces a JSON-decoded payload number to int. event.ParseAll
// unmarshals JSON numbers to float64, so `quorum` arrives as a float64; a snapshot
// resume rebuilds the Folder from a JSON State where an int survives as float64
// too. Both are handled; anything else yields 0.
func intFromPayload(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// strSlice pulls a []string out of a JSON-decoded []any payload value (e.g. the
// quorum assign's `reviewers`), skipping non-string entries. Returns nil for a
// missing/empty/wrong-typed value so quorum-free tasks keep a nil slice.
func strSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		out = append(out, str(e))
	}
	return out
}

// contains reports whether s is in xs.
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
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
			// Quorum multi-reviewer (opt-in): a quorum assign carries `reviewers[]` +
			// `quorum` instead of a single `reviewer`. Populate the list and mirror the
			// first reviewer into the single Reviewer field for back-compat consumers.
			// A legacy single-reviewer assign has no `reviewers` key → Reviewers stays
			// nil, Quorum 0, and everything below is byte-identical to before.
			if revs := strSlice(e.Payload["reviewers"]); len(revs) > 0 {
				tk.Reviewers = revs
				tk.Quorum = intFromPayload(e.Payload["quorum"])
				if tk.Reviewer == "" {
					tk.Reviewer = revs[0]
				}
			}
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
			// Dynamic-seat roster write (spec §6 WS-K): a join may carry `kind` (and
			// roles) so a seat can DECLARE/refresh how it is driven, and a planner-
			// proposed seat can enter the roster without an init/add-seat. Record it:
			// refresh an existing seat's kind (roles only when it had none, so an init
			// seat's roles are never clobbered), or append a brand-new roster seat.
			// A kind-free join by an existing roster seat mutates nothing here, keeping
			// pre-feature logs byte-identical.
			jkind := str(e.Payload["kind"])
			jroles := strSlice(e.Payload["roles"])
			found := false
			for ai := range st.Agents {
				if st.Agents[ai].ID != e.AgentID {
					continue
				}
				found = true
				if jkind != "" {
					st.Agents[ai].Kind = jkind
				}
				if len(st.Agents[ai].Roles) == 0 && len(jroles) > 0 {
					st.Agents[ai].Roles = jroles
				}
			}
			if !found {
				st.Agents = append(st.Agents, Seat{ID: e.AgentID, Kind: jkind, Roles: jroles})
			}
			// Seat-scoped cold-start signal: lift ONLY the seat's first actionable
			// owned task — assigned with every dep accepted — mirroring the branch
			// Join itself checks out (engine JoinWithClient). Flipping every
			// assigned task the seat owns corrupted the board: dep-blocked and
			// never-started tasks showed in_progress, and the join gate lost its
			// startable set. Task-scoped `start` covers the orchestrate path.
			// A TARGETED join (payload `task`, engine JoinTask) lifts exactly the
			// named task instead — and nothing when that task isn't liftable, so
			// replay of arbitrary logs stays deterministic.
			jtask := str(e.Payload["task"])
			lifted := false
			for fi := range st.Features {
				if lifted {
					break
				}
				fe := &st.Features[fi]
				for ti := range fe.Tasks {
					t := &fe.Tasks[ti]
					if jtask != "" && t.ID != jtask {
						continue
					}
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
				// New review round: clear the quorum tally so accepts are counted only
				// from this checkpoint forward (no-op for legacy tasks — Accepts is nil).
				t.Accepts = nil
			}
		case "accept":
			if t := f.find(e.Feature, e.TaskID); t != nil {
				if len(t.Reviewers) > 0 {
					// Quorum task: tally DISTINCT reviewer seats accepting since the last
					// checkpoint; the task only becomes accepted once the tally reaches
					// quorum. Below quorum it stays awaiting_review so the remaining
					// reviewers can still vote.
					if !contains(t.Accepts, e.AgentID) {
						t.Accepts = append(t.Accepts, e.AgentID)
					}
					if t.Quorum > 0 && len(t.Accepts) >= t.Quorum {
						t.Status = "accepted"
					}
				} else {
					// Legacy single-reviewer: one accept → accepted (byte-identical).
					t.Status = "accepted"
				}
			}
		case "changes_requested":
			if t := f.find(e.Feature, e.TaskID); t != nil {
				t.Status = "changes_requested"
				// Any reviewer's changes ends this round and resets the quorum tally, so
				// after a re-checkpoint every reviewer votes afresh (no-op for legacy).
				t.Accepts = nil
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
	// A seat's roles must marshal as [] not null: an empty-role seat (joined
	// with no roles) otherwise projects to `roles: null`, which crashes
	// null-unsafe clients (the dashboard's Board does a.roles.length → blank
	// page for that whole project). Normalize in place so every projection
	// path emits a non-nil slice. (The TS fold already inits roles:[].)
	for i := range st.Agents {
		if st.Agents[i].Roles == nil {
			st.Agents[i].Roles = []string{}
		}
	}
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
