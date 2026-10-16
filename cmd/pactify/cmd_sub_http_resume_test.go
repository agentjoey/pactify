package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubHTTPResumeStartsWorkerWithBriefData(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	out, code := runCLI(t, bin, repo, "first", "sub", "run", "fake", "--worktree", "--read-only", "--model", "chosen", "--effort", "high", "--wait", "10s", "--json")
	var old sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &old) != nil {
		t.Fatalf("%d %s", code, out)
	}
	store, _ := sub.DefaultStore()
	t.Cleanup(func() { _ = store.DiscardWorktree(old.JobID) })
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	log, err := os.Create(filepath.Join(t.TempDir(), "serve.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(bin, "serve", "--addr", addr)
	cmd.Dir = repo
	// serve's preflight only sees a disposable home, never real credentials.
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	client := &http.Client{Timeout: 3 * time.Second}
	base := "http://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, e := client.Get(base + "/api/sub/jobs")
		if e == nil {
			_ = res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("serve did not become ready", e)
		}
		time.Sleep(50 * time.Millisecond)
	}
	marker := filepath.Join(t.TempDir(), "executed")
	brief := "$(touch " + marker + "); `touch " + marker + "` & > " + marker + "\n'\"; echo injected"
	body, _ := json.Marshal(map[string]string{"brief": brief})
	started := time.Now()
	res, err := client.Post(base+"/api/sub/jobs/"+old.JobID+"/resume", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	var response struct {
		JobID string `json:"jobId"`
	}
	if res.StatusCode != 200 || json.Unmarshal(data, &response) != nil || response.JobID == "" {
		t.Fatalf("%d %s", res.StatusCode, data)
	}
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatal("resume waited for the worker result")
	}
	out, code = runCLI(t, bin, repo, "", "sub", "wait", response.JobID, "--timeout", "10s", "--json")
	if code != 0 {
		t.Fatalf("%d %s", code, out)
	}
	child, err := store.Load(response.JobID)
	if err != nil || child.Request.Brief != brief || child.Request.Harness != "fake" || child.Request.Model != "chosen" || child.Request.Effort != "high" || child.Request.Access != "read-only" || child.Worktree.Path != old.WorktreePath {
		t.Fatal(child, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("brief executed shell commands", err)
	}
}
