package sub

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestDoctorDoesNotRepeatIdenticalProbeErrors(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("PACTIFY_HOME", home)
			t.Setenv("PACTIFY_SUB_FAKE", "1")
			refusal := fmt.Errorf("%w: nope/none; available: test/model; … and 160 more; run `opencode models` to list all", cockpit.ErrModelUnavailable)
			h := fakeHarness()
			calls := 0
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{
				model: "nope/none",
				run: func(_ context.Context, _ Store, _ string, _ Harness, _ WorkerConfig) (Result, error) {
					calls++
					if calls == 2 && !same {
						return Result{Status: StatusFailed}, errors.New("different read-only failure")
					}
					// Different wrapping instances, identical diagnostic text.
					return Result{Status: StatusFailed}, fmt.Errorf("acp configure: %w", refusal)
				},
			})
			if err != nil || len(reports) != 1 || reports[0].Passed() || calls != 2 {
				t.Fatalf("reports=%+v calls=%d err=%v", reports, calls, err)
			}
			var write, readonly string
			for _, check := range reports[0].Checks {
				if check.Name == "Write access" {
					write = check.Reason
				} else if check.Name == "Read-only access" {
					readonly = check.Reason
				}
			}
			if !strings.Contains(write, refusal.Error()) {
				t.Fatalf("first error missing: %s", write)
			}
			if same {
				if strings.Contains(readonly, refusal.Error()) || !strings.Contains(readonly, "same as above (Write access)") {
					t.Fatalf("duplicate error: %s", readonly)
				}
			} else if !strings.Contains(readonly, "different read-only failure") || strings.Contains(readonly, "same as above") {
				t.Fatalf("distinct error was hidden: %s", readonly)
			}
		})
	}
}
