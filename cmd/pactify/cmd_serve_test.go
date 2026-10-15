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

func TestServeRetiredFlagsHidden(t *testing.T) {
	cmd := newServeCmd()
	for _, name := range []string{"relay-url", "relay-token", "remote-control"} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil || !flag.Hidden {
			t.Errorf("--%s must remain accepted but hidden", name)
		}
	}
}

func TestCloudAccountCommandRemoved(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"account"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("retired account command must be rejected, got %v", err)
	}
}

func TestServeRetiredFlagsIgnored(t *testing.T) {
	bin := buildBinary(t)
	for _, tc := range []struct {
		name string
		args []string
		warn []string
	}{
		{name: "unset"},
		{name: "url", args: []string{"--relay-url", "http://127.0.0.1:1"}, warn: []string{"relay-url"}},
		{name: "token", args: []string{"--relay-token", "test-secret"}, warn: []string{"relay-token"}},
		{name: "control", args: []string{"--remote-control"}, warn: []string{"remote-control"}},
		{name: "launchd", args: []string{"--relay-url=", "--relay-token=test-secret", "--remote-control=false"}, warn: []string{"relay-url", "relay-token", "remote-control"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := listener.Addr().String()
			listener.Close()
			cmd := exec.Command(bin, append([]string{"serve", "--addr", addr}, tc.args...)...)
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "PACTIFY_HOME="+t.TempDir(), "PACT_RELAY_TOKEN=test-env-secret")
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
				resp, err := client.Get("http://" + addr + "/api/projects")
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
			for _, name := range []string{"relay-url", "relay-token", "remote-control"} {
				want := 0
				for _, flagged := range tc.warn {
					if flagged == name {
						want = 1
					}
				}
				warning := "--" + name + " is ignored: the cloud relay was removed; remove it from your launch config"
				if got := strings.Count(string(output), warning); got != want {
					t.Errorf("warning for --%s: got %d, want %d; stderr: %s", name, got, want, output)
				}
			}
			if strings.Contains(string(output), "test-secret") || strings.Contains(string(output), "test-env-secret") || strings.Contains(stdout.String(), "relay") || strings.Contains(stdout.String(), "remote control") {
				t.Errorf("retired configuration must be ignored without leaking tokens: stdout=%s stderr=%s", stdout.String(), output)
			}
		})
	}
}
