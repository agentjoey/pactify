package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
)

type authRPCError struct{}

func (authRPCError) Error() string { return "opaque failure" }
func (authRPCError) RPCCode() int  { return -32000 }

func TestStructuredFailuresTakePrecedence(t *testing.T) {
	for _, tc := range []struct {
		h, raw, msg, class string
		reset              int64
	}{
		{"codex", `{"rateLimits":{"primary":{"usedPercent":100,"resetsAt":2000000000},"secondary":{"usedPercent":101,"resetsAt":2000000100}}}`, "Unauthorized", "quota", 2000000100},
		{"codex", `{"rateLimits":{"primary":{"usedPercent":99}}}`, "opaque", "other", 0},
		{"codex", `{"turn":{"error":{"codexErrorInfo":"unauthorized"}}}`, "rate limit", "auth", 0},
		{"codex", `{"error":{"codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":503}}}}`, "rate limit", "transient", 0},
		{"codex", `{"error":{"codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":401}}}}`, "opaque", "auth", 0},
		{"codex", `{"error":{"codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":429}}}}`, "opaque", "quota", 0},
		{"codex", `{"error":{"codexErrorInfo":"usageLimitExceeded"}}`, "opaque", "quota", 0},
		{"codex", `{"error":{"codexErrorInfo":"internalServerError"}}`, "opaque", "transient", 0},
		{"codex", `{"rateLimits":{"primary":{"usedPercent":100,"resets_at":2000000000}}}`, "opaque", "quota", 2000000000},
		{"codex", `{"rateLimits":{"primary":{"usedPercent":100},"secondary":{"usedPercent":100,"resetsAt":2000000000}}}`, "opaque", "quota", 0},
		{"kimi", `{"code":-32000}`, "Insufficient Balance", "auth", 0},
		{"opencode", `{"code":-32603}`, "Internal error: Insufficient Balance", "quota", 0},
		{"opencode", `{"code":-32603}`, "Internal error", "other", 0},
		{"agy", ``, "Individual quota reached. Resets in nonsense", "quota", 0},
	} {
		t.Run(tc.h+tc.raw+tc.msg, func(t *testing.T) {
			got := ClassifyFailure(tc.h, tc.msg, json.RawMessage(tc.raw), time.Now())
			if got.Class != tc.class {
				t.Fatalf("got=%+v want %s", got, tc.class)
			}
			if tc.reset == 0 && got.ResetAt != nil || tc.reset != 0 && (got.ResetAt == nil || got.ResetAt.Unix() != tc.reset) {
				t.Fatalf("reset=%+v want %d", got, tc.reset)
			}
		})
	}
	if got := FailureFromError("kimi", fmt.Errorf("wrapped: %w", authRPCError{}), time.Now()); got.Class != "auth" {
		t.Fatal(got)
	}
	if got := FailureFromError("opencode", fmt.Errorf("wrapped: %w", ErrModelUnavailable), time.Now()); got.Class != "model" {
		t.Fatal(got)
	}
}

func TestACPPreservesStructuredFailureCode(t *testing.T) {
	s := newACPSessionForTest(func(context.Context, string) (acp.StopReason, error) { return "", authRPCError{} })
	if err := s.Prompt(context.Background(), UserMessage{Text: "go"}); err == nil {
		t.Fatal("missing failure")
	}
	event := <-s.Events()
	if got := ClassifyFailure("kimi", event.Err, event.Raw, time.Now()); got.Class != "auth" {
		t.Fatalf("event=%+v class=%+v", event, got)
	}
	s.Close()
}
