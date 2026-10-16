package sub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestAgyReviewSnapshotsSeparateLedger(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	q := cockpit.QuotaObservation{ObservedAt: time.Now().UTC(), Buckets: []cockpit.QuotaBucket{{ID: "gemini-weekly", RemainingFraction: .5, ResetAt: time.Now().Add(time.Hour)}}}
	if e := store.rememberQuotaSnapshot("j1", "agy", q); e != nil {
		t.Fatal(e)
	}
	var legacy map[string]QuotaRoute
	if b, e := os.ReadFile(store.quotaPath()); e == nil {
		if e = json.Unmarshal(b, &legacy); e != nil {
			t.Fatal(e)
		}
		if len(legacy) > 0 {
			t.Fatal("snapshot poisoned legacy quota ledger", string(b))
		}
	}
	if b, e := os.ReadFile(filepath.Join(filepath.Dir(store.Root), "quota-snapshots.json")); e != nil || !strings.Contains(string(b), "gemini-weekly") {
		t.Fatalf("separate snapshot missing: %s %v", b, e)
	}
}

type reviewQuotaBackend struct {
	cockpit.Backend
	calls           atomic.Int32
	closingDeadline func(context.Context)
	t               *testing.T
	ended, release  chan struct{}
}

func (b *reviewQuotaBackend) Quota(ctx context.Context, _ cockpit.StartOpts) (cockpit.QuotaObservation, error) {
	if b.calls.Add(1) == 1 {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5100*time.Millisecond {
			b.t.Error("opening snapshot lacks 5s deadline")
		}
		return cockpit.QuotaObservation{}, errors.New("probe failure")
	}
	if b.closingDeadline != nil {
		b.closingDeadline(ctx)
	}
	close(b.ended)
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return cockpit.QuotaObservation{}, errors.New("closing probe failure")
}
func TestAgyReviewQuotaWarningsAndPublication(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failed], func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			st, _ := store.Load("j1")
			st.Request.Harness = "agy"
			_ = store.Save(st)
			quota := &reviewQuotaBackend{Backend: b, t: t, ended: make(chan struct{}), release: make(chan struct{})}
			h := newFakeHarness(b)
			h.Name = "agy"
			h.Backend = func() cockpit.Backend { return quota }
			s.script = func(s *fakeSession) {
				state := cockpit.StateTurnCompleted
				err := ""
				if failed {
					state = cockpit.StateTurnFailed
					err = "agy empty_answer: actual failure"
				}
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: state, Err: err}
			}
			done := make(chan Result, 1)
			go func() { r, _ := RunJob(context.Background(), store, "j1", h, WorkerConfig{}); done <- r }()
			select {
			case <-quota.ended:
			case <-time.After(3 * time.Second):
				t.Fatal("no closing snapshot")
			}
			current, _ := store.Load("j1")
			if !Terminal(current.Status) {
				t.Error("closing snapshot delayed terminal publication")
			}
			close(quota.release)
			r := <-done
			current, _ = store.Load("j1")
			if failed && !strings.Contains(current.Error, "actual failure") {
				t.Errorf("real failure hidden: %+v", current)
			}
			if !failed && current.Error != "" {
				t.Errorf("successful job has quota error: %s", current.Error)
			}
			if !contains(r.Warnings, "Quota snapshot unavailable") {
				t.Fatal(r.Warnings)
			}
		})
	}
}
func TestAgyReviewUserConfigWarning(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	st, _ := store.Load("j1")
	st.Request.Harness = "agy"
	st.Request.WithUserConfig = true
	st.ConfigMode = "user"
	_ = store.Save(st)
	h := newFakeHarness(b)
	h.Name = "agy"
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, e := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if e != nil || !contains(r.Warnings, "managed hooks are not installed with --with-user-config") {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestAgyReviewDoctorNeverSelectsNonFlash(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h, _ := Lookup("agy")
	h.Home = nil
	h.Version = func(context.Context) (string, error) { return "1.4.0", nil }
	h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) {
		return []string{"gemini-4-argon-low", "gemini-pro-low"}, nil
	}
	called := false
	reports, e := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "fixture", nil }, run: func(context.Context, Store, string, Harness, WorkerConfig) (Result, error) {
		called = true
		return Result{}, nil
	}})
	if e != nil || called || len(reports) != 1 || reports[0].Passed() {
		t.Fatalf("called=%t reports=%+v err=%v", called, reports, e)
	}
}

