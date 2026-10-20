package sub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/pelletier/go-toml/v2"
)

func kimiReadonlyReviewFixture(t *testing.T) (Store, Harness, string) {
	t.Helper()
	store, _, _, h, source, _ := kimiJobFixture(t, false)
	if err := os.WriteFile(filepath.Join(source, "config.toml"), []byte(kimiROConfigFixture), 0600); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load("j1")
	st.Request.Access = "read-only"
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	h.Verify, h.LocalUsageBaseline, h.LocalUsageBaselines = nil, nil, nil
	home, err := managedKimiReadOnlyHome()
	if err != nil {
		t.Fatal(err)
	}
	return store, h, home
}

func TestKimiReadonlyReviewStartupClassification(t *testing.T) {
	for _, tc := range []struct {
		name, class string
		failure     error
		cancel      bool
	}{
		{"auth", "auth", errors.New("authentication failed"), false},
		{"model", "model", cockpit.ErrModelUnavailable, false},
		{"identified-model", "model", &cockpit.StartError{ID: "created-session", Err: cockpit.ErrModelUnavailable}, false},
		{"quota", "quota", errors.New("subscription quota exhausted"), false},
		{"cancel", "canceled", context.Canceled, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, h, _ := kimiReadonlyReviewFixture(t)
			st, _ := store.Load("j1")
			if tc.class == "quota" {
				st.Routing = &Routing{Remaining: []Route{{Harness: "codex"}}}
				store.Save(st)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.Backend = func() cockpit.Backend {
				return lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
					if tc.cancel {
						cancel()
					}
					return nil, tc.failure
				}}
			}
			r, err := RunJob(ctx, store, "j1", h, WorkerConfig{})
			want := StatusFailed
			if tc.cancel {
				want = StatusCanceled
			}
			if err == nil || r.Status != want || r.FailureClass != tc.class {
				t.Fatalf("status=%s class=%s err=%v warnings=%v", r.Status, r.FailureClass, err, r.Warnings)
			}
			saved, _ := store.Load("j1")
			if saved.Error != tc.failure.Error() {
				t.Fatalf("original reason replaced: %s", saved.Error)
			}
			if tc.class == "quota" && r.FallbackTo == "" {
				t.Fatal("quota fallback was suppressed")
			}
		})
	}
}

func TestKimiReadonlyReviewViolationOverridesFailure(t *testing.T) {
	for _, status := range []Status{StatusFailed, StatusCanceled} {
		store, _, home := kimiReadonlyReviewFixture(t)
		st, _ := store.Load("j1")
		st.HarnessHome = home
		st.StartedAt = st.CreatedAt
		// A missing file baseline must not hide a positively observed tool violation.
		st.ReadOnlyBaseline = nil
		store.Save(st)
		writeKimiJobRecord(t, home, "session", `{"type":"llm.tools_snapshot","tools":[{"name":"Bash"}]}`+"\n")
		r := Result{Status: status, FailureClass: "auth", SessionID: "session"}
		if err := store.Finish("j1", &r, "original failure", time.Now()); err != nil {
			t.Fatal(err)
		}
		if r.Status != StatusFailed || r.FailureClass != "readonly_violation" {
			t.Fatalf("status=%s class=%s", r.Status, r.FailureClass)
		}
	}
}

