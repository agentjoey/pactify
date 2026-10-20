package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestEmptyReplyFollowup(t *testing.T) {
	for _, name := range []string{"codex", "kimi", "agy", "opencode"} {
		for _, access := range []string{"read-only", "write"} {
			for _, reply := range []string{"Result and checks", " \n\t"} {
				t.Run(fmt.Sprintf("%s/%s/%q", name, access, reply), func(t *testing.T) {
					store, s, _ := workerFixture(t, access)
					st, _ := store.Load("j1")
					st.Request.Harness, st.ConfigMode = name, "minimal"
					if name == "kimi" && access == "read-only" {
						source := fakeKimiUserHome(t)
						if err := os.WriteFile(filepath.Join(source, "config.toml"), []byte(kimiROConfigFixture), 0600); err != nil {
							t.Fatal(err)
						}
					}
					if err := recordVerifiedVersion(context.Background(), filepath.Dir(store.Root), name, "9.9.9", time.Now()); err != nil {
						t.Fatal(err)
					}
					zero := 0
					st.Request.Retries = &zero
					if err := store.Save(st); err != nil {
						t.Fatal(err)
					}
					var prompts []string
					starts := 0
					b := lifecycleBackend{start: func(_ context.Context, opts cockpit.StartOpts) (cockpit.Session, error) {
						starts++
						if string(opts.Access) != access {
							t.Errorf("access=%s, want %s", opts.Access, access)
						}
						if name == "kimi" && access == "read-only" {
							writeKimiJobRecord(t, childEnv(opts.Env, "KIMI_CODE_HOME"), "same-session", `{"type":"llm.tools_snapshot","tools":[{"name":"Read"}]}`+"\n")
						}
						return &retrySession{fakeSession: s, id: "same-session", prompt: func(_ context.Context, m cockpit.UserMessage) error {
							prompts = append(prompts, m.Text)
							n := len(prompts)
							if n > 2 {
								return fmt.Errorf("unexpected third prompt")
							}
							s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "read"}}
							input := n * 10
							if name == "kimi" {
								input = 10 // ACP reports per-prompt usage.
							}
							s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: input}}
							text := " \n\t"
							if n == 2 {
								text = reply
							}
							s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: text}
							s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
							return nil
						}}, nil
					}}
					h := newFakeHarness(nil)
					h.Name = name
					if name == "kimi" && access == "read-only" {
						kimi, _ := Lookup("kimi")
						h.Home = kimi.Home
					}
					h.Backend = func() cockpit.Backend { return b }
					r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{ToolCallLimit: 2})
					wantPrompts := []string{st.Request.Brief, "You ended without a written answer. Reply now with your result: what you found or changed and which checks you ran. Do not start new work."}
					if err != nil || r.Status != StatusSucceeded || r.FailureClass != "" || r.FinalText != reply || r.EmptyReplyFollowups != 1 || starts != 1 || !slices.Equal(prompts, wantPrompts) || r.SessionID != "same-session" || r.InputTokens != 20 || r.ToolCalls != 2 || len(r.Retries) != 0 {
						t.Fatalf("result=%+v prompts=%q starts=%d err=%v", r, prompts, starts, err)
					}
					wantWarning := "empty final reply after one follow-up: the harness ended without a text answer. Check pactify sub logs j1, or resume to ask for the result."
					empty := strings.TrimSpace(reply) == ""
					if empty && !slices.Contains(r.Warnings, wantWarning) || !empty && len(r.Warnings) != 0 {
						t.Fatalf("warnings=%q", r.Warnings)
					}
					data, err := os.ReadFile(store.ResultPath("j1"))
					var persisted Result
					if err != nil || json.Unmarshal(data, &persisted) != nil || persisted.EmptyReplyFollowups != 1 {
						t.Fatalf("persisted=%+v err=%v", persisted, err)
					}
					current, err := store.Load("j1")
					if err != nil {
						t.Fatal(err)
					}
					row := store.Summarize(current, nil, time.Now())
					hasWarningAttention := slices.ContainsFunc(row.Attention, func(a Attention) bool { return a.Kind == AttentionWarnings })
					if hasWarningAttention != empty || empty && current.Error != wantWarning {
						t.Fatalf("summary=%+v state error=%q", row, current.Error)
					}
					log, err := os.ReadFile(store.LogPath("j1"))
					if err != nil || strings.Count(string(log), `"state":"empty_reply_followup"`) != 1 || !strings.Contains(string(log), `"text":"`+name+`"`) {
						t.Fatalf("missing followup log marker: %s err=%v", log, err)
					}
				})
			}
		}
	}
}

