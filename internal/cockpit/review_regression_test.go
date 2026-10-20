package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/agentjoey/pactify/internal/procx"
	"github.com/agentjoey/pactify/internal/testenv"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
)

func TestReviewACPFinalAfterTool(t *testing.T) {
	s := newACPSession(nil, "sid")
	defer s.Close()
	s.promptFn = func(context.Context, string) (acp.StopReason, error) {
		s.dispatchEvent(Event{Kind: EventMessage, Text: "I'll inspect..."})
		s.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: "read"}})
		s.dispatchEvent(Event{Kind: EventMessage, Text: "FINAL ANSWER"})
		return "end_turn", nil
	}
	if err := s.Prompt(context.Background(), UserMessage{}); err != nil {
		t.Fatal(err)
	}
	for e := range s.Events() {
		if e.Final {
			if e.Text != "FINAL ANSWER" {
				t.Fatalf("final=%q", e.Text)
			}
			break
		}
	}
}

// read-only denies bash outright even when the user's config allows
// it, and every command, including formerly allowed ones and the old bypass
// shapes, ends in deny under the session rules (evaluated last in 1.19).
func TestReviewReadonlyBashDenied(t *testing.T) {
	env, err := OpencodeEnv(StartOpts{Access: AccessReadOnly, Env: []string{`OPENCODE_CONFIG_CONTENT={"permission":{"*":"allow","bash":"allow"}}`}})
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "OPENCODE_CONFIG_CONTENT="); ok {
			raw = v
		}
	}
	var cfg struct {
		Permission map[string]any `json:"permission"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Permission["bash"] != "deny" || cfg.Permission["*"] != "deny" {
		t.Fatalf("read-only config permission = %v", cfg.Permission)
	}
	for _, cmd := range []string{"ls", "git status", "git diff", "cat README.md > x.txt", "find . -delete", "rg --pre evil x", "git diff --output=.env", "ls; touch x", "ls | sh", "ls $(touch x)", "ls `touch x`", "> x"} {
		if action := opencodeLastAction(opencodeReadonlyPermissions(), "bash", cmd); action != "deny" {
			t.Errorf("read-only bash %q: %s", cmd, action)
		}
	}
}

func TestReviewFailureClassification(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"name":"APIError","data":{"statusCode":401,"isRetryable":true}}`, "auth"},
		{`{"name":"APIError","data":{"statusCode":403}}`, "auth"},
		{`{"name":"ProviderAuthError","data":{"message":"missing key"}}`, "auth"},
		{`{"name":"APIError","data":{"statusCode":429,"message":"usage limit reached"}}`, "quota"},
	} {
		if got := opencodeFailure("", json.RawMessage(tc.raw)); got.Class != tc.want {
			t.Errorf("%s: %s want %s", tc.raw, got.Class, tc.want)
		}
	}
}

