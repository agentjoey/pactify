package sub

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewReadonlyFilesystem(t *testing.T) {
	for _, scenario := range []string{"ignored-delete", "ignored-modify", "untracked-delete"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			testenv.InstallOpencodeServe(t)
			t.Setenv("COCKPIT_OC_SCENARIO", scenario)
			repo := t.TempDir()
			if err := initDoctorRepo(repo, false); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(scenario, "ignored") {
				os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("victim\n"), 0600)
			}
			os.WriteFile(filepath.Join(repo, "victim"), []byte("original"), 0600)
			store := Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}
			st, err := store.Create("job", Request{Harness: "opencode", Model: "opencode/test-free", Access: "read-only", Cwd: repo, Brief: "test"})
			if err != nil {
				t.Fatal(err)
			}
			st.ConfigMode = "minimal"
			store.Save(st)
			h, _ := Lookup("opencode")
			h.Version = nil
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r, err := RunJob(ctx, store, "job", h, WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != StatusFailed || r.FailureClass != "readonly_violation" {
				t.Errorf("status=%s class=%s warnings=%v", r.Status, r.FailureClass, r.Warnings)
			}
		})
	}
}
func TestReviewReadonlyLostWorker(t *testing.T) {
	store, _, _ := workerFixture(t, "read-only")
	st, _ := store.Load("j1")
	st.Request.Harness = "opencode"
	store.Save(st)
	if _, err := store.claim(context.Background(), st.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.WithState(context.Background(), st.ID, func(current *State) error { return store.fallbackLocked(current, StatusFailed, "worker absent") }); err != nil {
		t.Fatal(err)
	}
	var r Result
	b, _ := os.ReadFile(store.ResultPath(st.ID))
	json.Unmarshal(b, &r)
	if !strings.Contains(strings.Join(r.Warnings, " "), "Read-only verification unavailable") {
		t.Fatalf("warnings=%v", r.Warnings)
	}
}

// AJ-734: opencode read-only removes the shell tool, so doctor cannot ask the
// model to attempt a denied write. PASS rests on the session's own policy
// (read back after the job) plus an absent write target; one probe only.
func TestReviewDoctorOpencodeReadonlyPolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		policyErr error
		write     bool
		want      string
	}{
		{"policy denies", nil, false, "PASS"},
		{"policy allows", errors.New(`opencode readonly bash permission is "ask"; expected deny`), false, "FAIL"},
		{"written", nil, true, "FAIL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			h, _ := Lookup("opencode")
			h.Home = nil
			h.Models = nil
			h.Version = func(context.Context) (string, error) { return "test", nil }
			probes, checked := 0, 0
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "fake", nil }, run: func(_ context.Context, store Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
				st, _ := store.Load(id)
				if id == "write" {
					os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
					os.WriteFile(filepath.Join(st.Request.Cwd, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
				} else {
					probes++
					if strings.Contains(st.Request.Brief, "echo x > probe.txt") {
						t.Error("doctor asks for a shell write the read-only session cannot attempt")
					}
					if tc.write {
						os.WriteFile(filepath.Join(st.Request.Cwd, "probe.txt"), []byte("x"), 0600)
					}
				}
				return Result{Status: StatusSucceeded, CLIVersion: "test", ModelActual: st.Request.Model, ModelVerified: true, InputTokens: 1, FinalText: "done"}, nil
			}, readonlyCheck: func(_ context.Context, _ Store, id string, _ Result) error {
				checked++
				if id != "read-only" {
					t.Errorf("policy checked on %s", id)
				}
				return tc.policyErr
			}})
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range reports[0].Checks {
				if c.Name == "Read-only access" && c.Status != tc.want {
					t.Errorf("check=%+v, want %s", c, tc.want)
				}
			}
			if probes != 1 || checked != 1 {
				t.Errorf("read-only probes=%d policy checks=%d, want 1 and 1", probes, checked)
			}
		})
	}
}

func TestReviewReadonlyUnbornBaselineUnavailable(t *testing.T) {
	store, sess, backend := workerFixture(t, "read-only")
	st, _ := store.Load("j1")
	st.Request.Harness = "opencode"
	st.Request.Cwd = unbornRepo(t)
	write(t, st.Request.Cwd, "pre.txt", "before\n")
	write(t, st.Request.Cwd, ".gitattributes", "pre.txt filter=fail\n")
	git(t, st.Request.Cwd, "config", "filter.fail.clean", "false")
	git(t, st.Request.Cwd, "config", "filter.fail.required", "true")
	store.Save(st)
	sess.script = func(s *fakeSession) {
		t.Error("unverifiable readonly job reached harness")
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, _ := RunJob(context.Background(), store, "j1", newFakeHarness(backend), WorkerConfig{})
	if r.Status != StatusFailed || !strings.Contains(strings.Join(r.Warnings, " "), "Read-only verification unavailable") {
		t.Errorf("status=%s warnings=%v", r.Status, r.Warnings)
	}
}
