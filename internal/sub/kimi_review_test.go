package sub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// Derived from the review's TestZZSubAgentSatisfiesGuard: only the child's
// accounting has flushed. Main must remain partial until its own record arrives.
func TestKimiReviewChildCannotCompleteMain(t *testing.T) {
	_, s, _, _, _, home := kimiJobFixture(t, false)
	writeKimiJobRecord(t, home, s.ThreadID(), "")
	main := filepath.Join(home, "sessions", s.ThreadID(), "agents/main/wire.jsonl")
	if err := os.WriteFile(main, []byte("{\"type\":\"llm.request\",\"model\":\"m\"}\n{\"type\":\"turn.ended\",\"reason\":\"completed\"}\n{\"type\":\"prompt.completed\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(home, "sessions", s.ThreadID(), "agents/agent-0")
	os.MkdirAll(child, 0700)
	os.WriteFile(filepath.Join(child, "wire.jsonl"), []byte(kimiCurrentUsage), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	v := verifyKimiUsage(ctx, VerificationInput{SessionID: s.ThreadID(), Info: cockpit.SessionInfo{Home: home}, Prompts: 1})
	if !v.UsagePartial {
		t.Fatalf("child completed main: %+v", v)
	}
	appendKimiUsageFixture(t, main, kimiCurrentUsage)
	v = verifyKimiUsage(context.Background(), VerificationInput{SessionID: s.ThreadID(), Info: cockpit.SessionInfo{Home: home}, Prompts: 1})
	if v.UsagePartial || v.InputTokens != 246 || v.OutputTokens != 14 {
		t.Fatalf("main+child=%+v", v)
	}
}

func TestKimiReviewBadBaselineReturnsImmediately(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	v := verifyKimiUsage(ctx, VerificationInput{SessionID: "missing", Info: cockpit.SessionInfo{Home: t.TempDir()}, WireOffset: -1, Prompts: 1})
	if !v.UsagePartial || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("baseline waited: %+v elapsed=%s", v, time.Since(started))
	}
}

func TestKimiReviewReadOnlyRejectedBeforeStart(t *testing.T) {
	store, _, b, h, _, _ := kimiJobFixture(t, false)
	h.Verify = nil
	st, _ := store.Load("j1")
	st.Request.Access = "read-only"
	store.Save(st)
	r, err := RunJob(context.Background(), store, st.ID, h, WorkerConfig{})
	if err == nil || r.Status != StatusFailed || b.gotOpts.RepoDir != "" {
		t.Fatalf("read-only exposed workspace: %+v %v", r, err)
	}
}

func TestKimiReviewCatalogDoesNotBlockOtherHarness(t *testing.T) {
	c := newHarnessCatalog()
	c.lookupPath = func(string) (string, error) { return "fake", nil }
	store := Store{Root: t.TempDir()}
	entered := make(chan struct{})
	release := make(chan struct{})
	slow := Harness{Name: "slow", Models: func(context.Context, cockpit.StartOpts) ([]string, error) {
		close(entered)
		<-release
		return []string{"slow"}, nil
	}}
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Wait()
	// Close release before waiting if an assertion fails.
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	go func() { defer wg.Done(); c.harness(context.Background(), store, slow, HarnessDefault{}, false) }()
	<-entered
	done := make(chan HarnessInfo, 1)
	go func() {
		done <- c.harness(context.Background(), store, Harness{Name: "fast", Models: func(context.Context, cockpit.StartOpts) ([]string, error) { return []string{"fast"}, nil }}, HarnessDefault{}, false)
	}()
	select {
	case row := <-done:
		if row.Models.Error != "" {
			t.Fatal(row)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("slow external discovery held the catalog lock")
	}
}

func TestKimiReviewWirePreservesACPPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"auth", json.RawMessage(`{"code":-32000,"data":{"sentinel":"keep"}}`), "auth"},
		{"refusal", json.RawMessage(`{"stopReason":"refusal"}`), "refusal"},
		{"max-tokens", json.RawMessage(`{"stopReason":"max_tokens"}`), "max_tokens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, s, _, h, _, home := kimiJobFixture(t, false)
			s.script = func(s *fakeSession) {
				writeKimiJobRecord(t, home, s.ThreadID(), "{\"type\":\"turn.ended\",\"reason\":\"failed\",\"error\":{\"code\":\"provider.connection_error\",\"message\":\"lost\"}}\n")
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "ACP failure", Raw: tc.raw}
			}
			limit := 0
			st, _ := store.Load("j1")
			st.Request.Retries = &limit
			store.Save(st)
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err != nil || r.FailureClass != tc.want {
				t.Fatalf("%+v %v", r, err)
			}
			log, e := os.ReadFile(store.LogPath("j1"))
			if e != nil || !bytes.Contains(log, tc.raw) || !bytes.Contains(log, []byte(`"state":"kimi_wire_failure"`)) || !bytes.Contains(log, []byte("provider.connection_error")) {
				t.Fatalf("failure evidence lost: %s %v", log, e)
			}

		})
	}
}

