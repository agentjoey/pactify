package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubLogsMessageBlocksAndRaw(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	for _, tc := range []struct {
		name, events, want string
		flags              []string
	}{
		{"codex-final", `{"kind":"message","text":"代"}
{"kind":"message","text":"理"}
{"kind":"message","text":"，所有"}
{"kind":"message","text":"代理，所有","final":true}
`, "message 代理，所有\n", nil},
		{"raw", `{"kind":"message","text":"代"}
{"kind":"message","text":"理"}
{"kind":"message","text":"代理","final":true}
`, "message 代\nmessage 理\nmessage 代理\n", []string{"--raw"}},
		{"unfinished", `{"kind":"message","text":"没有"}
{"kind":"message","text":"完整消息"}
`, "message 没有完整消息\n", nil},
		{"empty-final", `{"kind":"message","text":"retained"}
{"kind":"message","text":" text"}
{"kind":"message","final":true}
`, "message retained text\n", nil},
		{"acp-blocks", `{"kind":"message","text":"before "}
{"kind":"message","text":"tool"}
{"kind":"tool","tool":{"phase":"start","name":"read"}}
{"kind":"tool","tool":{"phase":"output","name":"read"}}
{"kind":"tool","tool":{"phase":"end","name":"read"}}
{"kind":"message","text":"after "}
{"kind":"message","text":"tool"}
{"kind":"state","state":"turn_completed"}
`, "message before tool\ntool read\nmessage after tool\nstate turn_completed\n", nil},
		{"tail-blocks", `{"kind":"message","text":"first","final":true}
{"kind":"message","text":"sec"}
{"kind":"message","text":"ond"}
{"kind":"message","text":"second","final":true}
{"kind":"message","text":"trailing "}
{"kind":"message","text":"block"}
`, "message second\nmessage trailing block\n", []string{"--tail", "2"}},
		{"final-then-unfinished", `{"kind":"message","text":"first","final":true}
{"kind":"message","text":"new "}
{"kind":"message","text":"block"}
`, "message first\nmessage new block\n", nil},
		{"usage-between-fragments", `{"kind":"message","text":"com"}
{"kind":"usage","usage":{"inputTokens":1,"outputTokens":2}}
{"kind":"message","text":"plete"}
{"kind":"message","text":"complete","final":true}
`, "usage in 1 / out 2\nmessage complete\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, st := statusFixture(t, tc.name, "codex", time.Now(), &sub.Result{Status: sub.StatusSucceeded})
			if err := os.WriteFile(store.LogPath(st.ID), []byte(tc.events), 0600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"sub", "logs", st.ID}, tc.flags...)
			out, code := runCLI(t, bin, t.TempDir(), "", args...)
			if code != 0 || out != tc.want {
				t.Fatalf("exit %d: got %q want %q", code, out, tc.want)
			}
		})
	}
}

func TestSubLogsToolStartAndMeaningfulCompletion(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	for _, tc := range []struct {
		name, events, want string
		flags              []string
	}{
		{"duplicate-end", `{"kind":"tool","tool":{"phase":"start","name":"shell","text":"ls"}}
{"kind":"tool","tool":{"phase":"end","name":"shell","text":"ls"}}
`, "tool shell ls\n", nil},
		{"raw-end", `{"kind":"tool","tool":{"phase":"start","name":"ls"}}
{"kind":"tool","tool":{"phase":"end","name":"ls"}}
`, "tool ls\ntool ls\n", []string{"--raw"}},
		{"summary", `{"kind":"tool","tool":{"phase":"start","name":"shell","text":"ls"}}
{"kind":"tool","tool":{"phase":"end","name":"shell","text":"3 files"}}
`, "tool shell ls\ntool shell 3 files\n", nil},
		{"error", `{"kind":"tool","tool":{"phase":"start","name":"ls"}}
{"kind":"tool","tool":{"phase":"end","name":"ls"},"err":"permission denied"}
`, "tool ls\ntool ls permission denied\n", nil},
		{"codex-completion", `{"kind":"tool","tool":{"phase":"start","name":"ls"},"raw":{"threadId":"t","item":{"type":"commandExecution","id":"cmd","command":"ls","status":"inProgress"}}}
{"kind":"tool","tool":{"phase":"end","name":"ls"},"raw":{"threadId":"t","item":{"type":"commandExecution","id":"cmd","command":"ls","status":"failed","exitCode":2,"aggregatedOutput":"permission denied"}}}
`, "tool ls\ntool ls exit 2: permission denied\n", nil},
		{"tool-output-block", `{"kind":"tool","tool":{"phase":"start","name":"ls"},"raw":{"threadId":"t","item":{"id":"cmd"}}}
{"kind":"tool","tool":{"phase":"output","text":"3 "},"raw":{"threadId":"t","itemId":"cmd"}}
{"kind":"tool","tool":{"phase":"output","text":"files"},"raw":{"threadId":"t","itemId":"cmd"}}
{"kind":"tool","tool":{"phase":"end","name":"ls"},"raw":{"threadId":"t","item":{"id":"cmd"}}}
`, "tool ls\ntool ls 3 files\n", nil},
		{"acp-failed", `{"kind":"tool","tool":{"phase":"start","name":"Read"},"raw":{"sessionUpdate":"tool_call","toolCallId":"c","title":"Read"}}
{"kind":"tool","tool":{"phase":"output","name":"Read"},"raw":{"sessionUpdate":"tool_call_update","toolCallId":"c","status":"failed","content":[{"type":"content","content":{"type":"text","text":"not found"}}]}}
`, "tool Read\ntool Read failed: not found\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, st := statusFixture(t, tc.name, "codex", time.Now(), &sub.Result{Status: sub.StatusSucceeded})
			if err := os.WriteFile(store.LogPath(st.ID), []byte(tc.events), 0600); err != nil {
				t.Fatal(err)
			}
			out, code := runCLI(t, bin, t.TempDir(), "", append([]string{"sub", "logs", st.ID}, tc.flags...)...)
			if code != 0 || out != tc.want {
				t.Fatalf("exit %d: got %q want %q", code, out, tc.want)
			}
		})
	}
}

