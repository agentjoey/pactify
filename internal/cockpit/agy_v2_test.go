package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgyV2Helper(t *testing.T) {
	if os.Getenv("AGY_V2_HELPER") != "1" {
		return
	}
	args := strings.Join(os.Args, " ")
	if !strings.Contains(args, "--model ") {
		os.Exit(81)
	}
	if strings.Contains(args, "-- models") {
		fmt.Println("gemini-3.8-flash-low\tFlash\nmodel-a\tA")
		os.Exit(0)
	}
	emit := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	emit(map[string]any{"event": "init", "conversation_id": "v2-conversation", "init": map[string]string{"model": func() string {
		if os.Getenv("AGY_V2_MODE") == "mismatch" {
			return "wrong-model"
		}
		return "gemini-3.8-flash-low"
	}()}})
	if os.Getenv("AGY_V2_MODE") == "no-read" {
		for {
			time.Sleep(time.Hour)
		}
	}
	scan := bufio.NewScanner(os.Stdin)
	for round := 1; scan.Scan(); round++ {
		var v struct {
			Event   string `json:"event"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scan.Bytes(), &v) != nil || v.Event != "user" {
			fmt.Println(`{"event":"result","result":{"status":"ERROR","error":"failed to decode stream input"}}`)
			os.Exit(1)
		}
		mode := os.Getenv("AGY_V2_MODE")
		response := fmt.Sprintf("pid=%d round=%d", os.Getpid(), round)
		status, failure := "SUCCESS", ""
		result := map[string]any{"status": status, "conversation_id": "v2-conversation"}
		if mode == "empty" || mode == "denied" || mode == "timeout" {
			response = ""
		}
		if mode == "denied-once" && round == 1 {
			fmt.Fprintln(os.Stderr, "headless permission auto-denied")
			response = ""
		}
		if mode == "mismatch" {
			status = "ERROR"
			failure = "503 UNAVAILABLE"
		}
		if mode == "denied" {
			result["denied_actions"] = []any{map[string]string{"action": "command"}}
		}
		if mode == "timeout" {
			fmt.Fprintln(os.Stderr, "print timeout reached; returning partial output")
		}
		if mode == "transient" && round == 1 {
			status = "ERROR"
			failure = "backend: EOF"
			response = ""
		}
		if mode == "restart" {
			marker := os.Getenv("AGY_V2_MARKER")
			if _, e := os.Stat(marker); os.IsNotExist(e) {
				_ = os.WriteFile(marker, []byte("started"), 0600)
				os.Exit(1)
			}
			if !strings.Contains(args, "--conversation v2-conversation") {
				os.Exit(82)
			}
		}
		if mode == "always-pending" || mode == "append" && round == 1 {
			response = "I started the tests. I will check the results later."
		}
		if (mode == "active" || mode == "always-active") && round == 1 {
			emit(map[string]any{"event": "step_update", "step_update": map[string]any{"step_index": round * 10, "step_type": "tool", "state": "ACTIVE", "tool_name": "run_command"}})
		}
		if mode == "active" && round == 2 {
			emit(map[string]any{"event": "step_update", "step_update": map[string]any{"step_index": 10, "step_type": "tool", "state": "DONE", "tool_name": "run_command"}})
		}
		if response != "" && mode != "result-only" {
			emit(map[string]any{"event": "step_update", "step_update": map[string]any{"step_index": round*10 + 1, "step_type": "agent_response", "state": "DONE", "text_delta": response, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}}})
		}
		result["status"], result["error"], result["response"] = status, failure, response
		result["usage"] = map[string]int{"input_tokens": 1000 * round, "output_tokens": 200 * round}
		emit(map[string]any{"event": "result", "result": result})
	}
	os.Exit(0)
}

func agyV2Fixture(t *testing.T, mode string) (Backend, StartOpts) {
	t.Helper()
	exe, _ := os.Executable()
	b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, exe, append([]string{"-test.run=^TestAgyV2Helper$", "--"}, args...)...)
	}, grace: 100 * time.Millisecond}
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	return b, StartOpts{RepoDir: repo, Access: AccessWrite, Env: []string{"HOME=" + t.TempDir(), "AGY_V2_HELPER=1", "AGY_V2_MODE=" + mode, "AGY_V2_MARKER=" + t.TempDir() + "/marker"}}
}
func TestAgyV2PersistentAndStepUsage(t *testing.T) {
	b, o := agyV2Fixture(t, "")
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var finals []string
	var usage Usage
	for i := 0; i < 2; i++ {
		for _, e := range agyTurn(t, s) {
			if e.Final {
				finals = append(finals, e.Text)
			}
			if e.Usage != nil {
				usage = *e.Usage
			}
		}
	}
	if len(finals) != 2 || strings.Fields(finals[0])[0] != strings.Fields(finals[1])[0] || !strings.Contains(finals[1], "round=2") {
		t.Fatalf("not one persistent process: %v", finals)
	}
	if usage.InputTokens != 20 || usage.OutputTokens != 4 {
		t.Fatalf("cumulative result usage counted: %+v", usage)
	}
}
func TestAgyV2EmptyFailuresAndFollowups(t *testing.T) {
	for _, mode := range []string{"empty", "denied", "timeout", "append", "active"} {
		t.Run(mode, func(t *testing.T) {
			b, o := agyV2Fixture(t, mode)
			s, err := b.Start(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ev := agyTurn(t, s)
			last := ev[len(ev)-1]
			if mode == "append" || mode == "active" {
				if last.State != StateTurnCompleted {
					t.Fatal(last)
				}
				found := false
				for _, e := range ev {
					if e.Final && strings.Contains(e.Text, "round=2") {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing followup: %+v", ev)
				}
			} else if last.State != StateTurnFailed || ClassifyFailure("agy", last.Err, last.Raw, time.Now()).Class == "other" {
				t.Fatalf("unclassified empty result: %+v", last)
			}
		})
	}
}
func TestAgyV2TransientContinuation(t *testing.T) {
	for _, mode := range []string{"transient", "restart"} {
		t.Run(mode, func(t *testing.T) {
			b, o := agyV2Fixture(t, mode)
			s, err := b.Start(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			first := agyTurn(t, s)
			last := first[len(first)-1]
			if last.State != StateTurnFailed || ClassifyFailure("agy", last.Err, last.Raw, time.Now()).Class != "transient" {
				t.Fatal(last)
			}
			second := agyTurn(t, s)
			if second[len(second)-1].State != StateTurnCompleted {
				t.Fatal(second)
			}
		})
	}
}

func TestAgyV2BadInputLine(t *testing.T) {
	b, o := agyV2Fixture(t, "")
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	agyTurn(t, s)
	session := s.(*agySession)
	if _, err = session.process.stdin.Write([]byte("not-json\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.process.done:
	case <-time.After(3 * time.Second):
		t.Fatal("malformed input did not terminate the process")
	}
	if session.process.err == nil {
		t.Fatal("malformed input was accepted")
	}
}
func TestAgyV2CompletionLimits(t *testing.T) {
	for _, limit := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			b, o := agyV2Fixture(t, "always-pending")
			o.CompletionFollowups = &limit
			s, err := b.Start(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ev := agyTurn(t, s)
			last := ev[len(ev)-1]
			if last.State != StateTurnCompleted {
				t.Fatal(last)
			}
			var final string
			for _, e := range ev {
				if e.Final {
					final = e.Text
				}
			}
			if !strings.Contains(final, "I started the tests.") {
				t.Fatalf("final=%s", final)
			}
		})
	}
}
func TestAgyV2TransientTable(t *testing.T) {
	for _, message := range []string{"EOF", "backend returned 503 UNAVAILABLE", "connection: EOF", "The stream was interrupted. Please continue"} {
		if f := ClassifyFailure("agy", message, nil, time.Now()); f.Class != "transient" {
			t.Errorf("%s: %+v", message, f)
		}
	}
}
