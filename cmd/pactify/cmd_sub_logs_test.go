package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubLogsReadableTailAndRecovery(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	store, st := statusFixture(t, "logs", "codex", time.Now().Add(-time.Minute), nil)
	log := "{\"kind\":\"message\",\"text\":\"hello\\nworld\",\"time\":\"2026-10-15T10:00:00Z\"}\n" +
		"{\"kind\":\"tool\",\"tool\":{\"name\":\"read_file\",\"phase\":\"start\",\"text\":\"a\\nb\"}}\n" +
		"{\"kind\":\"error\",\"err\":\"oops\\nmore\"}\n" +
		"{\"kind\":\"state\",\"state\":\"turn_completed\"}\n" +
		"{\"kind\":\"usage\",\"usage\":{\"inputTokens\":42,\"outputTokens\":7}}\n" +
		"warning from harness\n{\"unrelated\":true}\n"
	if err := os.WriteFile(store.LogPath(st.ID), []byte(log), 0600); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, bin, t.TempDir(), "", "sub", "logs", st.ID)
	if code != 0 {
		t.Fatalf("logs: %d %s", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 7 {
		t.Fatalf("one output per log line: %s", out)
	}
	for i, want := range []string{"2026-10-15T10:00:00Z message hello", "tool read_file", "error oops", "state turn_completed", "usage", "stderr: warning from harness", `stderr: {"unrelated":true}`} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d missing %q: %q", i, want, lines[i])
		}
	}
	recovered, err := store.Load(st.ID)
	if err != nil || recovered.Status != sub.StatusFailed {
		t.Fatalf("logs bypassed recovery: %+v %v", recovered, err)
	}
	out, code = runCLI(t, bin, t.TempDir(), "", "sub", "logs", st.ID, "--tail", "2")
	if code != 0 || out != "stderr: warning from harness\nstderr: {\"unrelated\":true}\n" {
		t.Fatalf("tail: %d %q", code, out)
	}
	out, code = runCLI(t, bin, t.TempDir(), "", "sub", "logs", st.ID, "--tail", "0")
	if code != 0 || out != "" {
		t.Fatalf("tail zero: %d %q", code, out)
	}
	out, code = runCLI(t, bin, t.TempDir(), "", "sub", "logs", st.ID, "--tail", "-1")
	if code != 2 {
		t.Fatalf("negative tail: %d %s", code, out)
	}
}

func TestSubLogsFollowRetainsPartialLines(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	store, st := statusFixture(t, "follow", "codex", time.Now(), nil)
	identity, err := procx.Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithState(context.Background(), st.ID, func(current *sub.State) error {
		current.Status, current.Worker, current.PID = sub.StatusRunning, &identity, identity.PID
		return store.Save(*current)
	}); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(store.LogPath(st.ID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := fmt.Fprint(file, "discard\nready\n{\"kind\":\"message\",\"text\":\"par"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "sub", "logs", st.ID, "--follow", "--tail", "1")
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
	next := func() string {
		t.Helper()
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("logs exited before expected output")
			}
			return line
		case <-ctx.Done():
			t.Fatal("logs follow hung")
		}
		return ""
	}
	if got := next(); got != "stderr: ready" {
		t.Fatalf("tail included partial or old line: %q", got)
	}
	if _, err := fmt.Fprint(file, "tial"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-lines:
		t.Fatalf("emitted incomplete line: %q", line)
	case <-time.After(400 * time.Millisecond):
	}
	if _, err := fmt.Fprint(file, "\",\"final\":true}\n{\"kind\":\"message\",\"text\":\"last\",\"final\":true}\n"); err != nil {
		t.Fatal(err)
	}
	result := sub.Result{JobID: st.ID, Harness: "codex", Status: sub.StatusSucceeded}
	if err := store.Finish(st.ID, &result, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := next(); got != "message partial" {
		t.Errorf("lost partial event: %q", got)
	}
	if got := next(); got != "message last" {
		t.Errorf("lost final event: %q", got)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("follow: %v %s", err, stderr.String())
	}
	for line := range lines {
		t.Errorf("duplicate/extra output: %s", line)
	}
}

func TestSubLogsMissingLogIsEmpty(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	_, st := statusFixture(t, "no-log", "codex", time.Now(), &sub.Result{Status: sub.StatusFailed})
	out, code := runCLI(t, bin, t.TempDir(), "", "sub", "logs", st.ID, "--follow")
	if code != 0 || out != "" {
		t.Fatalf("missing log: %d %q", code, out)
	}
}

func TestSubLogsTerminalUnterminatedStderr(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	store, st := statusFixture(t, "unterminated", "codex", time.Now(), &sub.Result{Status: sub.StatusSucceeded})
	if err := os.WriteFile(store.LogPath(st.ID), []byte("first\nlast warning"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, follow := range []bool{false, true} {
		args := []string{"sub", "logs", st.ID, "--tail", "1"}
		if follow {
			args = append(args, "--follow")
		}
		out, code := runCLI(t, bin, t.TempDir(), "", args...)
		if code != 0 || out != "stderr: last warning\n" {
			t.Errorf("follow=%v: %d %q", follow, code, out)
		}
	}
}