func TestSubLogsFollowEmitsBlocksBeforeJobFinishes(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw=%v", raw), func(t *testing.T) {
			id := fmt.Sprintf("follow-blocks-%v", raw)
			store, st := statusFixture(t, id, "codex", time.Now(), nil)
			identity, err := procx.Inspect(os.Getpid())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.WithState(context.Background(), id, func(current *sub.State) error {
				current.Status, current.Worker, current.PID = sub.StatusRunning, &identity, identity.PID
				return store.Save(*current)
			}); err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(store.LogPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			// The initial pending fragment must survive snapshot tail selection.
			if _, err := fmt.Fprint(file, "{\"kind\":\"message\",\"text\":\"live \"}\nready\n"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			args := []string{"sub", "logs", id, "--follow", "--tail", "1"}
			if raw {
				args = append(args, "--raw")
			}
			cmd := exec.CommandContext(ctx, bin, args...)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			lines := make(chan string, 16)
			go func() {
				defer close(lines)
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					lines <- scanner.Text()
				}
			}()
			next := func(want string) {
				t.Helper()
				select {
				case got, ok := <-lines:
					if !ok || got != want {
						t.Fatalf("got %q want %q (stderr %s)", got, want, stderr.String())
					}
				case <-ctx.Done():
					t.Fatalf("message delayed until job exit: %v", ctx.Err())
				}
			}
			// The snapshot marker synchronizes with the real follower.
			next("stderr: ready")
			if _, err := fmt.Fprint(file, "{\"kind\":\"message\",\"text\":\"message\"}\n"); err != nil {
				t.Fatal(err)
			}
			if raw {
				next("message message")
			} else {
				select {
				case got := <-lines:
					t.Fatalf("fragment escaped: %q", got)
				case <-time.After(200 * time.Millisecond):
				}
			}
			if _, err := fmt.Fprint(file, "{\"kind\":\"message\",\"text\":\"live message\",\"final\":true}\n"); err != nil {
				t.Fatal(err)
			}
			next("message live message")
			// ACP chunks must flush at tool start while the job is still running.
			if _, err := fmt.Fprint(file, "{\"kind\":\"message\",\"text\":\"ACP \"}\n{\"kind\":\"message\",\"text\":\"block\"}\n{\"kind\":\"tool\",\"tool\":{\"phase\":\"start\",\"name\":\"read\"}}\n"); err != nil {
				t.Fatal(err)
			}
			if raw {
				next("message ACP ")
				next("message block")
			} else {
				next("message ACP block")
			}
			next("tool read")
			if _, err := fmt.Fprint(file, "{\"kind\":\"message\",\"text\":\"unfin"); err != nil {
				t.Fatal(err)
			}
			time.Sleep(150 * time.Millisecond)
			if _, err := fmt.Fprint(file, "ished\"}\n"); err != nil {
				t.Fatal(err)
			}
			if raw {
				next("message unfinished")
			}
			result := sub.Result{JobID: id, Status: sub.StatusSucceeded, Harness: "codex"}
			if err := store.Finish(id, &result, "", time.Now()); err != nil {
				t.Fatal(err)
			}
			if !raw {
				next("message unfinished")
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("follow: %v %s", err, stderr.String())
			}
			for line := range lines {
				t.Errorf("extra output: %s", line)
			}
			// Confirm this test did not accidentally rely on recovery terminating the job.
			data, err := os.ReadFile(store.ResultPath(st.ID))
			var completed sub.Result
			if err != nil || json.Unmarshal(data, &completed) != nil || completed.Status != sub.StatusSucceeded {
				t.Fatalf("job was not completed by test: %s %v", data, err)
			}
		})
	}
}
