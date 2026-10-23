package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestFailureRegression(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ h, msg, class string }{
		{"agy", "Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 166h58m25s.", "quota"},
		{"agy", "API error (attempt 1): UNAVAILABLE (code 503): The service is currently unavailable.", "transient"},
		{"agy", `API error (attempt 1): request failed: Post "https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse": EOF`, "transient"},
		{"agy", `API error (attempt 1): request failed: Post "https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse": write tcp 192.0.2.10:55862->198.51.100.4:443: use of closed network connection`, "transient"},
		{"agy", "The stream was interrupted. Please continue the task you were working on.", "transient"},
		{"opencode", "acp: rpc error -32603: Internal error: Insufficient Balance (request_id: …)", "quota"},
		{"opencode", "acp configure \"opencode\": requested model is not offered by this agent: …", "model"},
		{"agy", "Idle timeout", "idle_timeout"},
		{"codex", "Task canceled", "canceled"},
		{"agy", "agy models: signal: killed", "other"},
		{"agy", `invalid model selection (--model "e2e-nonexistent-model-718" --effort ""): model e2e-nonexistent-model-718 is not recognized as a known model or custom model in settings`, "model"},
		{"codex", "job worker is absent or its process identity no longer matches", "other"},
		{"agy", "Read-only access was not offered for agy in the first release", "other"},
		{"codex", "git rev-parse HEAD: fatal: not a git repository", "other"},
		{"agy", "Authentication expired. Please log back in.", "auth"},
		{"agy", "agy token unavailable; run agy login first: missing", "auth"},
		{"kimi", "acp: rpc error -32000: Authentication required", "auth"},
		{"kimi", "internal: The provided authorization grant is invalid", "auth"},
	} {
		t.Run(tc.h+"/"+tc.msg, func(t *testing.T) {
			s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
			_, err := s.Create("job", Request{Harness: tc.h, Model: "test-model"})
			if err != nil {
				t.Fatal(err)
			}
			r := Result{Status: StatusFailed, Harness: tc.h, JobID: "job", ModelRequested: "test-model"}
			if err := s.Finish("job", &r, tc.msg, now); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(s.ResultPath("job"))
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["failureClass"] != tc.class {
				t.Fatalf("failureClass=%v want %s", fields["failureClass"], tc.class)
			}
			st, _ := s.Load("job")
			if st.Error != tc.msg {
				t.Fatalf("error changed: %q", st.Error)
			}
			summary, _ := json.Marshal(s.Summarize(st, nil, now))
			if !strings.Contains(string(summary), `"failureClass":"`+tc.class+`"`) {
				t.Fatalf("summary=%s", summary)
			}
			if tc.class == "quota" && tc.h == "agy" {
				want := now.Add(166*time.Hour + 58*time.Minute + 25*time.Second).Format(time.RFC3339)
				if fields["quotaResetAt"] != want {
					t.Fatalf("reset=%v want %s", fields["quotaResetAt"], want)
				}
			}
		})
	}
}

func TestSuccessfulJobOmitsFailure(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	s.Create("ok", Request{Harness: "agy"})
	r := Result{Status: StatusSucceeded}
	if err := s.Finish("ok", &r, "Idle timeout", time.Now()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.ResultPath("ok"))
	if strings.Contains(string(data), "failureClass") || strings.Contains(string(data), "quotaResetAt") {
		t.Fatal(string(data))
	}
}

func TestWorkerTruncationWarningDoesNotBecomeError(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			store, session, backend := workerFixture(t, "write")
			if err := store.WithState(context.Background(), "j1", func(st *State) error { st.Request.Harness = "opencode"; return store.Save(*st) }); err != nil {
				t.Fatal(err)
			}
			session.script = func(s *fakeSession) {
				s.events <- cockpit.Event{Kind: cockpit.EventSystem, State: "warning", Text: "Opencode truncated tool output"}
				if fail {
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "Insufficient Balance"}
				} else {
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Text: "done", Final: true}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
			}
			r, err := RunJob(context.Background(), store, "j1", newFakeHarness(backend), WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			st, err := store.Load("j1")
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if fail {
				want = "Insufficient Balance"
			}
			if st.Error != want {
				t.Errorf("error=%q want %q", st.Error, want)
			}
			if fail && r.FailureClass != "quota" {
				t.Errorf("failure class=%s", r.FailureClass)
			}
			if !strings.Contains(strings.Join(r.Warnings, ";"), "Opencode truncated tool output") {
				t.Error("truncation warning lost")
			}
		})
	}
}

