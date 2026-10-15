package sub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func assertPartialUsage(t *testing.T, store Store, r Result) {
	t.Helper()
	data, err := os.ReadFile(store.ResultPath(r.JobID))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["usagePartial"] != true || !contains(r.Warnings, "Usage may be incomplete") {
		t.Errorf("missing partial usage flag/warning: %s", data)
	}
	st, err := store.Load(r.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if out := summaryJSON(t, store, st, nil); out["usagePartial"] != true {
		t.Errorf("summary lost partial usage: %v", out)
	}
}

// Each source keeps the same accounting unit on failures, cancellation, idle
// timeout and transport loss. Resumed exports/wires must exclude prior jobs.
func TestHarnessUsageOnAbnormalExit(t *testing.T) {
	for _, name := range []string{"codex", "kimi", "agy", "opencode"} {
		for _, ending := range []string{"failed", "canceled", "idle", "closed", "prompt-error", "succeeded"} {
			t.Run(name+"/"+ending, func(t *testing.T) {
				store, s, b := workerFixture(t, "write")
				h, _ := Lookup(name)
				h.Version, h.Conceal = nil, nil
				var record, home string
				switch name {
				case "kimi":
					fakeKimiUserHome(t)
					home, _ = managedKimiHome()
					writeKimiJobRecord(t, home, s.ThreadID(), "")
				case "agy":
					fakeAgyUserHome(t)
				case "opencode":
					fakeOpencodeUserHome(t)
					root, _ := managedHarnessHome("opencode")
					record = fakeOpencodeExport(t, filepath.Join(root, "share"), filepath.Join(root, "config"))
					if err := os.WriteFile(record, []byte(`{"messages":[{"info":{"role":"assistant","tokens":{"input":9000},"cost":9}}]}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				st, _ := store.Load("j1")
				st.Request.Harness, st.ConfigMode = name, "minimal"
				if name == "opencode" || name == "kimi" {
					st.Request.Resume = s.ThreadID()
				}
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
				h.Backend = func() cockpit.Backend { return b }
				if ending == "prompt-error" {
					h.Backend = func() cockpit.Backend {
						return lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
							return &lifecycleSession{fakeSession: s, prompt: func(context.Context) error {
								s.script(s)
								return errors.New("prompt RPC error")
							}, interrupt: func() error { return s.Interrupt(context.Background()) }}, nil
						}}
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s.script = func(s *fakeSession) {
					switch name {
					case "opencode":
						data := `{"messages":[{"info":{"role":"assistant","tokens":{"input":9000},"cost":9}},{"info":{"role":"user"}},{"info":{"role":"assistant","providerID":"test","modelID":"flash","tokens":{"input":20,"cache":{"read":100},"output":3},"cost":0.1}},{"info":{"role":"assistant","providerID":"test","modelID":"flash","tokens":{"input":3,"output":4},"cost":0.15}}]}`
						if err := os.WriteFile(record, []byte(data), 0600); err != nil {
							t.Error(err)
						}
						if ending == "succeeded" {
							// Successful prompts retain the established token source.
							s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 333, OutputTokens: 8}}
						}
					case "kimi":
						path := filepath.Join(home, "sessions", s.ThreadID(), "agents/main/wire.jsonl")
						appendKimiUsageFixture(t, path, "{\"type\":\"llm.request\",\"model\":\"current-model\"}\n"+kimiCurrentUsage+"{\"type\":\"prompt.completed\"}\n")
					case "codex":
						s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 1}, Raw: json.RawMessage(`{"threadId":"main","tokenUsage":{"total":{"inputTokens":100,"outputTokens":3}}}`)}
						s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 1}, Raw: json.RawMessage(`{"threadId":"child","tokenUsage":{"total":{"inputTokens":23,"outputTokens":4}}}`)}
					case "agy":
						s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 100, OutputTokens: 3}}
						s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 123, OutputTokens: 7}}
					}
					switch ending {
					case "failed":
						s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "provider error"}
					case "canceled":
						cancel()
					case "closed":
						close(s.events)
					case "succeeded":
						s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
					}
				}
				cfg := WorkerConfig{IdleTimeout: 100 * time.Millisecond, KimiWireTimeout: 50 * time.Millisecond}
				r, err := RunJob(ctx, store, "j1", h, cfg)
				wantStatus := StatusFailed
				wantInput, wantOutput := 123, 7
				if ending == "canceled" {
					wantStatus = StatusCanceled
				} else if ending == "succeeded" {
					wantStatus = StatusSucceeded
					if name == "opencode" {
						wantInput, wantOutput = 333, 8
					}
				}
				if err != nil || r.Status != wantStatus || r.InputTokens != wantInput || r.OutputTokens != wantOutput {
					t.Errorf("usage/status lost: %+v err=%v", r, err)
				}
				if name == "opencode" && (r.CostUSD != 0.25 || !r.ModelVerified || r.ModelActual != "test/flash") {
					t.Errorf("export recovery=%+v", r)
				}
				if ending == "succeeded" {
					if r.UsagePartial {
						t.Errorf("complete successful usage marked partial: %+v", r)
					}
				} else {
					assertPartialUsage(t, store, r)
				}
			})
		}
	}
}

func TestAgyFailedJobKeepsAllReportedSteps(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	fakeAgyUserHome(t)
	bin := t.TempDir()
	script := `#!/bin/sh
echo '{"event":"init","conversation_id":"failed-agy","init":{"model":"model-a"}}'
echo '{"event":"step_update","step_update":{"step_index":0,"step_type":"agent_response","state":"DONE","usage":{"input_tokens":10,"cache_read_tokens":90,"output_tokens":3}}}'
echo '{"event":"step_update","step_update":{"step_index":1,"step_type":"agent_response","state":"ACTIVE","usage":{"input_tokens":2,"cache_read_tokens":10,"output_tokens":1}}}'
echo '{"event":"step_update","step_update":{"step_index":1,"step_type":"agent_response","state":"ERROR","usage":{"input_tokens":3,"cache_read_tokens":20,"output_tokens":4}}}'
echo '{"event":"result","result":{"status":"ERROR","error":"provider error","usage":{"input_tokens":0,"output_tokens":0}}}'
exit 1
`
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h, _ := Lookup("agy")
	h.Version = nil
	st, _ := store.Load("j1")
	st.Request.Harness, st.ConfigMode = "agy", "minimal"
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || r.Status != StatusFailed || r.InputTokens != 123 || r.OutputTokens != 7 {
		t.Errorf("step usage lost or counted twice: %+v err=%v", r, err)
	}
	assertPartialUsage(t, store, r)
}

func TestAbsentWorkerRecoversLoggedUsage(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	st, err := store.claim(context.Background(), "j1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	log := "backend diagnostic\n" + `{"kind":"usage","usage":{"inputTokens":123,"outputTokens":7,"costUSD":0.25}}` + "\n" + `{"kind":"usage","usage":{"inputTokens":999}}`
	if err := os.WriteFile(store.LogPath(st.ID), []byte(log), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.WithState(context.Background(), st.ID, func(current *State) error {
		return store.fallbackLocked(current, StatusFailed, "worker absent")
	}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.ResultPath(st.ID))
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.InputTokens != 123 || r.OutputTokens != 7 || r.CostUSD != 0.25 {
		t.Errorf("logged usage lost: %+v", r)
	}
	assertPartialUsage(t, store, r)
}

func TestAbsentOpencodeWorkerDoesNotClaimSessionCost(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	st, _ := store.Load("j1")
	st.Request.Harness, st.Request.Resume = "opencode", "old-session"
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if _, err := store.claim(context.Background(), st.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	log := `{"kind":"usage","usage":{"inputTokens":123,"outputTokens":7,"costUSD":9.25}}` + "\n"
	if err := os.WriteFile(store.LogPath(st.ID), []byte(log), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.WithState(context.Background(), st.ID, func(current *State) error {
		return store.fallbackLocked(current, StatusFailed, "worker absent")
	}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.ResultPath(st.ID))
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.InputTokens != 123 || r.OutputTokens != 7 || r.CostUSD != 0 {
		t.Fatalf("session total must not become job cost: %+v", r)
	}
	assertPartialUsage(t, store, r)
}

func TestUsagePartialAfterIndependentVerificationFailure(t *testing.T) {
	for _, name := range []string{"kimi", "opencode"} {
		t.Run(name, func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			h, _ := Lookup(name)
			if name == "kimi" {
				fakeKimiUserHome(t)
			} else {
				fakeOpencodeUserHome(t) // Fake CLI exits 99; never invokes the installed harness.
			}
			st, _ := store.Load("j1")
			st.Request.Harness, st.ConfigMode = name, "minimal"
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			h.Version, h.Conceal = nil, nil
			h.Backend = func() cockpit.Backend { return b }
			s.script = func(s *fakeSession) {
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			r, err := RunJob(context.Background(), store, st.ID, h, WorkerConfig{KimiWireTimeout: time.Millisecond})
			if err != nil || r.Status != StatusSucceeded {
				t.Fatalf("accounting failure changed outcome: %+v %v", r, err)
			}
			assertPartialUsage(t, store, r)
		})
	}
}
