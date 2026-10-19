package cockpit

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestAgyModelsCommands(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		plainFails, legacyFails bool
	}{
		{name: "plain"}, {name: "legacy", plainFails: true}, {name: "both fail", plainFails: true, legacyFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls [][]string
			b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd {
				calls = append(calls, append([]string{}, args...))
				plain := len(args) == 1 && args[0] == "models"
				if (plain && tc.plainFails) || (!plain && tc.legacyFails) {
					// Exercise bounded stderr and retain the plain command's diagnostic.
					return exec.CommandContext(ctx, "sh", "-c", "printf '%s' \"$1\" >&2; exit 7", "fake-agy", "  plain discovery unavailable "+strings.Repeat("x", 250)+"  ")
				}
				return exec.CommandContext(ctx, "sh", "-c", "printf 'spinner on stderr\\r' >&2; printf '\\033[?25l\\033[32mone\\033[0m\\tFirst\\r\\ntwo\\tSecond\\rprogress\\rthree\\tThird\\n'")
			}}
			models, err := b.models(context.Background(), StartOpts{RepoDir: t.TempDir()})
			expected := [][]string{{"models"}}
			if tc.plainFails {
				expected = append(expected, []string{"models", "--model", AgyDefaultModel})
			}
			if !reflect.DeepEqual(calls, expected) {
				t.Fatalf("commands: %v; want %v", calls, expected)
			}
			if tc.legacyFails {
				if err == nil || !strings.Contains(err.Error(), "agy models: exit status 7: plain discovery unavailable") || !strings.Contains(err.Error(), "legacy fallback also failed") || len(err.Error()) > 300 {
					t.Fatalf("error: %v", err)
				}
			} else if err != nil || !reflect.DeepEqual(models, []string{"one", "two", "three"}) {
				t.Fatalf("models %v, error %v", models, err)
			}
		})
	}
}