func TestWorkerStructuredQuota(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	store.WithState(context.Background(), "j1", func(st *State) error { st.Request.Harness = "codex"; return store.Save(*st) })
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventSystem, State: "rate_limits", Raw: json.RawMessage(`{"rateLimits":{"secondary":{"usedPercent":100,"resetsAt":2000000000}}}`)}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "backend refused"}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(r)
	if !strings.Contains(string(data), `"failureClass":"quota"`) {
		t.Fatal(string(data))
	}
}

func TestQuotaMemoryConcurrentAndExpiry(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	now := time.Now().UTC()
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a' + i))
			if _, err := s.Create(id, Request{Harness: "agy", Model: id}); err != nil {
				t.Error(err)
				return
			}
			r := Result{Status: StatusFailed, Harness: "agy", ModelRequested: id}
			if err := s.Finish(id, &r, "Individual quota reached. Resets in 1h.", now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	path := filepath.Join(filepath.Dir(s.Root), "quota.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 12 {
		t.Fatalf("lost writes: %s", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}

func TestQuotaExpiryAndFallbackRoute(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	record := func(id, h, model, msg string, at time.Time) {
		t.Helper()
		if _, err := s.Create(id, Request{Harness: h, Model: model}); err != nil {
			t.Fatal(err)
		}
		r := Result{Status: StatusFailed}
		if err := s.Finish(id, &r, msg, at); err != nil {
			t.Fatal(err)
		}
	}
	record("old", "agy", "old", "Individual quota reached. Resets in 1h.", now)
	record("unknown", "opencode", "", "Insufficient Balance", now)
	routes, err := s.ExhaustedRoutes(now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Harness != "opencode" || routes[0].Model != "" || routes[0].ResetAt != nil {
		t.Fatalf("routes=%+v", routes)
	}
	routes, err = s.ExhaustedRoutes(now.Add(24 * time.Hour))
	if err != nil || len(routes) != 0 {
		t.Fatalf("expiry=%+v err=%v", routes, err)
	}
	record("fresh", "agy", "new", "Individual quota reached. Resets in 2h.", now.Add(25*time.Hour))
	data, _ := os.ReadFile(s.quotaPath())
	var entries map[string]QuotaRoute
	json.Unmarshal(data, &entries)
	if len(entries) != 1 || entries["agy/new"].JobID != "fresh" {
		t.Fatalf("not pruned: %s", data)
	}
	record("older-write", "agy", "new", "Individual quota reached. Resets in 1h.", now.Add(24*time.Hour))
	routes, _ = s.ExhaustedRoutes(now.Add(25 * time.Hour))
	if len(routes) != 1 || routes[0].JobID != "fresh" {
		t.Fatalf("older write replaced newer evidence: %+v", routes)
	}
}

func TestFailureCancelOverridesQuotaAndSuccessOmitsMetadata(t *testing.T) {
	for _, status := range []Status{StatusCanceled, StatusSucceeded} {
		t.Run(string(status), func(t *testing.T) {
			s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
			s.Create("job", Request{Harness: "agy"})
			r := Result{Status: status, FailureClass: "quota", QuotaResetAt: new(time.Now().Add(time.Hour))}
			if err := s.Finish("job", &r, "Individual quota reached", time.Now()); err != nil {
				t.Fatal(err)
			}
			want := "canceled"
			if status == StatusSucceeded {
				want = ""
			}
			if r.FailureClass != want || r.QuotaResetAt != nil {
				t.Fatalf("result=%+v", r)
			}
			routes, err := s.ExhaustedRoutes(time.Now())
			if err != nil || len(routes) != 0 {
				t.Fatalf("unexpected quota=%+v %v", routes, err)
			}
		})
	}
}

func TestFailureRenderingAndLegacy(t *testing.T) {
	now := time.Now()
	reset := now.Add(167 * time.Hour)
	r := Result{Harness: "agy", Status: StatusFailed, FailureClass: "quota", QuotaResetAt: &reset, Warnings: []string{"original error"}}
	if got := r.Markdown(); !strings.Contains(got, "failed (quota, resets in 6d 22h)") || !strings.Contains(got, "original error") {
		t.Fatal(got)
	}
	if got := FailureStatus(StatusFailed, "", nil, now); got != "failed" {
		t.Fatal(got)
	}
	if got := FailureStatus(StatusFailed, "quota", &reset, now); got != "failed (quota, resets in 6d 23h)" {
		t.Fatal(got)
	}
}

func TestQuotaCorruptionIsReportedWithoutChangingError(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	s.Create("job", Request{Harness: "agy"})
	os.WriteFile(s.quotaPath(), []byte("broken"), 0600)
	if _, err := s.ExhaustedRoutes(time.Now()); err == nil {
		t.Fatal("corruption hidden")
	}
	r := Result{Status: StatusFailed}
	if err := s.Finish("job", &r, "Individual quota reached", time.Now()); err != nil {
		t.Fatal(err)
	}
	if !contains(r.Warnings, "Failed to record quota memory") {
		t.Fatal(r.Warnings)
	}
	st, _ := s.Load("job")
	if st.Error != "Individual quota reached" {
		t.Fatal(st.Error)
	}
	data, _ := os.ReadFile(s.quotaPath())
	if string(data) != "broken" {
		t.Fatal("overwrote corrupt quota memory")
	}
}

func TestQuotaOnlyCarriesResetAndUsesActualModel(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	s.Create("quota", Request{Harness: "agy", Model: "requested"})
	r := Result{Status: StatusFailed, ModelActual: "actual"}
	if err := s.Finish("quota", &r, "Individual quota reached. Resets in 1h.", time.Now()); err != nil {
		t.Fatal(err)
	}
	routes, err := s.ExhaustedRoutes(time.Now())
	if err != nil || len(routes) != 1 || routes[0].Model != "actual" {
		t.Fatalf("routes=%+v %v", routes, err)
	}
	s.Create("other", Request{Harness: "agy"})
	other := Result{Status: StatusFailed, FailureClass: "model", QuotaResetAt: new(time.Now().Add(time.Hour))}
	if err := s.Finish("other", &other, "arbitrary failure", time.Now()); err != nil {
		t.Fatal(err)
	}
	if other.QuotaResetAt != nil {
		t.Fatal("non-quota retained reset timestamp")
	}
}

func TestWorkerTypedStartupAuth(t *testing.T) {
	store, _, b := workerFixture(t, "write")
	store.WithState(context.Background(), "j1", func(st *State) error { st.Request.Harness = "kimi"; return store.Save(*st) })
	b.startErr = fmt.Errorf("startup: %w", testAuthRPCError{})
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err == nil || r.FailureClass != "auth" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

type testAuthRPCError struct{}

func (testAuthRPCError) Error() string { return "opaque error" }
func (testAuthRPCError) RPCCode() int  { return -32000 }

func TestWorkerQuotaMessagePreservesLegacyReason(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	store.WithState(context.Background(), "j1", func(st *State) error { st.Request.Harness = "codex"; return store.Save(*st) })
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Raw: json.RawMessage(`{"turn":{"error":{"message":"original provider quota failure","codexErrorInfo":"usageLimitExceeded"}}}`)}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil || r.FailureClass != "quota" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	st, _ := store.Load("j1")
	if st.Error != "Turn failed" {
		t.Fatal(st.Error)
	}
	routes, err := store.ExhaustedRoutes(time.Now())
	if err != nil || len(routes) != 1 || routes[0].Message != "original provider quota failure" {
		t.Fatalf("quota=%+v err=%v", routes, err)
	}
}

func TestWorkerIdleAndCancelClassification(t *testing.T) {
	for _, cancelJob := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelJob), func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			s.script = func(*fakeSession) {}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := WorkerConfig{IdleTimeout: 20 * time.Millisecond}
			if cancelJob {
				cfg.IdleTimeout = time.Minute
				timer := time.AfterFunc(40*time.Millisecond, cancel)
				defer timer.Stop()
			}
			r, err := RunJob(ctx, store, "j1", newFakeHarness(b), cfg)
			if err != nil {
				t.Fatal(err)
			}
			want := "idle_timeout"
			if cancelJob {
				want = "canceled"
			}
			if r.FailureClass != want {
				t.Fatalf("got=%+v want %s", r, want)
			}
		})
	}
}