// A replay keeps parent and child accounting separate, as 1.19's PartUpdated
// projection updates only part.sessionID (never its ancestors).
func reviewReplay(t *testing.T, scenario string) (*opencodeSession, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)
	var mu sync.Mutex
	started, finished := false, false
	connections := 0
	reply := false
	secret := "review-secret-01234567890123456789"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pw, _ := r.BasicAuth(); pw != secret {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /event":
			mu.Lock()
			connections++
			connection := connections
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"type\":\"server.connected\",\"properties\":{}}\n\n")
			w.(http.Flusher).Flush()
			for {
				mu.Lock()
				ready := started
				mu.Unlock()
				if ready {
					break
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(time.Millisecond):
				}
			}
			send := func(v string) { fmt.Fprintf(w, "data: %s\n\n", v); w.(http.Flusher).Flush() }
			if (scenario == "reconnect" || scenario == "lost-idle") && connection == 1 || scenario == "exhausted" {
				send(`{"type":"message.updated","properties":{"info":{"id":"a","sessionID":"sid","role":"assistant"}}}`)
				if scenario == "lost-idle" {
					mu.Lock()
					finished = true
					mu.Unlock()
				}
				return
			}
			if scenario == "lost-idle" {
				<-r.Context().Done()
				return
			}
			if scenario == "children" {
				send(`{"type":"session.created","properties":{"info":{"id":"child","parentID":"sid"}}}`)
				send(`{"type":"permission.asked","properties":{"sessionID":"unrelated","id":"ignore","permission":"edit"}}`)
				send(`{"type":"permission.asked","properties":{"sessionID":"child","id":"child-perm","permission":"bash"}}`)
				for {
					mu.Lock()
					done := reply
					mu.Unlock()
					if done {
						break
					}
					select {
					case <-r.Context().Done():
						return
					case <-time.After(time.Millisecond):
					}
				}
				send(`{"type":"session.idle","properties":{"sessionID":"child"}}`)
			}
			mu.Lock()
			finished = true
			mu.Unlock()
			send(`{"type":"session.idle","properties":{"sessionID":"sid"}}`)
			<-r.Context().Done()
		case "POST /session/sid/prompt_async":
			if scenario == "http-error" {
				http.Error(w, "Service unavailable", 503)
				return
			}
			mu.Lock()
			started = true
			mu.Unlock()
			w.WriteHeader(204)
		case "POST /session/sid/abort":
			fmt.Fprint(w, "true")
		case "POST /permission/child-perm/reply":
			var body struct{ Reply string }
			json.NewDecoder(r.Body).Decode(&body)
			if body.Reply != "once" {
				t.Errorf("reply=%s", body.Reply)
			}
			mu.Lock()
			reply = true
			mu.Unlock()
			fmt.Fprint(w, "true")
		case "GET /permission":
			fmt.Fprint(w, `[]`)
		case "GET /session/status":
			mu.Lock()
			done := finished
			mu.Unlock()
			if scenario == "lost-idle" && done {
				fmt.Fprint(w, `{}`)
				return
			}
			status := "busy"
			if done {
				status = "idle"
			}
			fmt.Fprintf(w, `{"sid":{"type":%q}}`, status)
		case "GET /session/sid/children":
			if scenario == "children" {
				fmt.Fprint(w, `[{"id":"child","parentID":"sid"}]`)
			} else {
				fmt.Fprint(w, `[]`)
			}
		case "GET /session/child/children":
			fmt.Fprint(w, `[]`)
		case "GET /session/sid", "GET /session/child":
			n := 2
			if r.URL.Path == "/session/child" {
				n = 3
			}
			fmt.Fprintf(w, `{"id":%q,"tokens":{"input":%d,"output":1},"cost":0.1}`, strings.TrimPrefix(r.URL.Path, "/session/"), n)
		case "GET /session/sid/message":
			mu.Lock()
			done := finished
			mu.Unlock()
			if !done {
				fmt.Fprint(w, `[]`)
				return
			}
			text := "FINAL"
			if scenario == "secret" {
				text = secret
			}
			fmt.Fprintf(w, `[{"info":{"id":"a","sessionID":"sid","role":"assistant"},"parts":[{"type":"text","text":%q}]}]`, text)
		default:
			http.Error(w, "unexpected endpoint "+r.URL.Path, 404)
		}
	}))
	t.Cleanup(srv.Close)
	life, stop := context.WithCancel(ctx)
	t.Cleanup(stop)
	s := &opencodeSession{server: &opencodeServer{url: srv.URL, password: secret, http: srv.Client()}, id: "sid", opts: StartOpts{Access: AccessWrite}, base: opencodeSummary{Tokens: &opencodeTokens{}}, events: make(chan Event, 64), approvals: make(chan ApprovalRequest, 16), done: life.Done(), lifeCtx: life, stop: stop, info: SessionInfo{Access: "build"}}
	return s, ctx
}
func TestReviewServeReplay(t *testing.T) {
	for _, scenario := range []string{"children", "reconnect", "lost-idle", "exhausted", "secret"} {
		t.Run(scenario, func(t *testing.T) {
			s, ctx := reviewReplay(t, scenario)
			go func() {
				for {
					select {
					case a := <-s.Approvals():
						_ = a.Respond(DecisionAllow)
					case <-ctx.Done():
						return
					}
				}
			}()
			done := make(chan error, 1)
			go func() { done <- s.Prompt(ctx, UserMessage{Text: "test"}) }()
			var usage Usage
			final := ""
			for {
				select {
				case e := <-s.Events():
					b, _ := json.Marshal(e)
					if strings.Contains(string(b), s.server.password) {
						t.Error("server password leaked")
					}
					if e.Usage != nil {
						usage = *e.Usage
					}
					if e.Final {
						final = e.Text
					}
					if e.State == StateTurnCompleted || e.State == StateTurnFailed {
						want := StateTurnCompleted
						if scenario == "exhausted" {
							want = StateTurnFailed
							if got := opencodeFailure(e.Err, e.Raw); got.Class != "transient" {
								t.Errorf("class=%+v", got)
							}
						}
						if e.State != want {
							t.Errorf("terminal=%+v", e)
						}
						if scenario == "children" && (usage.InputTokens != 5 || usage.OutputTokens != 2) {
							t.Errorf("child usage=%+v", usage)
						}
						if scenario == "secret" && final != "[redacted]" {
							t.Error("final must be redacted")
						}
						if err := <-done; err != nil {
							t.Error(err)
						}
						return
					}
				case <-ctx.Done():
					t.Fatal("prompt did not finish", ctx.Err())
				}
			}
		})
	}
}

