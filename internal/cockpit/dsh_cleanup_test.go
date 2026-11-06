package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"github.com/agentjoey/pactify/internal/procx"
	"github.com/agentjoey/pactify/internal/vendorenv"
)

func TestDSHCredentialEnvironment(t *testing.T) {
	for _, model := range []string{"", "deepseek-v4-flash", "custom/model", "deepseek-official/model"} {
		t.Run(model, func(t *testing.T) {
			dshFixture(t, "ok")
			for _, key := range vendorenv.APIKeys {
				t.Setenv(key, "fixture-"+key)
			}
			keys, _ := json.Marshal(vendorenv.APIKeys)
			t.Setenv("DSH_TEST_ENV_KEYS", string(keys))
			mode := "official"
			if provider, _, ok := strings.Cut(model, "/"); ok {
				mode = "explicit"
				t.Setenv("DSH_TEST_PROVIDER", provider)
			}
			t.Setenv("DSH_TEST_ENV", mode)
			t.Setenv("DEEPSEEK_API_KEY", "fixture-deepseek")
			t.Setenv("DEEPSEEK_BASE_URL", "https://fixture.invalid")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := NewDSHBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Model: model, Access: AccessReadOnly, Env: []string{"OPENAI_API_KEY=fixture-OPENAI_API_KEY"}})
			if err != nil {
				t.Fatal("credential environment rejected by fake:", err)
			}
			s.Close()
		})
	}
}
func TestDSHSiblingCredentialRedaction(t *testing.T) {
	dshFixture(t, "ok")
	t.Setenv("DSH_TEST_SIBLING_LEAK", "1")
	t.Setenv("DSH_TEST_PROVIDER", "custom")
	t.Setenv("ANTHROPIC_API_KEY", "opaque-sibling-value")
	var stderr bytes.Buffer
	recorder := &golden.Recorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := NewDSHBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Model: "custom/model", Access: AccessReadOnly, Stderr: &stderr, Transcript: recorder})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	frames, _ := json.Marshal(recorder.Snapshot())
	if strings.Contains(stderr.String(), "opaque-sibling-value") || strings.Contains(string(frames), "opaque-sibling-value") {
		t.Fatal("sibling credential leaked")
	}
	if !strings.Contains(stderr.String(), golden.Redacted) {
		t.Fatal("missing sanitized stderr")
	}
}
func dshChildSession(t *testing.T, mode string) (*dshSession, procx.Identity) {
	t.Helper()
	t.Setenv("DSH_TEST_CHILD", mode)
	path := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("DSH_TEST_CHILD_PID", path)
	s := dshReviewSession(t, "ok")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	child, err := procx.Inspect(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Signal(syscall.SIGKILL, false) })
	return s, child
}
func TestDSHDetachedPipeCloseBound(t *testing.T) {
	s, child := dshChildSession(t, "detached")
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		_ = child.Signal(syscall.SIGKILL, false)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Close did not recover after fixture cleanup")
		}
		t.Fatal("Close blocked on detached pipe holder")
	}
	select {
	case <-s.reaped:
	default:
		t.Fatal("leader not reaped")
	}
}
func TestDSHGroupDescendants(t *testing.T) {
	for _, action := range []string{"interrupt", "close"} {
		t.Run(action, func(t *testing.T) {
			mode := "group"
			if action == "close" {
				mode = "group-closed"
			}
			s, child := dshChildSession(t, mode)
			if action == "interrupt" {
				if err := s.Interrupt(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				// The old Close needs the child removed after the assertion to avoid hanging.
				go s.Close()
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				alive, err := child.Alive()
				if err != nil {
					t.Fatal(err)
				}
				if !alive {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			_ = child.Signal(syscall.SIGKILL, false)
			t.Fatal("group descendant survived leader exit")
		})
	}
}
func TestDSHLargeStderrDrained(t *testing.T) {
	s := dshReviewSession(t, "large-stderr")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Prompt(ctx, UserMessage{Text: "continue after oversized stderr"}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case e := <-s.Events():
			if e.State == StateTurnCompleted {
				return
			}
			if e.State == StateTurnFailed {
				t.Fatal(e.Err)
			}
		case <-ctx.Done():
			t.Fatal("runtime stalled on oversized stderr")
		}
	}
}

type dshPartialWriter struct {
	*os.File
	writes  atomic.Int32
	pending chan struct{}
}

func (w *dshPartialWriter) Write(p []byte) (int, error) {
	w.writes.Add(1)
	if bytes.Contains(p, []byte("session/prompt")) {
		n, _ := w.File.Write(p[:12])
		return n, io.ErrShortWrite
	}
	n, err := w.File.Write(p)
	if bytes.Contains(p, []byte("test/pending")) {
		close(w.pending)
	}
	return n, err
}
func TestDSHPartialWriteBreaksSession(t *testing.T) {
	s := dshReviewSession(t, "ok")
	leader, err := procx.Inspect(s.cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	writer := &dshPartialWriter{File: s.in.(*os.File), pending: make(chan struct{})}
	s.in = writer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pending := make(chan error, 1)
	go func() { _, err := s.call(ctx, "test/pending", map[string]any{}); pending <- err }()
	<-writer.pending
	if err = s.Prompt(ctx, UserMessage{Text: "partial"}); err != nil {
		t.Fatal(err)
	}
	for {
		e := <-s.Events()
		if e.State == StateTurnFailed {
			break
		}
		if e.State == StateTurnCompleted {
			t.Fatal("partial write completed")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		alive, _ := leader.Alive()
		if !alive {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if alive, _ := leader.Alive(); alive {
		t.Error("runtime survived partial write")
	}
	select {
	case err := <-pending:
		if err == nil {
			t.Error("pending call succeeded")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("pending call did not fail")
	}
	before := writer.writes.Load()
	_, err = s.call(ctx, "session/query", map[string]any{})
	if err == nil || writer.writes.Load() != before {
		t.Error("later call wrote onto broken NDJSON stream")
	}
}
