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
	for i := 0; i < 2; i++ {
		ev := agyTurn(t, sess)
		if ev[len(ev)-1].State != StateTurnCompleted {
			t.Fatal(ev)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("calls=%v", calls)
	}
	for flag, want := range map[string]string{"--model": AgyDefaultModel, "--effort": "low"} {
		i := slices.Index(calls[0], flag)
		if i < 0 || i+1 >= len(calls[0]) || calls[0][i+1] != want {
			t.Errorf("continuation lost %s=%s: %v", flag, want, calls[0])
		}
	}
}
