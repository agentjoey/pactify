package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgyProcessHelper(t *testing.T) {
	if os.Getenv("PACTIFY_AGY_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	args = args[1:]
	if len(args) > 0 && args[0] == "models" {
		if os.Getenv("PACTIFY_AGY_MODE") == "models-fail" {
			os.Exit(9)
		}
		fmt.Println("model-a\tModel A\nmodel-b\tModel B")
		os.Exit(0)
	}
	has := func(flag string) bool {
		for _, arg := range args {
			if arg == flag {
				return true
			}
		}
		return false
	}
	if !has("--input-format") || !has("--output-format") || has("--sandbox") || has("SECRET-PROMPT") {
		os.Exit(12)
	}
	if !has("--dangerously-skip-permissions") {
		os.Exit(14)
	}
	scan := bufio.NewScanner(os.Stdin)
	if !scan.Scan() {
		os.Exit(15)
	}
	var input struct {
		Event   string `json:"event"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(scan.Bytes(), &input) != nil || input.Event != "user" || input.Message.Content != "SECRET-PROMPT" {
		os.Exit(16)
	}
	mode := os.Getenv("PACTIFY_AGY_MODE")
	if mode != "no-init" {
		fmt.Println(`{"event":"init","conversation_id":"session-a","init":{"model":"model-a"}}`)
	}
	if mode == "descendant" {
		signal.Ignore(os.Interrupt)
		child := exec.Command("sh", "-c", "sleep 3")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if child.Start() != nil {
			os.Exit(24)
		}
		select {}
	}
	if mode == "flood" {
		signal.Ignore(os.Interrupt)
		for i := 0; i < 2000; i++ {
			fmt.Println(`{"event":"step_update","step_update":{"step_type":"agent_response","text_delta":"x"}}`)
		}
		select {}
	}

	if mode == "wait" || mode == "kill" {
		if mode == "kill" {
			signal.Ignore(os.Interrupt)
			select {}
		} else {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, os.Interrupt)
			<-ch
			fmt.Println(`{"event":"result","result":{"status":"ERROR","error":"interrupted"}}`)
			os.Exit(1)
		}
	}
	if mode == "resume" && !has("--conversation") {
		os.Exit(17)
	}
	if mode == "narration" {
		fmt.Println(`{"event":"step_update","step_update":{"step_index":0,"step_type":"agent_response","state":"ACTIVE","text_delta":"Running the tests first."}}`)
		fmt.Println(`{"event":"step_update","step_update":{"step_index":0,"step_type":"agent_response","state":"DONE","text_delta":"\n"}}`)
	}
	fmt.Println(`{"event":"step_update","step_update":{"step_index":1,"step_type":"tool","state":"ACTIVE","tool_name":"run_command","tool_info":{"parameters":{"command":"pwd"}}}}`)
	fmt.Println(`{"event":"step_update","step_update":{"step_index":1,"step_type":"tool","state":"ACTIVE","tool_name":"run_command"}}`)
	fmt.Println(`{"event":"step_update","step_update":{"step_index":1,"step_type":"tool","state":"DONE","tool_name":"run_command","tool_info":{"output":"ok"}}}`)
	fmt.Println(`{"event":"step_update","step_update":{"step_index":2,"step_type":"agent_response","state":"DONE","text_delta":"answer","usage":{"input_tokens":10,"cache_read_tokens":90,"output_tokens":3,"total_tokens":103}}}`)
	switch mode {
	case "denied":
		fmt.Fprintln(os.Stderr, `jetski: no output produced — a tool required the "run_command" permission that headless mode cannot prompt for, so it was auto-denied`)
		fmt.Println(`{"event":"result","result":{"status":"SUCCESS","response":""}}`)
	case "narration":
		// agy 1.3.5 concatenates every agent_response step into result.response.
		fmt.Println(`{"event":"result","result":{"conversation_id":"session-a","status":"SUCCESS","response":"Running the tests first.\nanswer"}}`)
	case "missing":
	case "invalid":
		fmt.Println("not-json")
	case "error":
		fmt.Println(`{"event":"result","result":{"status":"ERROR","error":"bad model"}}`)
		os.Exit(1)
	case "exit":
		fmt.Println(`{"event":"result","result":{"status":"SUCCESS","response":"answer"}}`)
		os.Exit(1)
	default:
		fmt.Println(`{"event":"result","result":{"conversation_id":"session-a","status":"SUCCESS","response":"answer","usage":{"input_tokens":20,"cache_read_tokens":180,"output_tokens":7,"thinking_tokens":4,"total_tokens":27}}}`)
	}
	os.Exit(0)
}

func agyFixture(t *testing.T, mode string, access Access) (Backend, StartOpts) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, exe, append([]string{"-test.run=^TestAgyProcessHelper$", "--"}, args...)...)
	}, grace: 100 * time.Millisecond}
	o := StartOpts{RepoDir: t.TempDir(), Model: "model-a", Effort: "low", Access: access, Env: []string{"HOME=" + t.TempDir(), "PACTIFY_AGY_HELPER=1", "PACTIFY_AGY_MODE=" + mode}}
	return b, o
}
func agyTurn(t *testing.T, s Session) []Event {
	t.Helper()
	if err := s.Prompt(context.Background(), UserMessage{Text: "SECRET-PROMPT"}); err != nil {
		t.Fatal(err)
	}
	var out []Event
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-s.Events():
			out = append(out, e)
			if e.State == StateTurnCompleted || e.State == StateTurnFailed {
				return out
			}
		case <-timer.C:
			t.Fatal("turn timed out")
		}
	}
}
func TestAgyStreamResumeAndUsage(t *testing.T) {
	for _, access := range []Access{AccessWrite} {
		t.Run(string(access), func(t *testing.T) {
			b, o := agyFixture(t, "", access)
			s, err := b.Start(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			events := agyTurn(t, s)
			starts, ends := 0, 0
			var usage Usage
			final := ""
			for _, e := range events {
				if e.Tool != nil {
					if e.Tool.Phase == "start" {
						starts++
					}
					if e.Tool.Phase == "end" {
						ends++
					}
				}
				if e.Usage != nil {
					usage = *e.Usage
				}
				if e.Final {
					final = e.Text
				}
			}
			if starts != 1 || ends != 1 || usage.InputTokens != 200 || usage.OutputTokens != 7 || usage.TotalTokens != 207 || final != "answer" || events[len(events)-1].State != StateTurnCompleted || s.ThreadID() != "session-a" || s.(InfoSession).Info().Model != "model-a" {
				t.Fatalf("events=%+v usage=%+v id=%s", events, usage, s.ThreadID())
			}
			// The second prompt and an explicit resume both use the learned conversation id.
			s.(*agySession).opts.Env = append(s.(*agySession).opts.Env, "PACTIFY_AGY_MODE=resume")
			events = agyTurn(t, s)
			if events[len(events)-1].State != StateTurnCompleted {
				t.Fatal(events)
			}
			o.Env = append(o.Env, "PACTIFY_AGY_MODE=resume")
			resumed, err := b.(OptsResumer).ResumeWith(context.Background(), s.ThreadID(), o)
			if err != nil {
				t.Fatal(err)
			}
			defer resumed.Close()
			events = agyTurn(t, resumed)
			if events[len(events)-1].State != StateTurnCompleted {
				t.Fatal(events)
			}
		})
	}
}
func TestAgyFailuresAndModels(t *testing.T) {
	b, o := agyFixture(t, "", AccessWrite)
	o.Model = "unknown"
	if _, err := b.Start(context.Background(), o); err == nil || !strings.Contains(err.Error(), "model-a") || !strings.Contains(err.Error(), "model-b") {
		t.Fatal("missing available models", err)
	}
	for _, mode := range []string{"denied", "missing", "invalid", "error", "exit", "no-init"} {
		t.Run(mode, func(t *testing.T) {
			b, o := agyFixture(t, mode, AccessWrite)
			s, err := b.Start(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			events := agyTurn(t, s)
			last := events[len(events)-1]
			if last.State != StateTurnFailed || last.Err == "" {
				t.Fatal(events)
			}
			if mode == "denied" && !strings.Contains(last.Err, "auto-denied") {
				t.Fatal(last)
			}
		})
	}
}
func TestAgyCancellationAndClose(t *testing.T) {
	for _, mode := range []string{"wait", "kill"} {
		t.Run(mode, func(t *testing.T) {
			b, o := agyFixture(t, mode, AccessWrite)
			s, err := b.Start(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Prompt(context.Background(), UserMessage{Text: "SECRET-PROMPT"}); err != nil {
				t.Fatal(err)
			}
			timer := time.NewTimer(10 * time.Second)
			defer timer.Stop()
			for s.ThreadID() == "" {
				select {
				case <-s.Events():
				case <-timer.C:
					t.Fatal("no init")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.Interrupt(ctx); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAgyInterruptWithUndrainedEvents(t *testing.T) {
	b, o := agyFixture(t, "flood", AccessWrite)
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Prompt(context.Background(), UserMessage{Text: "SECRET-PROMPT"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("no init")
	}
	// Stop consuming while the producer fills the event buffer.
	time.Sleep(100 * time.Millisecond)
	interrupted := make(chan error, 1)
	go func() { interrupted <- s.Interrupt(context.Background()) }()
	select {
	case err := <-interrupted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Interrupt blocked on undrained progress events")
	}
}

func TestAgyInterruptWithInheritedPipes(t *testing.T) {
	b, o := agyFixture(t, "descendant", AccessWrite)
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Prompt(context.Background(), UserMessage{Text: "SECRET-PROMPT"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("no init")
	}
	time.Sleep(100 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- s.Interrupt(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Interrupt blocked on inherited child pipes")
	}
}

func TestAgyCockpitWrapperCloses(t *testing.T) {
	b, o := agyFixture(t, "", AccessWrite)
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Approvals() == nil {
		t.Fatal("Session must return a channel that Close can close; the cockpit approval pump ranges over it")
	}
	wrapped, err := NewCockpitSession(s, filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- wrapped.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cockpit wrapper close blocked")
	}
}

// The final reply is the last agent_response step, not agy's result.response,
// which also carries narration from earlier steps of the turn.
func TestAgyFinalReplyIsLastResponseStep(t *testing.T) {
	b, o := agyFixture(t, "narration", AccessWrite)
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	final := ""
	for _, e := range agyTurn(t, s) {
		if e.Final {
			final = e.Text
		}
	}
	if final != "answer" {
		t.Fatalf("final reply = %q, want only the last response step", final)
	}
}

// A failed 'agy models' listing (network fetch, killed by its timeout under
// load) must not fail the job: agy and init.model still verify the model.
func TestAgyStartSurvivesFailedModelListing(t *testing.T) {
	b, o := agyFixture(t, "models-fail", AccessWrite)
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatalf("start failed on a model-listing error: %v", err)
	}
	defer s.Close()
	events := agyTurn(t, s)
	if events[len(events)-1].State != StateTurnCompleted || s.(InfoSession).Info().Model != "model-a" {
		t.Fatalf("events=%+v", events)
	}
}
