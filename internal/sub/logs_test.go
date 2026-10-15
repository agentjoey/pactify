package sub

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/lockx"
	"github.com/agentjoey/pactify/internal/procx"
)

type lockingLogWriter struct {
	bytes.Buffer
	ctx    context.Context
	lock   string
	locked chan func()
}

func (w *lockingLogWriter) Write(data []byte) (int, error) {
	n, err := w.Buffer.Write(data)
	if w.locked != nil {
		release, lockErr := lockx.Acquire(w.ctx, w.lock)
		if lockErr != nil {
			return n, lockErr
		}
		w.locked <- release
		w.locked = nil
	}
	return n, err
}

// Block Recover after the initial EOF, append final events while holding the
// lifecycle lock, then publish terminal state. Checking terminal before the
// final drain loses these lines deterministically, without a real harness.
func TestLogsDrainsAfterTerminalObservation(t *testing.T) {
	store := Store{Root: t.TempDir()}
	st, err := store.Create("race", Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := procx.Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	err = store.WithState(context.Background(), st.ID, func(current *State) error {
		current.Status, current.Worker, current.PID = StatusRunning, &identity, identity.PID
		st = *current
		return store.Save(*current)
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(store.LogPath(st.ID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := io.WriteString(file, "ready\n"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	locked := make(chan func(), 1)
	out := &lockingLogWriter{ctx: ctx, lock: filepath.Join(store.Dir(st.ID), "state.lock"), locked: locked}
	done := make(chan error, 1)
	go func() { done <- store.Logs(ctx, st.ID, out, LogOptions{Tail: -1, Follow: true}) }()
	var release func()
	select {
	case release = <-locked:
	case err := <-done:
		t.Fatalf("logs exited before initial line: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	// The first drain has reached EOF; the next Recover is waiting on our lock.
	time.Sleep(100 * time.Millisecond)
	if _, err := io.WriteString(file, "{\"kind\":\"message\",\"text\":\"final one\",\"final\":true}\n{\"kind\":\"message\",\"text\":\"final two\",\"final\":true}\n"); err != nil {
		t.Fatal(err)
	}
	result := &Result{JobID: st.ID, Status: StatusSucceeded, Harness: "codex"}
	if err := store.finishLocked(&st, result, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	release()
	release = nil
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if got := out.String(); got != "stderr: ready\nmessage final one\nmessage final two\n" {
		t.Fatalf("lost final events: %q", got)
	}
}

func TestLogsOutputErrorAndContextCancellation(t *testing.T) {
	store := Store{Root: t.TempDir()}
	st, err := store.Create("live", Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.LogPath(st.ID), []byte("line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("output closed")
	if err := store.Logs(context.Background(), st.ID, failingLogWriter{want}, LogOptions{Tail: -1}); !errors.Is(err, want) {
		t.Fatalf("lost writer error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Logs(ctx, st.ID, io.Discard, LogOptions{Tail: 0, Follow: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ignored cancellation: %v", err)
	}
}

type failingLogWriter struct{ err error }

func (w failingLogWriter) Write([]byte) (int, error) { return 0, w.err }

func TestLogFormattingLongUnicodeAndMissingFields(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{`{"kind":"message","text":"hi\r\nthere","timestamp":"2026-10-15T01:02:03Z"}`, "2026-10-15T01:02:03Z message hi"},
		{`{"kind":"tool"}`, "tool"},
		{`{"kind":"system","state":"rate_limits"}`, "system rate_limits"},
		{`{"kind":"plan","text":"step\t1"}`, "plan step 1"},
		{`{"kind":"message","text":"` + strings.Repeat("中", 300) + `"}`, "message " + strings.Repeat("中", 240) + "…"},
	} {
		if got := logLine(tc.line); got != tc.want {
			t.Errorf("format: got %q want %q", got, tc.want)
		}
	}
}
