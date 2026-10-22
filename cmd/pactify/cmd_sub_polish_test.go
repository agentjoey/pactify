package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func runCLIStreams(t *testing.T, bin, dir, stdin string, args ...string) (string, string, int) {
	t.Helper()
	c := exec.Command(bin, args...)
	c.Dir = dir
	c.Stdin = strings.NewReader(stdin)
	var out, stderr bytes.Buffer
	c.Stdout, c.Stderr = &out, &stderr
	err := c.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), stderr.String(), code
}

func TestSubUnknownCommand(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	_, stderr, code := runCLIStreams(t, bin, t.TempDir(), "", "sub", "frobnicate")
	if code != 2 || !strings.Contains(stderr, `unknown command "frobnicate" for "pactify sub"`) || !strings.Contains(stderr, "pactify sub --help") || strings.Contains(stderr, "Did you mean") {
		t.Fatalf("unknown command: %d %s", code, stderr)
	}
	_, stderr, code = runCLIStreams(t, bin, t.TempDir(), "", "sub", "rn")
	if code != 2 || !strings.Contains(stderr, "Did you mean this?\n\trun") {
		t.Fatalf("suggestion: %d %s", code, stderr)
	}
	if out, _, code := runCLIStreams(t, bin, t.TempDir(), "", "sub"); code != 0 || !strings.Contains(out, "Available Commands") {
		t.Fatalf("bare sub: %d %s", code, out)
	}
	if out, _, code := runCLIStreams(t, bin, t.TempDir(), "", "sub", "run", "--help"); code != 0 || strings.Contains(out, "Deny all permission requests") || !strings.Contains(out, "read-only mechanism") {
		t.Fatalf("read-only help: %d %s", code, out)
	}
}

// Exit 2 follows the structured failure class, not the wording of the error.
func TestSubResultModelFailureExitCode(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, class, reason string
		want              int
	}{
		{"model", "model", "model is not supported when using codex with a chatgpt account", 2},
		{"auth", "auth", `codex: requested model "m": unauthorized`, 1},
	} {
		st, err := store.Create(tc.id, sub.Request{Harness: "codex", Access: "read-only"})
		if err != nil {
			t.Fatal(err)
		}
		r := sub.Result{JobID: st.ID, Status: sub.StatusFailed, FailureClass: tc.class}
		if err := store.Finish(st.ID, &r, tc.reason, time.Now()); err != nil {
			t.Fatal(err)
		}
		if out, _, code := runCLIStreams(t, bin, t.TempDir(), "", "sub", "result", st.ID); code != tc.want {
			t.Fatalf("%s: exit %d, want %d: %s", tc.id, code, tc.want, out)
		}
	}
}

func TestSubRunInPlaceWriteNote(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	for _, tc := range []struct {
		args []string
		note bool
	}{
		{[]string{"--write"}, true},
		{nil, true},
		{[]string{"--write", "--worktree"}, false},
		{[]string{"--read-only"}, false},
		{[]string{"--write", "--json"}, false},
	} {
		args := append([]string{"sub", "run", "fake", "--wait", "10s"}, tc.args...)
		out, stderr, code := runCLIStreams(t, bin, repo, "brief", args...)
		if code != 0 {
			t.Fatalf("%v: exit %d: %s %s", tc.args, code, out, stderr)
		}
		if got := strings.Contains(stderr, "in place; use --worktree"); got != tc.note {
			t.Fatalf("%v: note=%v, stderr=%q", tc.args, got, stderr)
		}
		if strings.Contains(out, "in place") {
			t.Fatalf("%v: note on stdout: %s", tc.args, out)
		}
	}
}
