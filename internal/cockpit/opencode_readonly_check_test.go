package cockpit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/testenv"
)

func TestOpencodeLastAction(t *testing.T) {
	rules := []opencodePermission{{"*", "*", "deny"}, {"read", "*", "allow"}, {"bash", "git *", "allow"}, {"bash", "*>*", "deny"}}
	for _, c := range []struct{ permission, pattern, want string }{
		{"read", "/x", "allow"},
		{"edit", "x", "deny"},
		{"bash", "git status", "allow"},
		{"bash", "git log > out", "deny"},
		{"bash", "ls", "deny"},
	} {
		if got := opencodeLastAction(rules, c.permission, c.pattern); got != c.want {
			t.Errorf("%s %q = %q, want %q", c.permission, c.pattern, got, c.want)
		}
	}
	if got := opencodeLastAction(nil, "bash", "ls"); got != "" {
		t.Errorf("no rules = %q, want no decision", got)
	}
}

// AJ-734: the doctor reads the persisted 1.x session's rules back. A write
// session (bash ask) must not pass as read-only.
func TestCheckOpencodeReadonlyReadsSessionRules(t *testing.T) {
	for _, access := range []Access{AccessReadOnly, AccessWrite} {
		t.Run(string(access), func(t *testing.T) {
			testenv.InstallOpencodeServe(t)
			t.Setenv("COCKPIT_OC_SCENARIO", "readonly")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			repo := t.TempDir()
			s, err := NewACPBackend("opencode").Start(ctx, StartOpts{RepoDir: repo, Model: "opencode/test-free", Access: access, MinimalConfig: true})
			if err != nil {
				t.Fatal(err)
			}
			id := s.ThreadID()
			_ = s.Close()
			err = CheckOpencodeReadonly(ctx, StartOpts{RepoDir: repo, Access: AccessReadOnly, MinimalConfig: true}, id)
			if access == AccessReadOnly && err != nil {
				t.Fatalf("read-only session rejected: %v", err)
			}
			if access == AccessWrite && (err == nil || !strings.Contains(err.Error(), `bash permission is "ask"`)) {
				t.Fatalf("write session accepted as read-only: %v", err)
			}
		})
	}
}
