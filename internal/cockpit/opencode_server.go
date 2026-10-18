package cockpit

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"github.com/agentjoey/pactify/internal/procx"
)

// opencodeServer owns a single loopback server, its transport and its process tree.
type opencodeServer struct {
	url, password, directory string
	http                     *http.Client
	cmd                      *exec.Cmd
	stdout                   io.ReadCloser
	identity                 procx.Identity
	reaped                   chan struct{}
	mu                       sync.Mutex
	descendants              map[procx.Identity]struct{}
	snapshotErr              error
	closeOnce                sync.Once
	closeErr                 error
	done                     chan struct{}
	stderr                   *secretWriter
	v2                       bool
	stdin                    io.WriteCloser
}

var opencodeListenURL = regexp.MustCompile(`http://127\.0\.0\.1:([0-9]+)`)

func startOpencodeServer(ctx context.Context, opts StartOpts) (*opencodeServer, error) {
	env, err := OpencodeEnv(opts)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	s := &opencodeServer{password: hex.EncodeToString(secret), directory: opts.RepoDir, reaped: make(chan struct{}), done: make(chan struct{}), descendants: map[procx.Identity]struct{}{}}
	transport := &http.Transport{Proxy: nil}
	var roundTrip http.RoundTripper = transport
	if opts.Transcript != nil {
		roundTrip = golden.Transport{Base: transport, Recorder: opts.Transcript}
	}
	s.http = &http.Client{Transport: roundTrip, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	s.cmd = exec.Command("opencode", "serve", "--hostname", "127.0.0.1", "--port", "0")
	s.cmd.Dir = opts.RepoDir
	s.cmd.Env = acp.CommandEnv(append(env, "OPENCODE_SERVER_USERNAME=opencode", "OPENCODE_SERVER_PASSWORD="+s.password))
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	s.stderr = &secretWriter{out: stderr, secret: s.password}
	s.cmd.Stderr = s.stderr
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	s.cmd.WaitDelay = time.Second
	s.stdout, err = s.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = procx.Start(ctx, s.cmd); err != nil {
		s.stdout.Close()
		return nil, fmt.Errorf("opencode serve: %w", err)
	}
	s.identity, err = procx.Inspect(s.cmd.Process.Pid)
	go func() { _ = s.cmd.Wait(); close(s.reaped) }()
	if err != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
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
			if strings.HasPrefix(string(line), "opencode server listening on ") {
				if match := opencodeListenURL.FindString(string(line)); match != "" {
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

func (s *opencodeServer) snapshot() {
	s.mu.Lock()
	defer s.mu.Unlock()
	children, err := procx.Descendants(s.identity)
	s.snapshotErr = errors.Join(s.snapshotErr, err)
	for _, child := range children {
		s.descendants[child] = struct{}{}
	}
}
func (s *opencodeServer) close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.snapshot()
		signalErr := s.identity.Signal(syscall.SIGTERM, true)
		if s.stdin != nil {
			_ = s.stdin.Close()
		}
		waitErr := s.waitReaped(250 * time.Millisecond)
		if waitErr != nil {
			killErr := s.identity.Signal(syscall.SIGKILL, true)
			// os.Process.Kill also works when kernel identity inspection fails;
			// Go's process handle prevents killing an already reaped leader.
			if killErr != nil && s.cmd != nil && s.cmd.Process != nil {
				killErr = errors.Join(killErr, s.cmd.Process.Kill())
			}
			signalErr = errors.Join(signalErr, killErr)
			waitErr = s.waitReaped(time.Second)
		}
		if s.stdout != nil {
			_ = s.stdout.Close()
		}
		if s.stderr != nil {
			s.stderr.flush()
		}
		s.mu.Lock()
		children := make([]procx.Identity, 0, len(s.descendants))
		for child := range s.descendants {
			children = append(children, child)
		}
		snapshotErr := s.snapshotErr
		s.mu.Unlock()
		s.closeErr = errors.Join(signalErr, waitErr, snapshotErr, procx.TerminateDescendants("opencode", children, 250*time.Millisecond))
		s.http.CloseIdleConnections()
	})
	return s.closeErr
}
func (s *opencodeServer) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	endpoint := s.url + path
	if !s.v2 {
		endpoint += "?" + url.Values{"directory": {s.directory}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	if s.v2 {
		req.Header.Set("x-opencode-directory", url.PathEscape(s.directory))
	}
	req.SetBasicAuth("opencode", s.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode %s %s: %w", method, path, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(res.Body, 16384))
		if s.v2 {
			var detail struct {
				Tag     string `json:"_tag"`
				Message string `json:"message"`
			}
			if json.Unmarshal(b, &detail) == nil && detail.Tag != "" {
				b = []byte(detail.Tag + ": " + detail.Message)
			}
		}
		return nil, &opencodeHTTPError{status: res.StatusCode, message: s.redact(fmt.Sprintf("opencode %s %s: HTTP %d: %s", method, path, res.StatusCode, strings.TrimSpace(string(b))))}
	}
	return res, nil
}
func (s *opencodeServer) call(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := s.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		_, err = io.Copy(io.Discard, res.Body)
		return err
	}
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if err = json.Unmarshal([]byte(s.redact(string(b))), out); err != nil {
		return fmt.Errorf("opencode %s: %w", path, err)
	}
	return nil
}
func (s *opencodeServer) models(ctx context.Context) ([]string, error) {
	var catalog struct {
		Providers []struct {
			ID     string                     `json:"id"`
			Models map[string]json.RawMessage `json:"models"`
		} `json:"providers"`
	}
	if err := s.call(ctx, "GET", "/config/providers", nil, &catalog); err != nil {
		return nil, err
	}
	models := []string{}
	for _, provider := range catalog.Providers {
		for id := range provider.Models {
			models = append(models, provider.ID+"/"+id)
		}
	}
	sort.Strings(models)
	if len(models) == 0 {
		return nil, fmt.Errorf("opencode returned no models")
	}
	return models, nil
}