func TestKimiReviewRPCFailureReadsWireBeforeInterrupt(t *testing.T) {
	store, s, _, h, _, home := kimiJobFixture(t, false)
	attempts := 0
	h.Backend = func() cockpit.Backend {
		return lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
			return &lifecycleSession{fakeSession: s, prompt: func(context.Context) error {
				attempts++
				if attempts == 1 {
					writeKimiJobRecord(t, home, s.ThreadID(), "{\"type\":\"turn.ended\",\"reason\":\"failed\",\"error\":{\"code\":\"provider.connection_error\",\"message\":\"lost\"}}\n")
					return errors.New("ACP internal failure")
				}
				writeKimiJobRecord(t, home, s.ThreadID(), "{\"type\":\"turn.ended\",\"reason\":\"completed\"}\n")
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				return nil
			}, interrupt: func() error {
				t.Error("interrupted before wire classification")
				return s.Interrupt(context.Background())
			}}, nil
		}}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return 0 }})
	if err != nil || r.Status != StatusSucceeded || len(r.Retries) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestKimiReviewDiscoveryCleanupAndStderr(t *testing.T) {
	source := fakeKimiUserHome(t)
	os.WriteFile(filepath.Join(source, "config.toml"), []byte("[models.offline]\n"), 0600)
	bin := t.TempDir()
	script := `#!/usr/bin/env python3
import json,sys
sys.stderr.write('private diagnostic sentinel\n');sys.stderr.flush()
for line in sys.stdin:
 q=json.loads(line)
 if q['method']=='initialize': r={'protocolVersion':1,'agentCapabilities':{}}
 elif q['method']=='session/new': r={'sessionId':'hidden','configOptions':[{'id':'model','currentValue':'live','options':[{'value':'live'}]}]}
 else: sys.exit(9)
 print(json.dumps({'jsonrpc':'2.0','id':q['id'],'result':r}),flush=True)
`
	os.WriteFile(filepath.Join(bin, "kimi"), []byte(script), 0700)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	var stderr bytes.Buffer
	got, err := kimiACPModelsWithCleanup(context.Background(), cockpit.StartOpts{Env: []string{"KIMI_CODE_HOME=" + source}, Stderr: &stderr}, func(dir string) error {
		if e := os.RemoveAll(dir); e != nil {
			t.Fatal(e)
		}
		return errors.New("cleanup sentinel")
	})
	if err != nil || len(got.Items) != 1 || got.Items[0] != "live" || !strings.Contains(got.Warning, "cleanup") {
		t.Errorf("lost successful catalog: %+v %v", got, err)
	}
	if stderr.Len() != 0 {
		t.Errorf("discovery leaked stderr: %q", stderr.String())
	}
}