func TestQueuedCancelAndOrphanClassification(t *testing.T) {
	for _, cancelJob := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelJob), func(t *testing.T) {
			store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
			st, err := store.Create("j", Request{Harness: "agy"})
			if err != nil {
				t.Fatal(err)
			}
			if cancelJob {
				st, err = store.RequestCancel("j")
			} else {
				st.CreatedAt = time.Now().Add(-time.Minute)
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
				st, err = store.Recover("j")
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(store.ResultPath("j"))
			if err != nil {
				t.Fatal(err)
			}
			var r Result
			if err := json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			want := "other"
			if cancelJob {
				want = "canceled"
			}
			if r.FailureClass != want {
				t.Fatalf("result=%+v state=%+v", r, st)
			}
		})
	}
}

func TestWorkerRetainsStructuredErrorBeforeGenericTerminal(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	if err := store.WithState(context.Background(), "j1", func(st *State) error { st.Request.Harness = "codex"; return store.Save(*st) }); err != nil {
		t.Fatal(err)
	}
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventError, Err: "opaque provider failure", Raw: json.RawMessage(`{"error":{"message":"opaque provider failure","codexErrorInfo":"unauthorized"}}`)}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Raw: json.RawMessage(`{"turn":{"status":"failed","error":{"message":"opaque provider failure"}}}`)}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil || r.FailureClass != "auth" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestWorkerPromptFailureUsesQuotaSnapshot(t *testing.T) {
	store, s, _ := workerFixture(t, "write")
	if err := store.WithState(context.Background(), "j1", func(st *State) error { st.Request.Harness = "codex"; return store.Save(*st) }); err != nil {
		t.Fatal(err)
	}
	s.events = make(chan cockpit.Event)
	b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
		return &lifecycleSession{fakeSession: s, interrupt: func() error { return s.Interrupt(context.Background()) }, prompt: func(context.Context) error {
			s.events <- cockpit.Event{Kind: cockpit.EventSystem, State: "rate_limits", Raw: json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":100}}}`)}
			return fmt.Errorf("opaque prompt failure")
		}}, nil
	}}
	r, err := RunJob(context.Background(), store, "j1", Harness{Backend: func() cockpit.Backend { return b }}, WorkerConfig{})
	if err != nil || r.FailureClass != "quota" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