func TestEmptyReplyNoFollowup(t *testing.T) {
	for _, mode := range []string{"text", "failed", "prompt failed", "timeout", "idle timeout", "total timeout", "canceled", "cancel requested", "closed", "closed after done"} {
		t.Run(mode, func(t *testing.T) {
			store, s, _ := workerFixture(t, "write")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := WorkerConfig{IdleTimeout: 20 * time.Millisecond}
			if mode == "total timeout" {
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
				cfg.IdleTimeout = time.Minute
			}
			prompts := 0
			b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
				return &retrySession{fakeSession: s, id: "same-session", prompt: func(context.Context, cockpit.UserMessage) error {
					prompts++
					e := cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
					switch mode {
					case "text":
						s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "Done"}
					case "failed":
						e.State, e.Err = cockpit.StateTurnFailed, "broken"
					case "prompt failed":
						return fmt.Errorf("broken prompt")
					case "timeout":
						e.State, e.Err = cockpit.StateTurnFailed, "agy print_timeout: print timeout returned no content"
					case "idle timeout", "total timeout":
						return nil // Interrupt supplies the terminal event.
					case "canceled":
						cancel()
					case "cancel requested":
						if _, err := store.RequestCancel("j1"); err != nil {
							t.Error(err)
						}
					case "closed":
						close(s.events)
						return nil
					case "closed after done":
						s.events <- e
						close(s.events)
						return nil
					}
					s.events <- e
					return nil
				}}, nil
			}}
			h := newFakeHarness(nil)
			h.Backend = func() cockpit.Backend { return b }
			r, err := RunJob(ctx, store, "j1", h, cfg)
			if err != nil || prompts != 1 || r.EmptyReplyFollowups != 0 || contains(r.Warnings, "empty final reply after one follow-up:") {
				t.Fatalf("result=%+v prompts=%d err=%v", r, prompts, err)
			}
			wantSuccess := mode == "text" || mode == "closed after done"
			if wantSuccess != (r.Status == StatusSucceeded) {
				t.Fatalf("unexpected status: %+v", r)
			}
		})
	}
}

func TestEmptyReplyFollowupAndTransientRetry(t *testing.T) {
	for _, failures := range [][]int{{1}, {2}, {2, 3}, {}} {
		for _, lastFailure := range []string{"", "Individual quota reached"} {
			t.Run(fmt.Sprintf("%v/%s", failures, lastFailure), func(t *testing.T) {
				store, s, _ := workerFixture(t, "write")
				st, _ := store.Load("j1")
				st.Request.Harness = "agy"
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
				var prompts []string
				b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
					return &retrySession{fakeSession: s, id: "same-session", prompt: func(_ context.Context, m cockpit.UserMessage) error {
						prompts = append(prompts, m.Text)
						n := len(prompts)
						if n > len(failures)+2 {
							return fmt.Errorf("unexpected extra prompt")
						}
						e := cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
						if slices.Contains(failures, n) {
							e.State, e.Err = cockpit.StateTurnFailed, "UNAVAILABLE (code 503)"
						} else if n == len(failures)+2 && lastFailure != "" {
							e.State, e.Err = cockpit.StateTurnFailed, lastFailure
						}
						s.events <- e
						return nil
					}}, nil
				}}
				h := newFakeHarness(nil)
				h.TransientRetry = true
				h.Backend = func() cockpit.Backend { return b }
				r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return 0 }})
				followups := 0
				for _, p := range prompts {
					if p == emptyReplyFollowupPrompt {
						followups++
					}
				}
				if err != nil || len(prompts) != len(failures)+2 || followups != 1 || r.EmptyReplyFollowups != 1 || len(r.Retries) != len(failures) {
					t.Fatalf("result=%+v prompts=%q err=%v", r, prompts, err)
				}
				if lastFailure == "" {
					if r.Status != StatusSucceeded || !contains(r.Warnings, "empty final reply after one follow-up:") {
						t.Fatalf("result=%+v", r)
					}
				} else if r.Status != StatusFailed || r.FailureClass != "quota" || contains(r.Warnings, "empty final reply after one follow-up:") {
					t.Fatalf("result=%+v", r)
				}
			})
		}
	}
}

func TestEmptyReplyFollowupToolLimit(t *testing.T) {
	store, s, _ := workerFixture(t, "read-only")
	prompts := 0
	b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
		return &retrySession{fakeSession: s, id: "same-session", prompt: func(context.Context, cockpit.UserMessage) error {
			prompts++
			s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "read"}}
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			return nil
		}}, nil
	}}
	h := newFakeHarness(nil)
	h.Backend = func() cockpit.Backend { return b }
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{ToolCallLimit: 1})
	if err != nil || r.Status != StatusFailed || prompts != 2 || r.EmptyReplyFollowups != 1 || r.ToolCalls != 2 || !contains(r.Warnings, "Tool-call limit exceeded (2 calls)") || contains(r.Warnings, "empty final reply after one follow-up:") {
		t.Fatalf("result=%+v prompts=%d err=%v", r, prompts, err)
	}
}

// A session that closes after an empty turn cannot take the follow-up; the job
// still must not succeed silently.
func TestEmptyReplyClosedSessionWarns(t *testing.T) {
	store, s, _ := workerFixture(t, "read-only")
	st, _ := store.Load("j1")
	st.Request.Harness, st.ConfigMode = "codex", "minimal"
	if err := recordVerifiedVersion(context.Background(), filepath.Dir(store.Root), "codex", "9.9.9", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	prompts := 0
	b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
		return &retrySession{fakeSession: s, id: "closed-session", prompt: func(context.Context, cockpit.UserMessage) error {
			prompts++
			s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: ""}
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			close(s.events)
			return nil
		}}, nil
	}}
	h := newFakeHarness(nil)
	h.Name = "codex"
	h.Backend = func() cockpit.Backend { return b }
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	want := "empty final reply: the session ended before a follow-up could be sent. Check pactify sub logs j1, or resume to ask for the result."
	if err != nil || r.Status != StatusSucceeded || prompts != 1 || r.EmptyReplyFollowups != 0 || !slices.Contains(r.Warnings, want) {
		t.Fatalf("result=%+v prompts=%d err=%v", r, prompts, err)
	}
}
