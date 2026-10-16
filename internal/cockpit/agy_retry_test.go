package cockpit

import (
	"context"
	"os/exec"
	"slices"
	"testing"
)

func TestAgyContinuationPinsDefaultModel(t *testing.T) {
	backend, opts := agyFixture(t, "", AccessWrite)
	opts.Model = ""
	b := backend.(*agyBackend)
	command := b.command
	var calls [][]string
	b.command = func(ctx context.Context, args ...string) *exec.Cmd {
		calls = append(calls, slices.Clone(args))
		return command(ctx, args...)
	}
	sess, err := b.Start(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	agyTurn(t, sess)
	agyTurn(t, sess)
	if len(calls) != 2 {
		t.Fatalf("calls=%v", calls)
	}
	for flag, want := range map[string]string{"--conversation": "session-a", "--model": "model-a", "--effort": "low"} {
		i := slices.Index(calls[1], flag)
		if i < 0 || i+1 >= len(calls[1]) || calls[1][i+1] != want {
			t.Errorf("continuation lost %s=%s: %v", flag, want, calls[1])
		}
	}
}
