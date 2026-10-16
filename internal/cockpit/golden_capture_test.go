package cockpit

import (
	"context"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"github.com/agentjoey/pactify/internal/testenv"
)

func TestGoldenCapturesHTTP(t *testing.T) {
	testenv.InstallOpencodeServe(t)
	r := &golden.Recorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := NewOpencodeBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Model: "opencode/test-free", Transcript: r})
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() { ch <- s.Prompt(ctx, UserMessage{Text: "test"}) }()
	for e := range s.Events() {
		if e.State == StateTurnCompleted {
			break
		}
	}
	if err := <-ch; err != nil {
		t.Fatal(err)
	}
	s.Close()
	found := false
	for _, f := range r.Snapshot() {
		if f.Path == "/event" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing raw SSE")
	}
}

// The fixture is the Go test executable; this never starts the installed agy.
func TestGoldenCapturesAgyStdio(t *testing.T) {
	b, opts := agyV2Fixture(t, "")
	recorder := &golden.Recorder{}
	opts.Transcript = recorder
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := b.Start(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	agyTurn(t, s)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	stdin, stdout := false, false
	for _, f := range recorder.Snapshot() {
		stdin = stdin || f.Channel == "stdin"
		stdout = stdout || f.Channel == "stdout"
	}
	if !stdin || !stdout {
		t.Fatalf("stdio recording incomplete: stdin=%t stdout=%t", stdin, stdout)
	}
}