func TestKimiReadonlyReviewConfigAllowlistAndSize(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		t.Run(fmt.Sprint(oversized), func(t *testing.T) {
			source := fakeKimiUserHome(t)
			input := kimiROConfigFixture + "\n[mcp]\nenabled=true\n[plugins]\nenabled=true\n[agents]\ndir=\"fixture\"\n[future]\nunsafe=true\n[providers.oauth]\ntype=\"kimi\"\n[providers.oauth.oauth]\nstorage=\"file\"\nkey=\"fixture-reference\"\n"
			if oversized {
				input = "#" + strings.Repeat("x", 1<<20) + "\n"
			}
			os.WriteFile(filepath.Join(source, "config.toml"), []byte(input), 0600)
			m := kimiReadOnlyHome(filepath.Join(t.TempDir(), "readonly"), source)
			err := m.Prepare(context.Background())
			if oversized {
				if err == nil || !strings.Contains(err.Error(), "1 MiB") {
					t.Fatalf("unbounded config accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer m.Cleanup()
			data, _ := os.ReadFile(filepath.Join(m.Home, "config.toml"))
			var got, original map[string]any
			if toml.Unmarshal(data, &got) != nil || toml.Unmarshal([]byte(input), &original) != nil {
				t.Fatal("invalid fixture TOML")
			}
			for _, key := range []string{"default_model", "models", "providers"} {
				if !reflect.DeepEqual(got[key], original[key]) {
					t.Errorf("%s was not preserved", key)
				}
			}
			for key := range got {
				if key != "default_model" && key != "models" && key != "providers" && key != "default_permission_mode" && key != "tools" && key != "permission" {
					t.Errorf("unexpected copied section: %s", key)
				}
			}
		})
	}
}

func TestKimiReadonlyReviewSharedPolicyAndCleanup(t *testing.T) {
	source := fakeKimiUserHome(t)
	os.WriteFile(filepath.Join(source, "config.toml"), []byte(kimiROConfigFixture), 0600)
	home := filepath.Join(t.TempDir(), "readonly")
	first, second := kimiReadOnlyHome(home, source), kimiReadOnlyHome(home, source)
	if err := first.prepare(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	defer first.Cleanup()
	before, _ := os.Lstat(filepath.Join(home, "config.toml"))
	if err := second.prepare(context.Background(), true); err != nil {
		t.Fatalf("identical policy cannot share home: %v", err)
	}
	defer second.Cleanup()
	after, _ := os.Lstat(filepath.Join(home, "config.toml"))
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Error("shared policy was rewritten")
	}
	if warnings := first.Cleanup(); len(warnings) != 0 {
		t.Fatal(warnings)
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); err != nil {
		t.Fatal("active job lost config")
	}
	if warnings := second.Cleanup(); len(warnings) != 0 {
		t.Fatal(warnings)
	}
	if _, err := os.Lstat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("secret config retained: %v", err)
	}
}

func TestKimiReadonlyReviewSingleJobRemovesConfig(t *testing.T) {
	source := fakeKimiUserHome(t)
	os.WriteFile(filepath.Join(source, "config.toml"), []byte(kimiROConfigFixture), 0600)
	m := kimiReadOnlyHome(filepath.Join(t.TempDir(), "readonly"), source)
	if err := m.prepare(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if warnings := m.Cleanup(); len(warnings) != 0 {
		t.Fatal(warnings)
	}
	if _, err := os.Lstat(filepath.Join(m.Home, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("generated config retained: %v", err)
	}
}

func TestKimiReadonlyReviewCleanupGuardsSource(t *testing.T) {
	source := fakeKimiUserHome(t)
	config := filepath.Join(source, "config.toml")
	os.WriteFile(config, []byte(kimiROConfigFixture), 0600)
	m := kimiReadOnlyHome(source, source)
	if warnings := m.Cleanup(); len(warnings) == 0 {
		t.Error("unsafe managed home accepted")
	}
	data, err := os.ReadFile(config)
	if err != nil || string(data) != kimiROConfigFixture {
		t.Fatal("cleanup changed source config")
	}
}

func TestKimiReadonlyReviewRuntimeOverrides(t *testing.T) {
	store, h, _ := kimiReadonlyReviewFixture(t)
	for key, value := range map[string]string{"KIMI_CODE_DANGEROUS_COMMAND_GUARD": "false", "KIMI_CODE_EXPERIMENTAL_FLAG": "1", "KIMI_CODE_EXPERIMENTAL_TOWER": "1", "KIMI_CODE_TRUST_WORKSPACE": "1", "KIMI_CODE_BACKGROUND_KEEP_ALIVE_ON_EXIT": "true", "KIMI_API_KEY": "fixture-key", "KIMI_MODEL_NAME": "fixture-model"} {
		t.Setenv(key, value)
	}
	h.Backend = func() cockpit.Backend {
		return lifecycleBackend{start: func(_ context.Context, opts cockpit.StartOpts) (cockpit.Session, error) {
			env := append(os.Environ(), opts.Env...)
			for key, want := range map[string]string{"KIMI_CODE_DANGEROUS_COMMAND_GUARD": "true", "KIMI_CODE_EXPERIMENTAL_FLAG": "0", "KIMI_CODE_EXPERIMENTAL_TOWER": "0", "KIMI_CODE_TRUST_WORKSPACE": "0", "KIMI_CODE_BACKGROUND_KEEP_ALIVE_ON_EXIT": "false", "KIMI_API_KEY": "fixture-key", "KIMI_MODEL_NAME": "fixture-model"} {
				if childEnv(env, key) != want {
					t.Errorf("runtime override %s was not isolated/preserved", key)
				}
			}
			return nil, cockpit.ErrModelUnavailable
		}}
	}
	RunJob(context.Background(), store, "j1", h, WorkerConfig{})
}

func TestKimiReadonlyReviewConcurrentJobs(t *testing.T) {
	store, h, home := kimiReadonlyReviewFixture(t)
	st, _ := store.Load("j1")
	if _, err := store.Create("j2", st.Request); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 2)
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	defer finish()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.Backend = func() cockpit.Backend {
		return lifecycleBackend{start: func(_ context.Context, opts cockpit.StartOpts) (cockpit.Session, error) {
			id := childEnv(opts.Env, "PACTIFY_JOB")
			s := &fakeSession{events: make(chan cockpit.Event, 8)}
			started <- id
			return &retrySession{fakeSession: s, id: id, prompt: func(context.Context, cockpit.UserMessage) error {
				go func() {
					select {
					case <-release:
					case <-ctx.Done():
					}
					writeKimiJobRecord(t, home, id, `{"type":"llm.tools_snapshot","tools":[{"name":"Read"}]}`+"\n")
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Text: "done", Final: true}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}()
				return nil
			}}, nil
		}}
	}
	results := make(chan error, 2)
	var workers sync.WaitGroup
	defer func() { finish(); cancel(); workers.Wait() }()
	for _, id := range []string{"j1", "j2"} {
		workers.Add(1)
		go func(id string) {
			defer workers.Done()
			r, err := RunJob(ctx, store, id, h, WorkerConfig{})
			if err == nil && r.Status != StatusSucceeded {
				err = fmt.Errorf("%s status=%s warnings=%v", id, r.Status, r.Warnings)
			}
			results <- err
		}(id)
	}
	for n := 0; n < 2; n++ {
		select {
		case <-started:
		case err := <-results:
			t.Fatalf("job finished before both started: %v", err)
		case <-ctx.Done():
			t.Fatal("concurrent jobs did not start")
		}
	}
	// Release explicitly while retaining a safe deferred close for early failures.
	finish()
	for n := 0; n < 2; n++ {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Error("last job retained generated config")
	}
}

func TestKimiReadonlyReviewRejectsForeignHome(t *testing.T) {
	store, _, _ := kimiReadonlyReviewFixture(t)
	st, _ := store.Load("j1")
	st.HarnessHome = t.TempDir()
	store.Save(st)
	writeKimiJobRecord(t, st.HarnessHome, "session", `{"type":"llm.tools_snapshot","tools":[{"name":"Read"}]}`+"\n")
	if err := store.CheckKimiReadonly("j1", Result{SessionID: "session"}, nil); err == nil {
		t.Fatal("foreign evidence home accepted")
	}
}

func TestKimiReadonlyReviewProbeEnvironment(t *testing.T) {
	store, _, home := kimiReadonlyReviewFixture(t)
	root := os.Getenv("PACTIFY_HOME")
	st, _ := store.Load("j1")
	st.HarnessHome = home
	store.Save(st)
	writeKimiJobRecord(t, home, "session", `{"type":"llm.tools_snapshot","tools":[{"name":"Read"}]}`+"\n")
	// The live E2E parent and probe have disjoint managed homes.
	t.Setenv("PACTIFY_HOME", t.TempDir())
	if err := store.CheckKimiReadonly("j1", Result{SessionID: "session"}, []string{"PACTIFY_HOME=" + root}); err != nil {
		t.Fatalf("probe environment ignored: %v", err)
	}
}
