package main

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSubPanelCommandAndInterrupt(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "sub", "panel", "--no-open", "--port", "0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scan := bufio.NewScanner(out)
	if !scan.Scan() {
		t.Fatal("no panel URL", scan.Err())
	}
	u, err := url.Parse(scan.Text())
	if err != nil || u.Hostname() != "127.0.0.1" || len(u.Query().Get("token")) < 40 {
		t.Fatalf("invalid URL %q %v", scan.Text(), err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(u.Scheme + "://" + u.Host + "/api/jobs")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.StatusCode)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
func TestSubPanelFlags(t *testing.T) {
	command := newSubCmd()
	panel, _, err := command.Find([]string{"panel"})
	if err != nil || panel.Name() != "panel" {
		t.Fatalf("panel missing: %v", err)
	}
	if f := panel.Flags().Lookup("port"); f == nil || f.DefValue != "0" {
		t.Fatal("random port missing")
	}
	if f := panel.Flags().Lookup("no-open"); f == nil {
		t.Fatal("no-open missing")
	}
	for _, port := range []string{"-1", "65536"} {
		c := newSubCmd()
		c.SetArgs([]string{"panel", "--port", port, "--no-open"})
		if err := c.Execute(); err == nil || !strings.Contains(err.Error(), "port") {
			t.Fatalf("invalid port %s: %v", port, err)
		}
	}
}

func TestSubPanelBrowserReceivesURLWithoutShellExpansion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX helper")
	}
	dir := t.TempDir()
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	record := filepath.Join(dir, "browser-url")
	t.Setenv("PACTIFY_PANEL_TEST_URL", record)
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$PACTIFY_PANEL_TEST_URL\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	address := "http://127.0.0.1:1234/?token=test&literal=1"
	if err := openSubPanel(context.Background(), address); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record)
	if err != nil || string(data) != address {
		t.Fatalf("browser arg: %q %v", data, err)
	}
}
