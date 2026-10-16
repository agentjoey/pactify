package cockpit

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestAgyReadOnlyRefusedBeforeLaunch(t *testing.T) {
	b := &agyBackend{command: func(context.Context, ...string) *exec.Cmd { t.Fatal("read-only launched a CLI process"); return nil }}
	for _, id := range []string{"", "existing-session"} {
		s, err := b.ResumeWith(context.Background(), id, StartOpts{Access: AccessReadOnly, Model: "model-a"})
		if s != nil {
			s.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "codex --read-only") {
			t.Fatalf("session=%v err=%v", s, err)
		}
	}
}
