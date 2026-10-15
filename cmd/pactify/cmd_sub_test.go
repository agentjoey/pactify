package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func buildPactify(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pactify")
	c := exec.Command("go", "build", "-o", bin, ".")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	return bin
}
func gitRepoForCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "base"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	return dir
}
func runCLI(t *testing.T, bin, dir, stdin string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, bin, args...)
	c.Dir = dir
	c.Stdin = strings.NewReader(stdin)
	var out, stderr bytes.Buffer
	c.Stdout = &out
	c.Stderr = &stderr
	err := c.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI hung: %s %s", out.String(), stderr.String())
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out.String() + stderr.String(), ee.ExitCode()
		}
		t.Fatal(err)
	}
	return out.String(), 0
}
func startSubFake(t *testing.T, bin, repo string) string {
	t.Helper()
	out, code := runCLI(t, bin, repo, "brief text", "sub", "run", "fake", "--write", "--wait", "200ms", "--json")
	if code != 3 {
		t.Fatalf("want exit 3, got %d: %s", code, out)
	}
	var started struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal([]byte(out), &started); err != nil || started.JobID == "" {
		t.Fatalf("bad output: %s", out)
	}
	return started.JobID
}
func TestSubRunWaitTimesOutThenWaitCollects(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("KIMI_SHARE_DIR", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	id := startSubFake(t, bin, repo)
	out, code := runCLI(t, bin, repo, "", "sub", "status", id, "--json")
	if code != 0 || !strings.Contains(out, `"running"`) {
		t.Fatalf("status: %d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "result", id)
	if code != 3 {
		t.Fatalf("premature result: %d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "wait", id, "--json")
	if code != 0 || !strings.Contains(out, `"finalText": "DONE"`) {
		t.Fatalf("wait: %d %s", code, out)
	}
	var r sub.Result
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Status != sub.StatusSucceeded || len(r.Diff.Files) != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if data, err := os.ReadFile(filepath.Join(repo, "out.txt")); err != nil || string(data) != "hi\n" {
		t.Fatalf("out.txt=%q err=%v", data, err)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "result", id)
	if code != 0 || !strings.HasPrefix(out, "✓ fake 完成") {
		t.Fatalf("result: %d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "status", "--json")
	if code != 0 || !strings.Contains(out, id) {
		t.Fatalf("list: %d %s", code, out)
	}
}
func TestSubRunInvalidArgumentsExit2(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	for _, tc := range []struct {
		args  []string
		brief string
	}{{[]string{"nope"}, "x"}, {[]string{"run", "nope", "--write"}, "x"}, {[]string{"run", "fake", "--write", "--read-only"}, "x"}, {[]string{"run", "fake"}, " "}, {[]string{"run", "fake", "--resume", "--fresh"}, "x"}, {[]string{"run", "fake", "--resume"}, "x"}, {[]string{"run", "fake", "--wat"}, "x"}, {[]string{"run", "fake", "--wait", "-1s"}, "x"}} {
		out, code := runCLI(t, bin, repo, tc.brief, append([]string{"sub"}, tc.args...)...)
		if code != 2 {
			t.Errorf("%v: code=%d out=%s", tc.args, code, out)
		}
	}
}
func TestSubCancelStopsWorker(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	id := startSubFake(t, bin, repo)
	out, code := runCLI(t, bin, repo, "", "sub", "cancel", id)
	if code != 0 {
		t.Fatalf("cancel: %d %s", code, out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "wait", id, "--json")
	if code != 1 || !strings.Contains(out, `"canceled"`) {
		t.Fatalf("wait: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(repo, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("canceled worker wrote file: %v", err)
	}
}

// Phase 0 K1: kimi's plan mode never converged on read-only tasks (3/3 hit the
// tool-call limit with no findings), so v1 refuses kimi --read-only up front.
func TestSubRunKimiReadOnlyRefused(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	// Keep the real kimi off PATH: if the refusal ever regresses, the job must fail
	// to start rather than drive a real kimi session from a unit test.
	t.Setenv("PATH", "/usr/bin:/bin")
	out, code := runCLI(t, bin, repo, "review this", "sub", "run", "kimi", "--read-only", "--wait", "0s")
	if code != 2 || !strings.Contains(out, "codex") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}
