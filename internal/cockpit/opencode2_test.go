package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/testenv"
)

func TestOpencode2Peer(t *testing.T) { testenv.Opencode2Peer(t) }
func oc2Opts(t *testing.T) StartOpts {
	t.Helper()
	testenv.InstallOpencode2(t)
	return StartOpts{RepoDir: t.TempDir(), Model: "opencode/test-free", Effort: "low", Access: AccessWrite, MinimalConfig: true, Stderr: io.Discard}
}
func oc2Requests(t *testing.T) []struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
} {
	t.Helper()
	b, err := os.ReadFile(os.Getenv("COCKPIT_OC2_REQUESTS"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Method string
		Path   string
		Query  string
		Body   map[string]any
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var row struct {
			Method string
			Path   string
			Query  string
			Body   map[string]any
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return rows
}
func TestOpencode2Flows(t *testing.T) {
	for _, scenario := range []string{"write", "readonly", "permission", "cancel", "failure", "early-failure", "history-failure", "reconnect", "reconnect-permission", "reconnect-wait", "background", "shutdown", "inactivity"} {
		t.Run(scenario, func(t *testing.T) {
			opts := oc2Opts(t)
			opts.SystemPrompt = "System instruction"
			t.Setenv("COCKPIT_OC2_SCENARIO", scenario)
			if scenario == "readonly" {
				opts.Access = AccessReadOnly
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s, err := NewOpencode2Backend().Start(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			go func() {
				for a := range s.Approvals() {
					if scenario == "reconnect-permission" && (!strings.Contains(string(a.RawInput), "git status") || !strings.Contains(string(a.RawInput), "retained")) {
						t.Error("recovered permission lost review context")
					}
					if err := a.Respond(DecisionAllowForSession); err != nil {
						t.Error(err)
					}
				}
			}()
			done := make(chan error, 1)
			go func() { done <- s.Prompt(ctx, UserMessage{Text: "synthetic test"}) }()
			if scenario == "cancel" {
				time.AfterFunc(100*time.Millisecond, func() {
					if err := s.Interrupt(ctx); err != nil {
						t.Error(err)
					}
				})
			}
			events := []Event{}
			var terminal Event
		loop:
			for {
				select {
				case e := <-s.Events():
					events = append(events, e)
					if e.Kind == EventState {
						terminal = e
						break loop
					}
				case <-ctx.Done():
					t.Fatal("no terminal event", ctx.Err())
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			want := StateTurnCompleted
			if scenario == "failure" || scenario == "early-failure" || scenario == "cancel" || scenario == "shutdown" || scenario == "inactivity" {
				want = StateTurnFailed
			}
			if terminal.State != want {
				t.Fatalf("terminal=%+v", terminal)
			}
			if scenario == "failure" || scenario == "early-failure" {
				if f := opencodeFailure(terminal.Err, terminal.Raw); f.Class != "quota" {
					t.Fatal(f)
				}
			}
			if scenario == "cancel" {
				if f := opencodeFailure(terminal.Err, terminal.Raw); f.Class != "canceled" {
					t.Fatal(f)
				}
			}
			if scenario == "shutdown" || scenario == "inactivity" {
				if !strings.Contains(terminal.Err, scenario) {
					t.Fatalf("lost interruption reason: %+v", terminal)
				}
			}
			final := ""
			warnings := 0
			var usage *Usage
			tools := 0
			inputSeen := false
			background := false
			for _, e := range events {
				if e.State == "session.reasoning.delta" || e.State == "session.tool.progress" || e.State == "session.usage.updated" {
					t.Fatalf("noisy increment forwarded: %+v", e)
				}
				if e.Final {
					final = e.Text
				}
				if strings.Contains(e.Text, "FOREIGN") {
					t.Fatal("foreign event leaked")
				}
				if e.Text == "Opencode truncated tool output" {
					warnings++
				}
				if e.Usage != nil {
					usage = e.Usage
				}
				if e.Tool != nil {
					if e.Tool.Phase == "start" {
						tools++
					}
					if e.Tool.Phase == "start" && strings.Contains(e.Tool.Text, "git status") {
						inputSeen = true
					}
				}
				if strings.Contains(e.Text, "background child") {
					background = true
				}
			}
			if scenario == "history-failure" {
				if final != "STREAM_FINAL" {
					t.Fatal(final)
				}
			} else if final != "FINAL_TEXT" {
				t.Fatal(final)
			}
			if usage == nil || usage.InputTokens != 45 || usage.OutputTokens != 15 || usage.CostUSD != .75 {
				t.Fatalf("usage=%+v", usage)
			}
			if scenario == "write" && (warnings != 1 || tools != 1 || !inputSeen) {
				t.Fatalf("warnings=%d starts=%d input=%t", warnings, tools, inputSeen)
			}
			if scenario == "background" && !background {
				t.Fatal("missing background informational warning")
			}
			for _, r := range oc2Requests(t) {
				if r.Method == "POST" && r.Path == "/api/session" {
					if r.Body["title"] != "Pactify delegated task" || r.Body["agent"] != s.(InfoSession).Info().Access || r.Body["permissions"] == nil {
						t.Fatalf("bad create: %+v", r)
					}
				}
				if strings.HasSuffix(r.Path, "/reply") {
					want := "once"
					if scenario == "readonly" {
						want = "reject"
						if r.Body["message"] == nil {
							t.Fatal("reject missing feedback")
						}
					}
					if r.Body["decision"] != want {
						t.Fatalf("reply=%+v", r)
					}
				}
				if strings.HasSuffix(r.Path, "/form/form1") && r.Method == "DELETE" && !strings.Contains(r.Query, "message=") {
					t.Fatal("missing form feedback")
				}
			}
			if scenario == "write" {
				info := s.(InfoSession).Info()
				if info.Model != "opencode/test-free" || info.Effort != "low" {
					t.Fatalf("info=%+v", info)
				}
			}
		})
	}
}
func TestOpencode2ResumeAndModelDiscovery(t *testing.T) {
	opts := oc2Opts(t)
	t.Setenv("COCKPIT_OC2_SCENARIO", "resume-children")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := Opencode2ModelDetails(ctx, opts)
	if err != nil || len(d.Items) != 1 || d.Items[0] != "opencode/test-free" || len(d.Efforts) != 1 || d.Efforts[0] != "low" {
		t.Fatalf("details=%+v err=%v", d, err)
	}
	for _, id := range []string{"ses_root", "ses_missing"} {
		s, err := NewOpencode2Backend().(OptsResumer).ResumeWith(ctx, id, opts)
		if id == "ses_missing" {
			var start *StartError
			if !errors.As(err, &start) || start.ID != id || !strings.Contains(err.Error(), "SessionNotFoundError") {
				t.Fatalf("missing session: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
	seen := map[string]bool{}
	for _, r := range oc2Requests(t) {
		seen[r.Method+" "+r.Path] = true
		if r.Method == "PATCH" {
			rules := r.Body["permissions"].([]any)
			wantJSON, _ := json.Marshal(opencode2Permissions(AccessWrite))
			var want []any
			json.Unmarshal(wantJSON, &want)
			if !reflect.DeepEqual(rules, want) {
				t.Fatalf("resume must replace full policy: %+v", r)
			}
		}
	}
	for _, key := range []string{"PATCH /api/session/ses_child", "PATCH /api/session/ses_grandchild", "PATCH /api/session/ses_root", "POST /api/session/ses_root/agent", "POST /api/session/ses_root/model"} {
		if !seen[key] {
			t.Fatal("missing resume operation", key)
		}
	}
	opts.Model = ""
	opts.Effort = "low"
	defaultSession, err := NewOpencode2Backend().Start(ctx, opts)
	if err != nil {
		t.Fatal("effort with default model", err)
	}
	if err := defaultSession.Close(); err != nil {
		t.Fatal(err)
	}
	opts.Model = "opencode/test-free"
	opts.Effort = "invalid"
	if _, err = NewOpencode2Backend().Start(ctx, opts); err == nil || !strings.Contains(err.Error(), "effort") {
		t.Fatalf("invalid variant accepted: %v", err)
	}
	opts.Effort = ""
	opts.Model = "minimax-coding-plan/unavailable" // connectable integration with no connection
	if _, err = NewOpencode2Backend().Start(ctx, opts); err == nil || !strings.Contains(err.Error(), "pactify sub login opencode") || FailureFromError("opencode", err, time.Now()).Class != "auth" {
		t.Fatalf("missing login: %v", err)
	}
}
func TestOpencode2PermissionPolicyAndLease(t *testing.T) {
	opts := oc2Opts(t)
	opts.Access = AccessReadOnly
	env, err := Opencode2Env(opts)
	if err != nil {
		t.Fatal(err)
	}
	config := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "OPENCODE_CONFIG_CONTENT="); ok {
			config = v
		}
	}
	if strings.Contains(config, "continue_loop_on_deny") || strings.Contains(config, `"permission":`) || strings.Contains(config, `"list"`) {
		t.Fatal(config)
	}
	if got, want := opencode2Permissions(AccessWrite), ([]Opencode2Rule{{"edit", "*", "ask"}, {"shell", "*", "ask"}, {"question", "*", "deny"}, {"external_directory", "*", "deny"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("write rules=%+v want=%+v", got, want)
	}
	rules := opencode2Permissions(AccessReadOnly)
	if !reflect.DeepEqual(rules, []Opencode2Rule{{"*", "*", "deny"}, {"read", "*", "allow"}, {"glob", "*", "allow"}, {"grep", "*", "allow"}, {"shell", "*", "deny"}}) {
		t.Fatal(rules)
	}
	release, err := Opencode2Lease(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if r, err := Opencode2Lease(ctx, env); err == nil {
		r()
		t.Fatal("shared database lease was not exclusive")
	} else if !strings.Contains(err.Error(), "pactify sub login opencode") {
		t.Fatal("lock error lacks actionable hint", err)
	}
	release()
	r, err := Opencode2Lease(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	r()
	id := opencode2PromptID()
	if len(id) != 30 || !strings.HasPrefix(id, "msg_") {
		t.Fatal(id)
	}
}
func TestOpencode2FailureTypes(t *testing.T) {
	for typ, want := range map[string]string{"provider.auth": "auth", "provider.quota": "quota", "provider.rate-limit": "transient", "provider.internal": "transient", "provider.transport": "transient", "provider.timeout": "transient", "provider.unknown": "transient", "provider.no-route": "model", "unknown": "other"} {
		raw, _ := json.Marshal(map[string]string{"type": typ, "message": "test"})
		if got := ClassifyFailure("opencode", "test", raw, time.Now()).Class; got != want {
			t.Errorf("%s=%s", typ, got)
		}
		if typ == "provider.auth" && !strings.Contains(opencode2ErrorText(raw), "pactify sub login opencode") {
			t.Fatal("missing auth hint")
		}
	}
}

func TestOpencode2QuotaReset(t *testing.T) {
	raw := json.RawMessage(`{"type":"provider.quota","message":"Quota exhausted until 2026-10-19T00:00:00Z"}`)
	f := opencodeFailure("", raw)
	if f.Class != "quota" || f.ResetAt == nil || f.ResetAt.Format(time.RFC3339) != "2026-10-19T00:00:00Z" {
		t.Fatalf("quota=%+v", f)
	}
}