func TestAgyReviewDoctorRequiresHookEvidence(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h, _ := Lookup("agy")
	h.Home = nil
	h.Models = nil
	h.Version = func(context.Context) (string, error) { return "1.4.0", nil }
	hookProbe := false
	reports, e := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{model: cockpit.AgyDefaultModel, lookPath: func(string) (string, error) { return "fake", nil }, run: func(_ context.Context, s Store, id string, _ Harness, cfg WorkerConfig) (Result, error) {
		st, _ := s.Load(id)
		if id == "write" {
			_ = os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
			_ = os.WriteFile(filepath.Join(st.Request.Cwd, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
		} else if id == "read-only" {
			writeAgyDeniedDoctorLog(t, s, id)
		} else {
			hookProbe = true
			if st.Request.Access != "write" {
				t.Error("hook probe must bypass native plan restrictions")
			}
		}
		return Result{Status: StatusSucceeded, CLIVersion: "1.4.0", ModelActual: cockpit.AgyDefaultModel, ModelVerified: true, InputTokens: 1, ToolCalls: 1, FinalText: "Denied"}, nil
	}})
	found := false
	for _, check := range reports[0].Checks {
		if check.Name == "Read-only hook" {
			found = true
			if check.Status == "PASS" {
				t.Error("passed without hook-specific evidence")
			}
		}
	}
	if e != nil || !hookProbe || !found {
		t.Fatalf("no independent hook probe: %+v err=%v", reports, e)
	}
}

// Doctor's human-readable IDs repeat across disposable stores. Their process
// ownership marker must not collide with a regular job named "write".
func TestAgyReviewDoctorProcessOwnership(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h, _ := Lookup("agy")
	h.Home = nil
	h.Models = nil
	h.Version = func(context.Context) (string, error) { return "1.4.0", nil }
	outsider := jobMarkerOrphan(t, "write")
	markers := map[string]bool{}
	for i := 0; i < 2; i++ {
		_, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{model: cockpit.AgyDefaultModel, lookPath: func(string) (string, error) { return "fake", nil }, run: func(ctx context.Context, s Store, id string, h Harness, cfg WorkerConfig) (Result, error) {
			b := &fakeBackend{sess: &fakeSession{events: make(chan cockpit.Event, 128), approvals: make(chan cockpit.ApprovalRequest, 4)}}
			b.sess.script = func(s *fakeSession) {
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			h.Backend = func() cockpit.Backend { return b }
			h.Version = nil
			_, e := RunJob(ctx, s, id, h, cfg)
			if id == "write" {
				marker := childEnv(b.gotOpts.Env, "PACTIFY_JOB")
				if marker == "write" || markers[marker] {
					t.Errorf("doctor process marker collision: %q", marker)
				}
				markers[marker] = true
			}
			return Result{}, e
		}})
		if err != nil {
			t.Fatal(err)
		}
		if alive, e := outsider.Alive(); e != nil || !alive {
			t.Fatalf("doctor cleanup interrupted a different store: alive=%t err=%v", alive, e)
		}
	}
}

func TestAgyReviewClosingQuotaHasTimeAfterPublication(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	quota := &reviewQuotaBackend{Backend: b, t: t, ended: make(chan struct{}), release: make(chan struct{})}
	h := newFakeHarness(b)
	h.Backend = func() cockpit.Backend { return quota }
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	// The source validates the opening deadline in its first call. Closing gets a
	// separate deadline after publication, enough for the observed 7s /usage call.
	quota.closingDeadline = func(ctx context.Context) {
		if d, ok := ctx.Deadline(); !ok || time.Until(d) < 8*time.Second {
			t.Error("closing snapshot still limited to the opening 5s budget")
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = RunJob(context.Background(), store, "j1", h, WorkerConfig{}) }()
	<-quota.ended
	close(quota.release)
	<-done
}

type reviewQuotaSource struct {
	cockpit.Backend
	quota func(context.Context) (cockpit.QuotaObservation, error)
}

func (b reviewQuotaSource) Quota(ctx context.Context, _ cockpit.StartOpts) (cockpit.QuotaObservation, error) {
	return b.quota(ctx)
}
func TestAgyReviewQuotaResetPublishedBeforeClosing(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	st, _ := store.Load("j1")
	st.Request.Harness = "agy"
	st.Request.Model = cockpit.AgyDefaultModel
	_ = store.Save(st)
	n := 0
	closing := make(chan struct{})
	release := make(chan struct{})
	reset := time.Now().Add(time.Hour).UTC()
	source := reviewQuotaSource{Backend: b, quota: func(ctx context.Context) (cockpit.QuotaObservation, error) {
		n++
		if n == 2 {
			close(closing)
			<-release
		}
		return cockpit.QuotaObservation{ObservedAt: time.Now(), Buckets: []cockpit.QuotaBucket{{ID: "gemini-weekly", RemainingFraction: 0, ResetAt: reset}}}, nil
	}}
	h := newFakeHarness(b)
	h.Backend = func() cockpit.Backend { return source }
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "Individual quota reached"}
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = RunJob(context.Background(), store, "j1", h, WorkerConfig{}) }()
	<-closing
	routes, e := store.ExhaustedRoutes(time.Now())
	if e != nil || len(routes) != 1 || routes[0].ResetAt == nil {
		t.Errorf("opening reset missing from published quota failure: %+v %v", routes, e)
	}
	close(release)
	<-done
}

