package cockpit

import (
	"context"
	"strings"
	"testing"
)

func TestOpencodeReadOnlyRefusedBeforeSpawn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/nonexistent")
	t.Setenv("OPENCODE_CONFIG_CONTENT", "not-json")
	opts := StartOpts{Access: AccessReadOnly, Model: "test/model"}
	for _, resume := range []bool{false, true} {
		b := NewACPBackend("opencode")
		var s Session
		var err error
		if resume {
			s, err = b.(OptsResumer).ResumeWith(context.Background(), "existing", opts)
		} else {
			s, err = b.Start(context.Background(), opts)
		}
		if s != nil {
			s.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "Read-only access is not offered for opencode") || !strings.Contains(err.Error(), "codex --read-only") {
			t.Fatalf("resume=%t session=%v err=%v", resume, s, err)
		}
	}
	if _, err := OpencodeEnv(opts); err == nil || !strings.Contains(err.Error(), "codex --read-only") {
		t.Fatalf("read-only env accepted: %v", err)
	}
}
