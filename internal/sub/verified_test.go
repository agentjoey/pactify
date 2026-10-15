package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestRunJobUnverifiedVersion(t *testing.T) {
	for _, tc := range []struct {
		name, table string
		warn        bool
	}{
		{"missing", "", true}, {"corrupt", "{", true}, {"null", "null", true},
		{"other-version", `{"codex":{"version":"old","verifiedAt":"2026-10-15T00:00:00Z"}}`, true},
		{"other-harness", `{"kimi":{"version":"9.9.9","verifiedAt":"2026-10-15T00:00:00Z"}}`, true},
		{"verified", `{"codex":{"version":"9.9.9","verifiedAt":"2026-10-15T00:00:00Z"}}`, false},
		{"missing-time", `{"codex":{"version":"9.9.9"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			store.Root = filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")
			_, err := store.Create("j1", Request{Harness: "codex", Access: "write", Cwd: gitRepo(t), Brief: "task"})
			if err != nil {
				t.Fatal(err)
			}
			if tc.table != "" {
				if err := os.WriteFile(filepath.Join(os.Getenv("PACTIFY_HOME"), "verified-versions.json"), []byte(tc.table), 0600); err != nil {
					t.Fatal(err)
				}
			}
			s.script = func(s *fakeSession) {
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			h := newFakeHarness(b)
			h.Name = "codex"
			h.Experimental = "experimental test warning"
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			want := "codex 9.9.9 尚未自检，建议先跑 pactify sub doctor"
			if contains(r.Warnings, want) != tc.warn {
				t.Fatalf("warnings=%v want warning=%v", r.Warnings, tc.warn)
			}
			st, err := store.Load("j1")
			if err != nil || st.Error != "" || r.Status != StatusSucceeded || !strings.Contains(r.Markdown(), h.Experimental) {
				t.Fatalf("result=%+v state=%+v err=%v", r, st, err)
			}
			if !contains(b.gotOpts.Env, "PACTIFY_HOME="+filepath.Dir(store.Root)) {
				t.Fatalf("job home not isolated: %v", b.gotOpts.Env)
			}
		})
	}
}

func TestRunJobUnverifiedVersionKeepsRealError(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	h := newFakeHarness(b)
	h.Name = "codex"
	h.Experimental = "experimental"
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "real failure"}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	st, _ := store.Load("j1")
	if err != nil || !contains(r.Warnings, "尚未自检") || st.Error != "real failure" || st.Status != StatusFailed {
		t.Fatalf("result=%+v state=%+v err=%v", r, st, err)
	}
}
