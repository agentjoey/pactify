package sub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func routerFile(t *testing.T, s Store, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(s.Root), "router.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestRouterConfigAndAliases(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	r, err := s.LoadRouter()
	if err != nil || r != nil {
		t.Fatalf("missing: %v %v", r, err)
	}
	routerFile(t, s, `{"aliases":{"cheap":{"harness":"opencode","model":"m","effort":"low"}},"fallback":{"codex":["@cheap","kimi"]}}`)
	r, err = s.LoadRouter()
	if err != nil {
		t.Fatal(err)
	}
	route, err := r.Resolve("@cheap")
	if err != nil || route.Model != "m" || route.Effort != "low" {
		t.Fatalf("%+v %v", route, err)
	}
	if _, err = r.Resolve("@missing"); err == nil || !strings.Contains(err.Error(), "@cheap") {
		t.Fatal(err)
	}
	for _, body := range []string{`null`, `{"aliases":[]}`, `{"wat":1}`, `{"aliases":{"bad":{"harness":"nope"}}}`, `{"fallback":{"codex":["@missing"]}}`, `{"quotaGroups":{"g":{"harness":"agy","models":[]}}}`, `{"aliases":{"bad":{"harness":"codex","modle":"x"}}}`, `{"fallback":{"nope":[]}}`, `{"aliases":{"@bad":{"harness":"codex"}}}`, `{"fallback":{"codex":null}}`, `{"aliases":{"bad":null}}`, `{} {}`} {
		routerFile(t, s, body)
		if _, err = s.LoadRouter(); err == nil || !strings.Contains(err.Error(), "router.json") {
			t.Fatalf("body=%s err=%v", body, err)
		}
	}
}
func TestRouterSelection(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Hour)
	r := &Router{Aliases: map[string]Route{"mm": {Harness: "opencode", Model: "mini"}}, Fallback: map[string][]string{"agy": {"@mm", "kimi", "codex"}}, QuotaGroups: map[string]QuotaGroup{"argon": {Harness: "agy", Models: []string{"low", "high"}}}}
	preferred := Route{Harness: "agy", Model: "high"}
	for _, q := range []QuotaRoute{{Harness: "agy"}, {Harness: "agy", Model: "high"}, {Harness: "agy", Model: "low"}} {
		q.RecordedAt = now
		q.ResetAt = &later
		got, remaining, warnings, err := r.Select([]Route{preferred, {Harness: "opencode", Model: "mini"}, {Harness: "kimi"}}, []QuotaRoute{q}, "write", now)
		if err != nil || got.Harness != "opencode" || len(remaining) != 1 || len(warnings) == 0 {
			t.Fatalf("%+v %v %v %v", got, remaining, warnings, err)
		}
	}
	got, _, _, err := r.Select([]Route{preferred, {Harness: "codex"}}, []QuotaRoute{{Harness: "agy", Model: "unrelated", RecordedAt: now}}, "write", now)
	if err != nil || got != preferred {
		t.Fatalf("%+v %v", got, err)
	}
	// read-only skips only harnesses that refuse it (kimi), in both
	// the chosen route and the remaining fallback chain.
	got, remaining, _, err := r.Select([]Route{preferred, {Harness: "kimi"}, {Harness: "opencode", Model: "mini"}, {Harness: "codex"}}, nil, "read-only", now)
	if err != nil || got != preferred || len(remaining) != 2 || remaining[0].Harness != "opencode" || remaining[1].Harness != "codex" {
		t.Fatalf("read-only agy: %+v %v %v", got, remaining, err)
	}
	got, _, _, err = r.Select([]Route{{Harness: "kimi"}, {Harness: "opencode", Model: "mini"}}, nil, "read-only", now)
	if err != nil || got.Harness != "opencode" {
		t.Fatalf("read-only opencode: %+v %v", got, err)
	}
	if _, _, _, err = r.Select([]Route{{Harness: "kimi"}, {Harness: "nope"}}, nil, "read-only", now); err == nil || !strings.Contains(err.Error(), "no routes support read-only access") {
		t.Fatalf("kimi/unknown read-only must be refused: %v", err)
	}
	_, _, _, err = r.Select([]Route{preferred}, []QuotaRoute{{Harness: "agy", ResetAt: &later, RecordedAt: now}}, "write", now)
	if err == nil || !strings.Contains(err.Error(), later.UTC().Format(time.RFC3339)) {
		t.Fatal(err)
	}
}
func TestQuotaFallbackBaseline(t *testing.T) {
	for _, unborn := range []bool{false, true} {
		for _, changed := range []bool{false, true} {
			t.Run(routerBool(unborn)+routerBool(changed), func(t *testing.T) {
				s, sess, b := workerFixture(t, "write")
				st, _ := s.Load("j1")
				st.Request.Harness = "codex"
				if unborn {
					st.Request.Cwd = unbornRepo(t)
					write(t, st.Request.Cwd, "pre.txt", "before\n")
				}
				st.Routing = &Routing{Remaining: []Route{{Harness: "kimi"}}}
				_ = s.Save(st)
				sess.script = func(f *fakeSession) {
					if changed {
						write(t, st.Request.Cwd, "new.txt", "changed\n")
					}
					f.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "usage limit"}
				}
				result, err := RunJob(context.Background(), s, "j1", newFakeHarness(b), WorkerConfig{})
				if err != nil {
					t.Fatal(err)
				}
				old, _ := s.Load("j1")
				if changed {
					if old.FallbackTo != "" || !contains(result.Warnings, "review the diff, then: pactify sub run kimi --worktree-from j1") {
						t.Fatalf("%+v %+v", old, result)
					}
				} else {
					if old.FallbackTo == "" || result.FallbackTo != old.FallbackTo {
						t.Fatalf("%+v %+v", old, result)
					}
					child, err := s.Load(old.FallbackTo)
					if err != nil || child.FallbackFrom != "j1" || child.Request.Brief != st.Request.Brief || child.Request.Resume != "" {
						t.Fatalf("%+v %v", child, err)
					}
				}
			})
		}
	}
}
func routerBool(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
func TestWorktreeFromTransfersAndPreservesBaseline(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	req := Request{Harness: "codex", Cwd: gitRepo(t), Access: "write", Brief: "first"}
	old, err := s.CreateRun("old", req, true, false)
	if err != nil {
		t.Fatal(err)
	}
	write(t, old.Worktree.Cwd, "new.txt", "change\n")
	result := Result{JobID: "old", Status: StatusFailed}
	if err = s.Finish("old", &result, "failed", time.Now()); err != nil {
		t.Fatal(err)
	}
	req.Harness = "kimi"
	req.Brief = "reviewed"
	next, err := s.WorktreeFrom("new", req, "old")
	if err != nil {
		t.Fatal(err)
	}
	if next.Worktree.Path != old.Worktree.Path || next.Worktree.BaseCommit != old.Worktree.BaseCommit || next.Request.Resume != "" || next.Request.Harness != "kimi" {
		t.Fatalf("%+v", next)
	}
	if _, err = s.WorktreeFrom("again", req, "old"); err == nil {
		t.Fatal("reused transferred worktree")
	}
	next.Status = StatusSucceeded
	_ = s.Save(next)
	if err = s.ApplyWorktree("new"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(req.Cwd, "new.txt"))
	if string(data) != "change\n" {
		t.Fatal(string(data))
	}
	if err = s.DiscardWorktree("new"); err != nil {
		t.Fatal(err)
	}
	plain, _ := s.Create("plain", req)
	_, err = s.WorktreeFrom("bad", req, plain.ID)
	if err == nil {
		t.Fatal("accepted plain job")
	}
}
func TestFallbackSummaryCostAndFinal(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	old, _ := s.Create("old", Request{Harness: "codex"})
	next, _ := s.Create("next", Request{Harness: "kimi"})
	old.Status = StatusFailed
	old.FallbackTo = "next"
	_ = s.Save(old)
	next.FallbackFrom = "old"
	_ = s.Save(next)
	r := Result{JobID: "next", Status: StatusSucceeded}
	if err := s.Finish("next", &r, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	st, err := s.FinalJob("old")
	if err != nil || st.ID != "next" {
		t.Fatalf("%+v %v", st, err)
	}
	row := s.Summarize(st, nil, time.Now())
	if row.FallbackFrom != "old" || !strings.Contains(row.StatusText(time.Now()), "fallback from old") {
		t.Fatalf("%+v", row)
	}
	report, err := s.CostReport(time.Now().Add(-time.Hour), "harness", time.Now())
	if err != nil || report.Total.Fallbacks != 1 {
		data, _ := json.Marshal(report)
		t.Fatalf("%s %v", data, err)
	}
	next.FallbackTo = "old"
	_ = s.Save(next)
	if _, err = s.FinalJob("old"); err == nil {
		t.Fatal("cycle accepted")
	}
}

func TestFallbackDoesNotIgnoreRemovedUserFile(t *testing.T) {
	s, sess, b := workerFixture(t, "write")
	st, _ := s.Load("j1")
	st.Request.Harness = "codex"
	st.Routing = &Routing{Remaining: []Route{{Harness: "kimi"}}}
	_ = s.Save(st)
	write(t, st.Request.Cwd, "preexisting.txt", "user content\n")
	sess.script = func(f *fakeSession) {
		_ = os.Remove(filepath.Join(st.Request.Cwd, "preexisting.txt"))
		f.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "usage limit"}
	}
	r, err := RunJob(context.Background(), s, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil || r.FallbackTo != "" || !contains(r.Warnings, "review the diff") {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestWorktreeFallbackAndResumeOwnership(t *testing.T) {
	s, sess, b := workerFixture(t, "write")
	initial, _ := s.Load("j1")
	old, err := s.CreateRun("isolated", Request{Harness: "codex", Cwd: initial.Request.Cwd, Access: "write", Brief: "task"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	old.Routing = &Routing{Remaining: []Route{{Harness: "kimi"}}}
	_ = s.Save(old)
	sess.script = func(f *fakeSession) {
		f.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "usage limit"}
	}
	r, err := RunJob(context.Background(), s, old.ID, newFakeHarness(b), WorkerConfig{})
	if err != nil || r.FallbackTo == "" {
		t.Fatalf("%+v %v", r, err)
	}
	child, _ := s.Load(r.FallbackTo)
	result := Result{JobID: child.ID, Status: StatusSucceeded, SessionID: "session"}
	if err = s.Finish(child.ID, &result, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	req := child.Request
	resumed, err := s.ResumeRun("resumed", req, false, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	final := Result{JobID: resumed.ID, Status: StatusSucceeded, SessionID: "session"}
	if err = s.Finish(resumed.ID, &final, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyWorktree(resumed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WorktreeFrom("stale", req, old.ID); err == nil {
		t.Fatal("old job still owns worktree")
	}
	if err = s.DiscardWorktree(resumed.ID); err != nil {
		t.Fatal(err)
	}
}
func TestFallbackUnavailableBaselineAndReadOnly(t *testing.T) {
	for _, access := range []string{"write", "read-only"} {
		s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
		st, _ := s.Create("j", Request{Harness: "codex", Access: access, Cwd: gitRepo(t)})
		st.Routing = &Routing{Remaining: []Route{{Harness: "kimi"}}}
		_ = s.Save(st)
		r := Result{JobID: st.ID, Status: StatusFailed, FailureClass: "quota"}
		if err := s.Finish(st.ID, &r, "quota", time.Now()); err != nil {
			t.Fatal(err)
		}
		if r.FallbackTo != "" {
			t.Fatalf("unsafe fallback: %+v", r)
		}
	}
}

func TestFallbackStopsWhenCleanupCannotBeProven(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	st, _ := s.Create("j", Request{Harness: "codex", Access: "write", Cwd: gitRepo(t)})
	baseline, err := TakeSnapshot(st.Request.Cwd)
	if err != nil {
		t.Fatal(err)
	}
	st.Baseline = &baseline
	st.Routing = &Routing{Remaining: []Route{{Harness: "kimi"}}}
	st.ProcessTrackingError = "lost process identity"
	_ = s.Save(st)
	r := Result{JobID: st.ID, Status: StatusFailed, FailureClass: "quota"}
	if err = s.Finish(st.ID, &r, "usage limit", time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.FallbackTo != "" {
		t.Fatalf("fallback after cleanup failure: %+v", r)
	}
}
func TestFallbackStatusDuringRetry(t *testing.T) {
	row := JobSummary{Status: StatusRunning, RetryAttempt: 1, RetryLimit: 2, FallbackFrom: "first"}
	if got := row.StatusText(time.Now()); !strings.Contains(got, "retrying (1/2)") || !strings.Contains(got, "fallback from first") {
		t.Fatal(got)
	}
}

func TestRouterResetAndChainOrder(t *testing.T) {
	now := time.Now()
	soon := now.Add(time.Hour)
	late := now.Add(2 * time.Hour)
	expired := now.Add(-time.Minute)
	r := &Router{Aliases: map[string]Route{"same": {Harness: "codex"}, "cheap": {Harness: "opencode", Model: "m"}}, Fallback: map[string][]string{"codex": {"@same", "@cheap", "kimi", "@cheap"}}}
	routes := r.Chain(Route{Harness: "codex"})
	if len(routes) != 3 || routes[1].Model != "m" || routes[2].Harness != "kimi" {
		t.Fatalf("%+v", routes)
	}
	q := []QuotaRoute{{Harness: "codex", ResetAt: &late, RecordedAt: now}, {Harness: "codex", Model: "m", ResetAt: &soon, RecordedAt: now}, {Harness: "kimi", ResetAt: &soon, RecordedAt: now}}
	_, _, _, err := r.Select([]Route{{Harness: "codex", Model: "m"}, {Harness: "kimi"}}, q, "write", now)
	if err == nil || !strings.Contains(err.Error(), soon.UTC().Format(time.RFC3339)) {
		t.Fatal(err)
	}
	got, _, _, err := r.Select(routes, []QuotaRoute{{Harness: "codex", ResetAt: &expired, RecordedAt: now}}, "write", now)
	if err != nil || got.Harness != "codex" {
		t.Fatalf("%+v %v", got, err)
	}
	blocked, reset := r.exhausted(Route{Harness: "codex", Model: "m"}, q, now)
	if !blocked || !reset.Equal(late) {
		t.Fatalf("%v %v", blocked, reset)
	}
}

func TestRouterEnglishWarningWithReset(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	reset := now.Add((6*24 + 18) * time.Hour)
	r := &Router{QuotaGroups: map[string]QuotaGroup{"argon": {Harness: "agy", Models: []string{"low", "high"}}}}
	for _, tc := range []struct {
		name, model string
		reset       *time.Time
		want        string
	}{
		{"model", "high", &reset, "quota exhausted for agy/high; using opencode/mini (resets in 6d 18h)"},
		{"harness", "", &reset, "quota exhausted for agy/high; using opencode/mini (resets in 6d 18h)"},
		{"group", "low", &reset, "quota exhausted for agy/high; using opencode/mini (resets in 6d 18h)"},
		{"unknown", "high", nil, "quota exhausted for agy/high; using opencode/mini"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, warnings, err := r.Select([]Route{{Harness: "agy", Model: "high"}, {Harness: "opencode", Model: "mini"}}, []QuotaRoute{{Harness: "agy", Model: tc.model, ResetAt: tc.reset, RecordedAt: now}}, "write", now)
			if err != nil || len(warnings) != 1 || warnings[0] != tc.want {
				t.Fatalf("warnings=%q err=%v", warnings, err)
			}
		})
	}
}

func TestRouteWarningsPersistAcrossTerminalPaths(t *testing.T) {
	const warning = "quota exhausted for codex; using kimi (resets in 6d 18h)"
	for _, ending := range []string{"success", "failure", "already included", "queued cancel", "orphan"} {
		t.Run(ending, func(t *testing.T) {
			store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
			st, err := store.Create("routed", Request{Harness: "codex"})
			if err != nil {
				t.Fatal(err)
			}
			st.RouteWarnings = []string{warning}
			if ending == "orphan" {
				st.CreatedAt = time.Now().Add(-time.Minute)
			}
			if err = store.Save(st); err != nil {
				t.Fatal(err)
			}
			switch ending {
			case "queued cancel":
				_, err = store.RequestCancel(st.ID)
			case "orphan":
				_, err = store.Recover(st.ID)
			default:
				r := Result{JobID: st.ID, Status: StatusSucceeded}
				if ending == "failure" {
					r.Status = StatusFailed
				}
				if ending == "already included" {
					r.Warnings = []string{warning}
				}
				err = store.Finish(st.ID, &r, "", time.Now())
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(store.ResultPath(st.ID))
			if err != nil {
				t.Fatal(err)
			}
			var result Result
			if err = json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, w := range result.Warnings {
				if w == warning {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("warning count=%d result=%s", count, data)
			}
		})
	}
}

func TestRuntimeFallbackEnglishWarningPersistsToFinalResult(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	reset := now.Add((6*24 + 18) * time.Hour)
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	st, err := store.Create("first", Request{Harness: "agy", Model: "high", Access: "write", Cwd: gitRepo(t), Brief: "task"})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := TakeSnapshot(st.Request.Cwd)
	if err != nil {
		t.Fatal(err)
	}
	st.Baseline = &baseline
	st.Routing = &Routing{Remaining: []Route{{Harness: "opencode", Model: "mini"}}}
	if err = store.Save(st); err != nil {
		t.Fatal(err)
	}
	failed := Result{JobID: st.ID, Status: StatusFailed, FailureClass: "quota", QuotaResetAt: &reset}
	if err = store.Finish(st.ID, &failed, "Individual quota reached", now); err != nil {
		t.Fatal(err)
	}
	const want = "quota exhausted for agy/high; using opencode/mini (resets in 6d 18h)"
	if !contains(failed.Warnings, want) || failed.FallbackTo == "" {
		t.Fatalf("%+v", failed)
	}
	final := Result{JobID: failed.FallbackTo, Status: StatusSucceeded}
	if err = store.Finish(final.JobID, &final, "", now); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.ResultPath(final.JobID))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(final.Warnings, want) || !strings.Contains(string(data), want) {
		t.Fatalf("final=%+v disk=%s", final, data)
	}
}