func TestAgyReviewHostGitReadsDisableHelpers(t *testing.T) {
	repo := gitRepo(t)
	marker := filepath.Join(t.TempDir(), "executed")
	helper := filepath.Join(t.TempDir(), "fsmonitor")
	if e := os.WriteFile(helper, []byte("#!/bin/sh\nprintf unsafe >> '"+marker+"'\nprintf '\\0'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := runGit(repo, "config", "core.fsmonitor", helper); e != nil {
		t.Fatal(e)
	}
	if _, e := captureReadOnly(repo); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("host read-only baseline executed repo-local fsmonitor", e)
	}
	snap, e := TakeSnapshot(repo)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DiffSince(repo, snap); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("host snapshot/diff executed repo-local fsmonitor", e)
	}
}

func TestAgyReviewWorkspaceRefusedBeforeQuota(t *testing.T) {
	store, _, b := workerFixture(t, "read-only")
	st, _ := store.Load("j1")
	st.Request.Harness = "agy"
	if e := os.Mkdir(filepath.Join(st.Request.Cwd, ".agent"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(st.Request.Cwd, ".agent", "hooks.json"), []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	_ = store.Save(st)
	called := false
	source := reviewQuotaSource{Backend: b, quota: func(context.Context) (cockpit.QuotaObservation, error) {
		called = true
		return cockpit.QuotaObservation{}, errors.New("unexpected probe")
	}}
	h := newFakeHarness(b)
	h.Backend = func() cockpit.Backend { return source }
	b.startErr = errors.New("stop after quota")
	_, e := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if called || e == nil || !strings.Contains(e.Error(), "workspace hooks") {
		t.Fatalf("read-only preflight was too late: called=%t err=%v", called, e)
	}
}

func TestAgyReviewCostUsesObservationTimeAfterPublication(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	now := time.Now().UTC()
	// Both probes finish after their jobs publish. Their observation order differs
	// from the finish timestamps used to select legacy rate-limit summaries.
	for i, id := range []string{"a", "b"} {
		st, e := store.Create(id, Request{Harness: "agy", Access: "write", Cwd: t.TempDir()})
		if e != nil {
			t.Fatal(e)
		}
		st.Status = StatusSucceeded
		st.FinishedAt = now.Add(time.Duration(-30+i*5) * time.Second)
		st.CreatedAt = now.Add(-time.Hour)
		if e = store.Save(st); e != nil {
			t.Fatal(e)
		}
		q := cockpit.QuotaObservation{ObservedAt: now.Add(time.Duration(-20+i*5) * time.Second), Buckets: []cockpit.QuotaBucket{{ID: "gemini-weekly", RemainingFraction: .8 - float64(i)/10, ResetAt: now.Add(time.Hour)}}}
		r := Result{JobID: id, Harness: "agy", Status: StatusSucceeded, RateLimit: q.Summary(), QuotaSnapshots: []cockpit.QuotaObservation{q}}
		if e = atomicJSON(store.ResultPath(id), r); e != nil {
			t.Fatal(e)
		}
	}
	report, e := store.CostReport(time.Time{}, "harness", now)
	if e != nil || len(report.QuotaSnapshots) != 1 || report.QuotaSnapshots[0].JobID != "b" {
		t.Fatalf("stale quota snapshot selected: %+v err=%v", report.QuotaSnapshots, e)
	}
}
