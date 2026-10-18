package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeRetiredFlagsRejected(t *testing.T) {
	for _, name := range []string{"relay-url", "relay-token", "remote-control", "seat", "project"} {
		t.Run(name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs([]string{"serve", "--" + name, "x"})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown flag: --"+name) {
				t.Fatalf("retired flag must be rejected, got %v", err)
			}
		})
	}
}

func TestServeStartsAndStops(t *testing.T) {
	bin := buildBinary(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	cmd := exec.Command(bin, "serve", "--addr", addr)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "PACTIFY_HOME="+t.TempDir())
	stderr, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	client := &http.Client{Timeout: 200 * time.Millisecond}
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		resp, err := client.Get("http://" + addr + "/api/sub/jobs")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			ready = resp.StatusCode == http.StatusOK
			if ready {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-done
		t.Fatal("serve did not stop")
	}
	output, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatalf("serve never became ready: %s", output)
	}
	if !strings.Contains(stdout.String(), "pactify serve on http://"+addr) {
		t.Errorf("missing listening line: %s", stdout.String())
	}
	if len(output) != 0 {
		t.Errorf("unexpected startup diagnostics: %s", output)
	}
}
