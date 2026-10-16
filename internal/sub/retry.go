package sub

import (
	"context"
	"encoding/json"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

const DefaultRetries = 2
const MaxRetries = 5
const retryPrompt = "The previous turn was interrupted by a transient backend error. Continue the task from where you left off; do not redo completed steps."

func retryBackoff(attempt int) time.Duration {
	if attempt == 1 {
		return 30 * time.Second
	}
	return 120 * time.Second
}

func (r *Result) classifyTurnFailure(harness string, c Collector, now time.Time, promptFailed bool) {
	if !promptFailed {
		r.failureMessage = c.FailureMessage
	}
	raw := c.FailureRaw
	if harness == "codex" && cockpit.ClassifyFailure(harness, "", c.RateLimit, now).Class == "quota" {
		raw = c.RateLimit
	}
	if harness == "kimi" && len(c.WireRaw) > 0 {
		raw, _ = json.Marshal(map[string]json.RawMessage{"acp": c.FailureRaw, "acpError": c.ErrorRaw, "wire": c.WireRaw})
	}
	f := cockpit.ClassifyFailure(harness, c.FailureMessage, raw, now)
	if f.Class == "other" && len(c.ErrorRaw) > 0 && len(c.WireRaw) == 0 {
		f = cockpit.ClassifyFailure(harness, c.FailureMessage, c.ErrorRaw, now)
	}
	if f.Class != "other" || r.FailureClass == "" || len(c.WireRaw) > 0 {
		r.FailureClass, r.QuotaResetAt = f.Class, f.ResetAt
	}
}

func (s Store) retryProgress(ctx context.Context, id string, attempt, limit int, retry *Retry, now time.Time) error {
	return s.WithState(ctx, id, func(st *State) error {
		if ctx.Err() != nil || st.CancelRequested || Terminal(st.Status) {
			return context.Canceled
		}
		st.RetryAttempt, st.RetryLimit = attempt, limit
		if retry != nil {
			st.Retries = append(st.Retries, *retry)
		}
		st.UpdatedAt = now.UTC()
		return s.Save(*st)
	})
}

// Poll the durable cancellation flag even when no CLI signal reaches this worker.
func (s Store) waitRetry(ctx context.Context, id string, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-poll.C:
			st, err := s.Load(id)
			if err != nil {
				return err
			}
			if st.CancelRequested {
				return context.Canceled
			}
		}
	}
}
