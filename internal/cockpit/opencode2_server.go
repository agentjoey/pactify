package cockpit

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"github.com/agentjoey/pactify/internal/lockx"
	"github.com/agentjoey/pactify/internal/procx"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Opencode2Lease serializes private-home startup/migrations and interactive login.
// Ready servers release it: initialized SQLite supports concurrent session writes.
func Opencode2Lease(ctx context.Context, env []string) (func(), error) {
	data := os.Getenv("XDG_DATA_HOME")
	for _, entry := range env {
		if v, ok := strings.CutPrefix(entry, "XDG_DATA_HOME="); ok {
			data = v
		}
	}
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		data = filepath.Join(home, ".local/share")
	}
	dir := filepath.Join(data, "opencode")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	wait, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	release, err := lockx.Acquire(wait, filepath.Join(dir, ".pactify-v2.lock"))
	if err != nil {
		return nil, fmt.Errorf("opencode 2.x startup lock unavailable; another startup or pactify sub login opencode may be using the private home; retry after it finishes: %w", err)
	}
	return release, nil
}

func startOpencode2Server(ctx context.Context, opts StartOpts) (*opencodeServer, error) {
	env, err := Opencode2Env(opts)
	if err != nil {
		return nil, err
	}
	if opts.MinimalConfig {
		release, err := Opencode2Lease(ctx, env)
		if err != nil {
			return nil, err
		}
		defer release() // All startup exits, including ready, failure and cancellation.
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	s := &opencodeServer{password: hex.EncodeToString(secret), v2: true, directory: opts.RepoDir, reaped: make(chan struct{}), done: make(chan struct{}), descendants: map[procx.Identity]struct{}{}}
	transport := &http.Transport{Proxy: nil}
	var roundTrip http.RoundTripper = transport
	if opts.Transcript != nil {
		roundTrip = golden.Transport{Base: transport, Recorder: opts.Transcript}
	}
	s.http = &http.Client{Transport: roundTrip, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	s.cmd = exec.Command("opencode", "serve", "--stdio", "--hostname", "127.0.0.1", "--port", "0")
	s.cmd.Dir = opts.RepoDir
	s.cmd.Env = acp.CommandEnv(append(env, "OPENCODE_PASSWORD="+s.password))
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	s.stderr = &secretWriter{out: stderr, secret: s.password}
	s.cmd.Stderr = s.stderr
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	s.cmd.WaitDelay = time.Second
	s.stdin, err = s.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	s.stdout, err = s.cmd.StdoutPipe()
	if err != nil {
		_ = s.stdin.Close()
		return nil, err
	}
	if err = procx.Start(ctx, s.cmd); err != nil {
		_ = s.stdin.Close()
		s.stdout.Close()
		return nil, fmt.Errorf("opencode serve: %w", err)
	}
	s.identity, err = procx.Inspect(s.cmd.Process.Pid)
	go func() { _ = s.cmd.Wait(); close(s.reaped) }()
	if err != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		_ = s.stdin.Close()
		s.stdout.Close()
		_ = s.waitReaped(time.Second)
		return nil, fmt.Errorf("opencode server identity: %w", err)
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = s.close()
		case <-s.done:
		}
	}()
	ready := make(chan string, 1)
	go func() {
		scan := bufio.NewReader(s.stdout)
		for {
			line, err := scan.ReadSlice('\n')
			var announcement struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(line, &announcement) == nil {
				if match := opencodeListenURL.FindString(announcement.URL); match != "" && match == announcement.URL {
					select {
					case ready <- match:
					default:
					}
				}
			}
			if err != nil && err != bufio.ErrBufferFull {
				break
			}
		}
	}()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case s.url = <-ready:
		return s, nil
	case <-ctx.Done():
		_ = s.close()
		return nil, ctx.Err()
	case <-s.reaped:
		_ = s.close()
		return nil, fmt.Errorf("opencode serve exited before announcing its listening address")
	case <-timer.C:
		_ = s.close()
		return nil, fmt.Errorf("opencode serve startup timed out")
	}
}

// call2 unwraps v2's data envelope; no credential API is used.
func (s *opencodeServer) call2(ctx context.Context, method, path string, body, out any) error {
	if out == nil {
		return s.call(ctx, method, path, body, nil)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := s.call(ctx, method, path, body, &envelope); err != nil {
		return err
	}
	if len(envelope.Data) == 0 && path == "/api/model/default" {
		envelope.Data = json.RawMessage("null")
	}
	if len(envelope.Data) == 0 {
		return fmt.Errorf("opencode %s: missing data envelope", path)
	}
	return json.Unmarshal(envelope.Data, out)
}
