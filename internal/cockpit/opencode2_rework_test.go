package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOpencode2RecoveryEvidence(t *testing.T) {
	for _, scenario := range []string{"reconnect-missing-boundary", "reconnect-no-outcome", "reconnect-missed-delivery", "reconnect-exhausted", "reconnect-wait", "reconnect-interrupted", "stream-stall", "cancel-before-delivery"} {
		t.Run(scenario, func(t *testing.T) {
			opts := oc2Opts(t)
			t.Setenv("COCKPIT_OC2_SCENARIO", scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := NewOpencode2Backend().Start(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if scenario == "stream-stall" {
				session.(*opencode2Session).inactivityTimeout = 100 * time.Millisecond
			}
			done := make(chan error, 1)
			go func() { done <- session.Prompt(ctx, UserMessage{Text: "test recovery"}) }()
			if scenario == "cancel-before-delivery" {
				time.AfterFunc(100*time.Millisecond, func() {
					if err := session.Interrupt(ctx); err != nil {
						t.Error(err)
					}
				})
			}
			var terminal Event
		loop:
			for {
				select {
				case e := <-session.Events():
					if e.Kind == EventState {
						terminal = e
						break loop
					}
				case <-ctx.Done():
					t.Fatal("prompt hung", ctx.Err())
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			want := StateTurnFailed
			if scenario == "reconnect-missed-delivery" || scenario == "reconnect-exhausted" || scenario == "reconnect-wait" {
				want = StateTurnCompleted
			}
			if terminal.State != want {
				t.Fatalf("terminal=%+v want=%s", terminal, want)
			}
			for _, pair := range [][2]string{{"reconnect-missing-boundary", "prompt boundary"}, {"reconnect-no-outcome", "no terminal outcome"}, {"reconnect-interrupted", "reason unavailable"}, {"stream-stall", "inactive"}} {
				if scenario == pair[0] && !strings.Contains(terminal.Err, pair[1]) {
					t.Fatalf("missing explanation: %+v", terminal)
				}
			}
			if scenario == "reconnect-interrupted" && opencodeFailure(terminal.Err, terminal.Raw).Class == "canceled" {
				t.Fatal("unknown interruption treated as user cancellation")
			}
			connects, interrupts := 0, 0
			for _, r := range oc2Requests(t) {
				if r.Path == "/api/event" {
					connects++
				}
				if strings.HasSuffix(r.Path, "/interrupt") {
					interrupts++
				}
			}
			if scenario == "reconnect-wait" && connects != 2 {
				t.Fatalf("wait settlement consumed reconnect attempts: %d", connects)
			}
			if scenario == "cancel-before-delivery" && interrupts != 2 {
				t.Fatalf("interrupt was not retried on delivery: %d", interrupts)
			}
		})
	}
}

func TestOpencode2StartupLeaseScope(t *testing.T) {
	opts := oc2Opts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := startOpencode2Server(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	second, err := startOpencode2Server(ctx, opts)
	if err != nil {
		t.Fatal("ready server retained startup lease", err)
	}
	defer second.close()
	env, _ := Opencode2Env(opts)
	release, err := Opencode2Lease(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	release()
	// User mode must not create directories or a lock under the user's data root.
	data := filepath.Join(t.TempDir(), "user-data")
	opts.MinimalConfig = false
	opts.Env = []string{"XDG_DATA_HOME=" + data}
	user, err := startOpencode2Server(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	user.close()
	if _, err := os.Stat(filepath.Join(data, "opencode/.pactify-v2.lock")); !os.IsNotExist(err) {
		t.Fatal("user home lock created", err)
	}
}

func TestOpencode2StartupFailureReleasesLease(t *testing.T) {
	for _, mode := range []string{"exit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			opts := oc2Opts(t)
			bin := t.TempDir()
			body := "#!/bin/sh\nexit 1\n"
			if mode == "cancel" {
				body = "#!/bin/sh\nexec sleep 60\n"
			}
			if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if s, err := startOpencode2Server(ctx, opts); err == nil {
				s.close()
				t.Fatal("startup should fail")
			}
			env, _ := Opencode2Env(opts)
			wait, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			release, err := Opencode2Lease(wait, env)
			if err != nil {
				t.Fatal("startup leaked lease", err)
			}
			release()
		})
	}
}

func TestOpencode2MissingDefaultModel(t *testing.T) {
	for _, scenario := range []string{"default-missing", "default-null"} {
		t.Run(scenario, func(t *testing.T) {
			opts := oc2Opts(t)
			opts.Model = ""
			t.Setenv("COCKPIT_OC2_SCENARIO", scenario)
			s, err := NewOpencode2Backend().Start(context.Background(), opts)
			if err == nil {
				s.Close()
				t.Fatal("missing default accepted")
			}
			if !errors.Is(err, ErrModelUnavailable) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestOpencode2UserDatabaseSelectionAndLegacyFailures(t *testing.T) {
	opts := StartOpts{Env: []string{"OPENCODE_DB=custom.db", "OPENCODE_DISABLE_CHANNEL_DB=0"}}
	env, err := Opencode2Env(opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(env, "\n"), "OPENCODE_DB=opencode.db") {
		t.Fatal("user database overridden")
	}
	raw := []byte(`{"type":"error","name":"APIError","data":{"statusCode":401}}`)
	if f := opencodeFailure("authentication failed: invalid api key", raw); f.Class != "auth" {
		t.Fatalf("legacy classifier masked: %+v", f)
	}
}

func TestOpencode2LastMatchingRules(t *testing.T) {
	for access, expected := range map[Access]map[string]Opencode2Rule{
		AccessWrite:    {"edit": {"edit", "*", "ask"}, "shell": {"shell", "*", "ask"}, "question": {"question", "*", "deny"}},
		AccessReadOnly: {"read": {"read", "*", "allow"}, "glob": {"glob", "*", "allow"}, "grep": {"grep", "*", "allow"}, "shell": {"shell", "*", "deny"}, "edit": {"*", "*", "deny"}, "question": {"*", "*", "deny"}, "webfetch": {"*", "*", "deny"}, "external_directory": {"*", "*", "deny"}, "execute": {"*", "*", "deny"}},
	} {
		for action, want := range expected {
			var last Opencode2Rule
			for _, r := range opencode2Permissions(access) {
				if r.Action == action || r.Action == "*" {
					last = r
				}
			}
			if last != want {
				t.Fatalf("access=%v action=%s last=%+v want=%+v", access, action, last, want)
			}
		}
	}
}

func TestOpencode2CancelRecordingWaitsForRunningSleep(t *testing.T) {
	opts := oc2Opts(t)
	t.Setenv("COCKPIT_OC2_SCENARIO", "cancel-real-process")
	t.Setenv("PACTIFY_RECORD_HOME", t.TempDir())
	opts.Transcript = &golden.Recorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := goldenRun(ctx, golden.Transcript{Harness: "opencode2", Scenario: "cancel", Model: opts.Model, Prompt: "synthetic sleep"}, opts, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Failure != "canceled" {
		t.Fatalf("result=%+v", result)
	}
	// goldenRun requires a live sleep descendant and verifies its birth identity is
	// no longer alive after Interrupt. This test invokes no model or vendor binary.
}

func TestOpencode2ResumeReadonlyExistingChildren(t *testing.T) {
	opts := oc2Opts(t)
	opts.Access = AccessReadOnly
	t.Setenv("COCKPIT_OC2_SCENARIO", "resume-children")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := NewOpencode2Backend().(OptsResumer).ResumeWith(ctx, "ses_root", opts)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	seen := map[string]bool{}
	wantJSON, _ := json.Marshal(Opencode2Permissions(AccessReadOnly))
	var want any
	json.Unmarshal(wantJSON, &want)
	for _, r := range oc2Requests(t) {
		if r.Method == "PATCH" {
			seen[r.Path] = true
			if !reflect.DeepEqual(r.Body["permissions"], want) {
				t.Fatalf("incomplete readonly replacement: %+v", r)
			}
		}
	}
	for _, id := range []string{"ses_root", "ses_child", "ses_grandchild"} {
		if !seen["/api/session/"+id] {
			t.Fatal("missing descendant policy replacement", id)
		}
	}
}

// 2.0.32 sends only ": heartbeat" comments while a tool runs silently; those
// bytes must keep the stream alive (the real 4-minute tool hit the idle limit).
func TestOpencode2HeartbeatKeepsQuietToolAlive(t *testing.T) {
	opts := oc2Opts(t)
	t.Setenv("COCKPIT_OC2_SCENARIO", "heartbeat-quiet")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := NewOpencode2Backend().Start(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	session.(*opencode2Session).inactivityTimeout = 100 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- session.Prompt(ctx, UserMessage{Text: "quiet tool"}) }()
	for e := range session.Events() {
		if e.Kind == EventState && (e.State == StateTurnCompleted || e.State == StateTurnFailed) {
			if e.State != StateTurnCompleted {
				t.Fatalf("quiet tool with heartbeats ended %s: %s", e.State, e.Err)
			}
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOpencode2MinimalEnvKeepsProjectInstructions(t *testing.T) {
	env, err := Opencode2Env(StartOpts{MinimalConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "OPENCODE_DISABLE_PROJECT_CONFIG=") {
			t.Fatalf("%s disables AGENTS.md discovery for jobs", kv)
		}
	}
}

// Only a known provider with no available model suggests logging in; a typo or
// unknown provider must stay a model error.
func TestOpencode2UnavailableModelLoginHint(t *testing.T) {
	for _, tc := range []struct{ model, class string }{
		{"minimax-coding-plan/MiniMax-M3", "auth"},
		{"pactify-e2e-nonexistent/model", "model"},
		{"opencode/missing", "model"},
		{"e2e-nonexistent-model-718", "model"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			opts := oc2Opts(t)
			opts.Model = tc.model
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := NewOpencode2Backend().Start(ctx, opts)
			if err == nil {
				session.Close()
				t.Fatal("unavailable model started")
			}
			if got := FailureFromError("opencode", err, time.Now()).Class; got != tc.class {
				t.Fatalf("class=%s want %s: %v", got, tc.class, err)
			}
			if tc.class == "auth" != strings.Contains(err.Error(), "pactify sub login opencode") {
				t.Fatalf("login hint mismatch: %v", err)
			}
		})
	}
}
