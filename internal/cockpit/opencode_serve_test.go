package cockpit

import (
	"context"
	"fmt"
	"github.com/agentjoey/pactify/internal/procx"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/testenv"
)

func TestOpencodeServePeer(t *testing.T) { testenv.OpencodeServePeer(t) }
func TestOpencodeServeV2(t *testing.T) {
	for _, scenario := range []string{"permission", "readonly", "quota", "unknown", "unknown-no-id", "transient"} {
		t.Run(scenario, func(t *testing.T) {
			data := testenv.InstallOpencodeServe(t)
			t.Setenv("COCKPIT_OC_SCENARIO", scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			access := AccessWrite
			if scenario == "readonly" {
				access = AccessReadOnly
			}
			opts := StartOpts{RepoDir: t.TempDir(), Model: "opencode/test-free", Access: access, MinimalConfig: true}
			s, err := NewACPBackend("opencode").Start(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			go func() {
				for a := range s.Approvals() {
					d := DecisionAllow
					if access == AccessReadOnly {
						d = DecisionDeny
					}
					a.Respond(d)
				}
			}()
			ch := make(chan error, 1)
			go func() { ch <- s.Prompt(ctx, UserMessage{Text: "test"}) }()
			var usage Usage
			var text string
			var terminal Event
			permission := false
		loop:
			for {
				select {
				case e := <-s.Events():
					if e.Usage != nil {
						usage = *e.Usage
					}
					if e.Final {
						text = e.Text
					}
					if strings.Contains(string(e.Raw), "per-1") {
						permission = true
					}
					if e.State == StateTurnCompleted || e.State == StateTurnFailed {
						terminal = e
						break loop
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if err := <-ch; err != nil {
				t.Fatal(err)
			}
			if usage.InputTokens != 122 || usage.OutputTokens != 10 || usage.CostUSD != .25 {
				t.Errorf("usage must use summary including cache writes: %+v", usage)
			}
			want := StateTurnCompleted
			class := ""
			switch scenario {
			case "quota":
				want = StateTurnFailed
				class = "quota"
			case "transient":
				want = StateTurnFailed
				class = "transient"
			case "unknown", "unknown-no-id":
				want = StateTurnFailed
				class = "other"
			}
			if terminal.State != want {
				t.Fatalf("terminal=%+v", terminal)
			}
			if class != "" {
				if got := ClassifyFailure("opencode", terminal.Err, terminal.Raw, time.Now()); got.Class != class {
					t.Errorf("failure=%+v event=%+v", got, terminal)
				}
			} else if text != "DONE" {
				t.Errorf("text=%q", text)
			}
			if (scenario == "unknown" || scenario == "unknown-no-id") && text != "" {
				t.Errorf("user prompt leaked into final: %q", text)
			}
			if (scenario == "unknown" || scenario == "unknown-no-id") && !strings.Contains(terminal.Err, "Model not found") {
				t.Error(terminal.Err)
			}
			if scenario == "permission" || scenario == "readonly" {
				b, err := os.ReadFile(filepath.Join(data, "reply"))
				want := "once"
				if scenario == "readonly" {
					want = "reject"
				}
				if err != nil || string(b) != want || !permission {
					t.Errorf("reply=%q recorded=%v err=%v", b, permission, err)
				}
			}
		})
	}
}
func TestOpencodeServeRestartV2(t *testing.T) {
	testenv.InstallOpencodeServe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := StartOpts{RepoDir: t.TempDir(), Model: "opencode/test-free", Access: AccessWrite, MinimalConfig: true}
	b := NewACPBackend("opencode")
	for round := 0; round < 2; round++ {
		var s Session
		var err error
		if round == 0 {
			s, err = b.Start(ctx, opts)
		} else {
			s, err = b.(OptsResumer).ResumeWith(ctx, "sid", opts)
		}
		if err != nil {
			t.Fatal(err)
		}
		ch := make(chan error, 1)
		go func() { ch <- s.Prompt(ctx, UserMessage{Text: "continue"}) }()
		var usage Usage
		for e := range s.Events() {
			if e.Usage != nil {
				usage = *e.Usage
			}
			if e.State == StateTurnCompleted {
				break
			}
		}
		if err := <-ch; err != nil {
			t.Fatal(err)
		}
		if usage.InputTokens != 122 || usage.OutputTokens != 10 || usage.CostUSD != .25 {
			t.Errorf("round=%d usage=%+v", round, usage)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestOpencodeServeModelsV2(t *testing.T) {
	testenv.InstallOpencodeServe(t)
	got, err := OpencodeModels(context.Background(), StartOpts{RepoDir: t.TempDir(), MinimalConfig: true})
	if err != nil || strings.Join(got, ",") != "minimax-coding-plan/MiniMax-M3,opencode/paid,opencode/test-free" {
		t.Fatalf("models=%v err=%v", got, err)
	}
}

func TestOpencodeServeAbortV2(t *testing.T) {
	data := testenv.InstallOpencodeServe(t)
	t.Setenv("COCKPIT_OC_SCENARIO", "cancel")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := NewOpencodeBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Model: "opencode/test-free", Access: AccessWrite, MinimalConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	go func() { _ = s.Prompt(ctx, UserMessage{Text: "sleep"}) }()
	childPath := filepath.Join(data, "child")
	var child []byte
	for {
		child, err = os.ReadFile(childPath)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	var pid int
	fmt.Sscanf(string(child), "%d", &pid)
	identity, err := procx.Inspect(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "aborted")); err != nil {
		t.Fatal("v1 abort was not called", err)
	}
	if alive, err := identity.Alive(); err != nil || alive {
		t.Fatalf("tool survived: alive=%v err=%v", alive, err)
	}
	server := s.(*opencodeSession).server
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if alive, err := server.identity.Alive(); err != nil || alive {
		t.Fatalf("server survived: alive=%v err=%v", alive, err)
	}
}
