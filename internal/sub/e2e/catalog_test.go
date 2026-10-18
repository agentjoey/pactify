//go:build harness_e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/agentjoey/pactify/internal/cockpit"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"syscall"
	"time"
)

// Discovery never sends a prompt: v1 retains its live provider fixture, while
// v2 uses the selected product discovery API and the fixture's private home.
func liveOpencodeModels(f *fixture) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if f.s.adapter.Opencode2 {
		env, err := opencodeFixtureEnv(f.s.adapter, f.env)
		if err != nil {
			return nil, err
		}
		models, err := f.s.adapter.Models(ctx, cockpit.StartOpts{Env: env, RepoDir: f.repo, MinimalConfig: true})
		if err != nil {
			return nil, err
		}
		available := map[string]bool{}
		for _, model := range models {
			available[model] = true
		}
		return available, nil
	}
	password := randomWord() + randomWord() + randomWord()
	env := setenv(f.env, "OPENCODE_SERVER_PASSWORD", password)
	env = append(env, "OPENCODE_SERVER_USERNAME=opencode", "OPENCODE_PURE=1", "OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_CLAUDE_CODE=1", "OPENCODE_DISABLE_EXTERNAL_SKILLS=1")
	cmd := exec.CommandContext(ctx, "opencode", "serve", "--hostname", "127.0.0.1", "--port", "0")
	cmd.Env = env
	cmd.Dir = f.repo
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()
	addresses := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		pattern := regexp.MustCompile(`http://127\.0\.0\.1:[0-9]+`)
		for scanner.Scan() {
			if address := pattern.FindString(scanner.Text()); address != "" {
				select {
				case addresses <- address:
				default:
				}
			}
		}
	}()
	var address string
	select {
	case address = <-addresses:
	case <-ctx.Done():
		return nil, fmt.Errorf("live provider server did not start within 30s")
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "GET", address+"/config/providers", nil)
	if e != nil {
		return nil, e
	}
	req.SetBasicAuth("opencode", password)
	response, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("live provider discovery failed: %w", e)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("live provider discovery HTTP %d", response.StatusCode)
	}
	var catalog struct {
		Providers []struct {
			ID     string                     `json:"id"`
			Models map[string]json.RawMessage `json:"models"`
		} `json:"providers"`
	}
	if e = json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&catalog); e != nil {
		return nil, fmt.Errorf("invalid live provider catalog")
	}
	available := map[string]bool{}
	for _, provider := range catalog.Providers {
		for id := range provider.Models {
			available[provider.ID+"/"+id] = true
		}
	}
	return available, nil
}
