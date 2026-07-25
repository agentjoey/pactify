package orchestrate

import (
	"context"
	"errors"
	"testing"
)

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		delivered bool
		want      FailClass
	}{
		{"timeout with no delivery is env", context.DeadlineExceeded, false, FailEnv},
		{"idle kill with no delivery is env", errIdle, false, FailEnv},
		{"generic failure with no delivery is env", errors.New("exit status 1"), false, FailEnv},
		{"spawn failure is env", errors.New(`acp spawn "claude-code": exec: "claude-agent-acp": executable file not found in $PATH`), false, FailEnv},
		{"timeout AFTER delivering is logic", context.DeadlineExceeded, true, FailLogic},
		{"generic failure after delivering is logic", errors.New("exit status 1"), true, FailLogic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyFailure(tc.err, tc.delivered); got != tc.want {
				t.Fatalf("classifyFailure(%v, delivered=%v) = %q, want %q", tc.err, tc.delivered, got, tc.want)
			}
		})
	}
}

// A nil error is not a failure at all — classification must not be asked, but
// if it is, logic is the safe answer (never propose swapping an agent that
// did not fail).
func TestClassifyNilErrorIsLogic(t *testing.T) {
	if got := classifyFailure(nil, false); got != FailLogic {
		t.Fatalf("nil error classified %q, want logic", got)
	}
}
