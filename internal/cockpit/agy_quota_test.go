package cockpit

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAgyQuotaShape(t *testing.T) {
	b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd {
		argv := strings.Join(args, " ")
		if !strings.Contains(argv, "--model gemini-3.8-flash-low") || !strings.Contains(argv, "-p /usage") {
			t.Fatal(argv)
		}
		return exec.CommandContext(ctx, "printf", "%s", `{"status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"gemini-weekly","window":"weekly","remaining_fraction":0.75,"reset_time":"2026-10-23T00:40:01Z"}]}]}}}`)
	}}
	q, err := b.Quota(context.Background(), StartOpts{})
	if err != nil || len(q.Buckets) != 1 || q.Buckets[0].RemainingFraction != .75 || q.Buckets[0].ResetAt.IsZero() {
		t.Fatalf("quota=%+v err=%v", q, err)
	}
	if !strings.Contains(q.Summary(), "75% remaining") {
		t.Fatal(q.Summary())
	}
	f := ClassifyFailure("agy", "Individual quota reached. Resets in 2h.", nil, time.Now())
	if f.Class != "quota" || f.ResetAt == nil {
		t.Fatal(f)
	}
}