// codex can start a thread with an unknown model, then reject the
// account/model combination at turn time using a provider error message.
func TestCodexUnsupportedSubscriptionModelFailure(t *testing.T) {
	message := `{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The 'e2e-nonexistent-model-718' model is not supported when using Codex with a ChatGPT account."}}`
	store, sess, backend := workerFixture(t, "write")
	st, err := store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	st.Request.Harness = "codex"
	if err = store.Save(st); err != nil {
		t.Fatal(err)
	}
	h, _ := Lookup("codex")
	h.Version = nil
	h.Conceal = nil
	h.Backend = func() cockpit.Backend { return backend }
	sess.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventError, Err: message}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Raw: json.RawMessage(`{"turn":{"status":"failed"}}`)}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusFailed || r.FailureClass != "model" || len(r.Retries) != 0 {
		t.Fatalf("status=%s class=%s retries=%v", r.Status, r.FailureClass, r.Retries)
	}
	st, err = store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.Error, "model is not supported") {
		t.Fatalf("provider error replaced with generic terminal message: %q", st.Error)
	}
}

func TestManagedMissingLoginFailureIsAuth(t *testing.T) {
	// The home path is quoted in the error; a 429 in it once made this
	// transient about 1% of the time via random TempDir names.
	home := func(t *testing.T) string {
		dir := filepath.Join(t.TempDir(), "home-1429")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	for _, name := range []string{"opencode", "kimi"} {
		t.Run(name, func(t *testing.T) {
			store, _, backend := workerFixture(t, "write")
			t.Setenv("XDG_DATA_HOME", home(t))
			t.Setenv("XDG_CONFIG_HOME", home(t))
			t.Setenv("KIMI_CODE_HOME", home(t))
			st, err := store.Load("j1")
			if err != nil {
				t.Fatal(err)
			}
			st.Request.Harness = name
			st.ConfigMode = "minimal"
			if err = store.Save(st); err != nil {
				t.Fatal(err)
			}
			h, _ := Lookup(name)
			h.Version = nil
			h.Backend = func() cockpit.Backend { return backend }
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err == nil {
				t.Fatal("expected a missing-login startup error")
			}
			if r.Status != StatusFailed || r.FailureClass != "auth" {
				t.Fatalf("missing login: status=%s class=%s warnings=%v", r.Status, r.FailureClass, r.Warnings)
			}
			st, err = store.Load("j1")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(st.Error, "login first") {
				t.Fatalf("missing actionable login hint: %q", st.Error)
			}
		})
	}
}