func TestReviewReadonlySessionOverridesUserConfig(t *testing.T) {
	data := testenv.InstallOpencodeServe(t)
	opts := StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly, MinimalConfig: false, Env: []string{`OPENCODE_CONFIG_CONTENT={"permission":"allow","agent":{"plan":{"permission":"allow"}}}`}}
	for _, resume := range []bool{false, true} {
		var sess Session
		var err error
		b := NewOpencodeBackend()
		if resume {
			sess, err = b.(OptsResumer).ResumeWith(context.Background(), "sid", opts)
		} else {
			sess, err = b.Start(context.Background(), opts)
		}
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()
		raw, err := os.ReadFile(filepath.Join(data, "permissions.json"))
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Permission []struct{ Permission, Pattern, Action string }
		}
		json.Unmarshal(raw, &body)
		action := "allow"
		for _, r := range body.Permission {
			if r.Permission == "*" || r.Permission == "bash" {
				if r.Pattern == "*" {
					action = r.Action
				}
			}
		}
		if action != "deny" {
			t.Errorf("resume=%v: session rules did not override user allow: %s", resume, raw)
		}
		if err := sess.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestReviewStopKeepsServer(t *testing.T) {
	testenv.InstallOpencodeServe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	sess, err := NewOpencodeBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Model: "opencode/test-free", Access: AccessWrite})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := sess.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	if alive, err := sess.(*opencodeSession).server.identity.Alive(); err != nil || !alive {
		t.Fatalf("Stop destroyed session server: alive=%v err=%v", alive, err)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Prompt(ctx, UserMessage{Text: "continue"}) }()
	for {
		select {
		case e := <-sess.Events():
			if e.State == StateTurnCompleted {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
func TestReviewShutdownBounded(t *testing.T) {
	s := &opencodeServer{done: make(chan struct{}), reaped: make(chan struct{}), stdout: io.NopCloser(strings.NewReader("")), http: &http.Client{}, descendants: map[procx.Identity]struct{}{}}
	finished := make(chan error, 1)
	go func() { finished <- s.close() }()
	select {
	case err := <-finished:
		if err == nil {
			t.Error("missing shutdown failure")
		}
	case <-time.After(2 * time.Second):
		close(s.reaped)
		t.Fatal("shutdown waited indefinitely")
	}
}
func TestReviewHTTPFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{{401, "auth"}, {403, "auth"}, {503, "transient"}, {429, "transient"}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "upstream", tc.status) }))
		s := &opencodeServer{url: srv.URL, http: srv.Client()}
		err := s.call(context.Background(), "GET", "/session", nil, nil)
		srv.Close()
		if got := FailureFromError("opencode", err, time.Now()); got.Class != tc.want {
			t.Errorf("HTTP %d: class=%s", tc.status, got.Class)
		}
	}
}
func TestReviewSecretSplitWrites(t *testing.T) {
	var out strings.Builder
	w := &secretWriter{out: &out, secret: "abcdef"}
	for _, v := range []string{"prefix ab", "c", "def suffix"} {
		if _, err := w.Write([]byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	w.flush()
	if out.String() != "prefix [redacted] suffix" {
		t.Fatal("split secret was not redacted")
	}
}
func TestReviewFractionalTokens(t *testing.T) {
	var s opencodeSummary
	if err := json.Unmarshal([]byte(`{"tokens":{"input":1.5,"output":2.25,"reasoning":0.5,"cache":{"read":0.25,"write":0.25}}}`), &s); err != nil {
		t.Fatal(err)
	}
	if got := s.usageSince(opencodeSummary{}); got.InputTokens != 2 || got.OutputTokens != 3 {
		t.Errorf("usage=%+v", got)
	}
}
func TestReviewPromptHTTPError(t *testing.T) {
	s, ctx := reviewReplay(t, "http-error")
	err := s.Prompt(ctx, UserMessage{})
	if got := FailureFromError("opencode", err, time.Now()); got.Class != "transient" {
		t.Errorf("failure=%+v err=%v", got, err)
	}
	for len(s.events) > 0 {
		e := <-s.events
		if e.State == StateTurnFailed {
			if got := opencodeFailure(e.Err, e.Raw); got.Class != "transient" {
				t.Errorf("event failure=%+v", got)
			}
		}
	}
}

// Approvers answer asynchronously, after the event stream that carried the
// request may have ended; the reply is bounded by the session instead.
func TestOpencodePermissionReplyOutlivesStream(t *testing.T) {
	replies := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/permission/") && strings.HasSuffix(r.URL.Path, "/reply") {
			b, _ := io.ReadAll(r.Body)
			replies <- string(b)
			w.Write([]byte("true"))
			return
		}
		http.Error(w, "unexpected "+r.URL.Path, 404)
	}))
	t.Cleanup(srv.Close)
	life, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	s := &opencodeSession{server: &opencodeServer{url: srv.URL, password: "p", http: srv.Client()}, id: "sid", opts: StartOpts{Access: AccessWrite}, events: make(chan Event, 64), approvals: make(chan ApprovalRequest, 16), done: life.Done(), lifeCtx: life, stop: stop}
	s.permission("per-1", "bash", json.RawMessage(`{"command":"git status"}`))
	a := <-s.Approvals()
	if err := a.Respond(DecisionAllow); err != nil {
		t.Fatalf("late reply failed: %v", err)
	}
	if got := <-replies; !strings.Contains(got, `"once"`) {
		t.Fatalf("reply body = %s", got)
	}
	s.permission("per-2", "bash", nil)
	late := <-s.Approvals()
	stop()
	if err := late.Respond(DecisionAllow); err == nil {
		t.Fatal("reply after Close must fail instead of reaching opencode")
	}
}
