package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestMinimalConfigJobPolicyAndPersistence(t *testing.T) {
	for _, name := range []string{"codex", "kimi"} {
		for _, user := range []bool{true, false} {
			for _, resume := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/user=%v/resume=%v", name, user, resume), func(t *testing.T) {
					store, sess, backend := workerFixture(t, "write")
					if name == "kimi" {
						fakeKimiUserHome(t)
					}
					base, _ := store.Load("j1")
					base.Request.Harness = name
					base.Request.WithUserConfig = user
					if resume {
						base.Request.Resume = "previous-session"
					}
					st, err := store.Create("policy", base.Request)
					if err != nil {
						t.Fatal(err)
					}
					want := "user"
					if !user {
						want = "minimal"
					}
					if st.ConfigMode != want || st.Request.WithUserConfig != user {
						t.Fatalf("queued state=%+v, want %s", st, want)
					}
					sess.script = func(s *fakeSession) {
						s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
					}
					h, _ := Lookup(name)
					h.Backend = func() cockpit.Backend { return backend }
					h.Version, h.Verify, h.Conceal = nil, nil, nil
					r, err := RunJob(context.Background(), store, "policy", h, WorkerConfig{})
					if err != nil {
						t.Fatal(err)
					}
					if backend.gotOpts.MinimalConfig != (want == "minimal") || !backend.gotOpts.Hidden || r.ConfigMode != want {
						t.Fatalf("opts=%+v result=%+v", backend.gotOpts, r)
					}
					if resume && backend.resumed != "previous-session" {
						t.Fatal("resume lost")
					}
					data, _ := os.ReadFile(store.ResultPath("policy"))
					var persisted map[string]any
					_ = json.Unmarshal(data, &persisted)
					if persisted["configMode"] != want || !strings.Contains(r.Markdown(), "Harness: "+name+" ("+want+" config)") {
						t.Fatalf("result=%s markdown=%s", data, r.Markdown())
					}
					// The Harness line already reports the config mode; a per-job
					// warning on every kimi run would only add noise.
					if contains(r.Warnings, "configuration") {
						t.Fatalf("config mode belongs in the Harness line: %v", r.Warnings)
					}
				})
			}
		}
	}
}

func TestMinimalConfigFallbackResultRetainsMode(t *testing.T) {
	for _, user := range []bool{true, false} {
		store := Store{Root: t.TempDir()}
		_, err := store.Create("queued", Request{Harness: "codex", WithUserConfig: user})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.RequestCancel("queued"); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(store.ResultPath("queued"))
		var r Result
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		want := "minimal"
		if user {
			want = "user"
		}
		if r.ConfigMode != want {
			t.Fatalf("fallback=%s", data)
		}
	}
}

func TestMinimalConfigDoctorUsesDefaultPolicy(t *testing.T) {
	h, _ := Lookup("codex")
	h.Version = func(context.Context) (string, error) { return "test", nil }
	count := 0
	_, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{
		lookPath: func(string) (string, error) { return "/fake/codex", nil },
		run: func(_ context.Context, s Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
			count++
			st, err := s.Load(id)
			if err != nil || st.Request.WithUserConfig || st.ConfigMode != "minimal" {
				t.Fatalf("doctor policy: %+v %v", st, err)
			}
			return Result{Status: StatusFailed}, nil
		},
	})
	if err != nil || count != 2 {
		t.Fatalf("calls=%d err=%v", count, err)
	}
}

func TestMinimalConfigFinishUsesRecordedModeForLegacyJobs(t *testing.T) {
	store := Store{Root: t.TempDir()}
	st, err := store.Create("legacy", Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	// Pre-AJ-636 jobs loaded user configuration; never label those minimal.
	st.ConfigMode = ""
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	r := Result{Status: StatusFailed}
	if err := store.Finish("legacy", &r, "worker absent", time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.ConfigMode != "user" {
		t.Fatalf("legacy mode=%q", r.ConfigMode)
	}
}
