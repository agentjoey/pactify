package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestRunJobExperimentalWarningFromRegistry(t *testing.T) {
	for _, name := range []string{"codex", "kimi", "agy", "opencode", "renamed-agy"} {
		for _, outcome := range []string{"succeeded", "failed", "startup-failed", "canceled"} {
			t.Run(name+"/"+outcome, func(t *testing.T) {
				store, s, b := workerFixture(t, "write")
				registered := name
				if name == "renamed-agy" {
					registered = "agy"
				}
				h, ok := Lookup(registered)
				if !ok {
					t.Fatal("missing harness")
				}
				fixture := newFakeHarness(b)
				h.Name, h.Backend, h.Version, h.Verify, h.Conceal = name, fixture.Backend, fixture.Version, fixture.Verify, nil
				if registered == "opencode" {
					h.Version = func(context.Context) (string, error) { return "1.19.0", nil }
				}
				st, err := store.Load("j1")
				if err != nil {
					t.Fatal(err)
				}
				st.Request.Harness = name
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s.script = func(s *fakeSession) {
					if outcome == "canceled" {
						cancel()
						return
					}
					state := cockpit.StateTurnCompleted
					if outcome == "failed" {
						state = cockpit.StateTurnFailed
					}
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "DONE"}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: state, Err: "turn failed"}
				}
				if outcome == "startup-failed" {
					b.startErr = fmt.Errorf("startup failed")
				}
				r, err := RunJob(ctx, store, "j1", h, WorkerConfig{})
				if (err != nil) != (outcome == "startup-failed") {
					t.Fatalf("unexpected error: %v", err)
				}
				wantStatus := Status(outcome)
				if outcome == "startup-failed" {
					wantStatus = StatusFailed
				}
				if r.Status != wantStatus {
					t.Fatalf("status=%s, want %s", r.Status, wantStatus)
				}
				wantError := ""
				switch outcome {
				case "failed":
					wantError = "turn failed"
				case "startup-failed":
					wantError = "startup failed"
				case "canceled":
					wantError = "Task canceled"
				}
				state, err := store.Load("j1")
				if err != nil || state.Status != wantStatus || state.Error != wantError {
					t.Fatalf("state=%+v err=%v; want error %q", state, err, wantError)
				}
				want := registered + " is experimental: no OS sandbox for writes; results vary by model"
				if name == "codex" || name == "kimi" || name == "opencode" {
					if contains(r.Warnings, "experimental") || strings.Contains(r.Markdown(), "experimental") {
						t.Fatalf("%s warnings=%v", name, r.Warnings)
					}
				} else if len(r.Warnings) == 0 || r.Warnings[0] != want || !strings.HasPrefix(r.Markdown(), "⚠️ "+want) {
					t.Fatalf("experimental warning missing: %+v", r)
				}
				if name == "opencode" && !contains(r.Warnings, "opencode has no OS sandbox for writes; file-tool directory guard only; shell is not confined") {
					t.Fatalf("missing opencode permission boundary warning: %v", r.Warnings)
				}
				data, err := os.ReadFile(store.ResultPath("j1"))
				var saved Result
				if err != nil || json.Unmarshal(data, &saved) != nil || strings.Join(saved.Warnings, "\n") != strings.Join(r.Warnings, "\n") || saved.Status != wantStatus {
					t.Fatalf("saved result=%s err=%v", data, err)
				}
			})
		}
	}
}
