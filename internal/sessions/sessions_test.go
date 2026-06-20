package sessions

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeKimiSession creates a fake kimi session dir root/<hash>/<uuid>/state.json
// with the given custom_title.
func writeKimiSession(t *testing.T, root, hash, uuid, title string) string {
	t.Helper()
	dir := filepath.Join(root, hash, uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"),
		[]byte(`{"version":1,"custom_title":`+strconv.Quote(title)+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCleanupKimiSeat_DeletesOnlyMatchingSeatPactSessions(t *testing.T) {
	root := t.TempDir()
	mine := writeKimiSession(t, root, "h1", "u1", "# pact worker — seat `kimi-worker` (roles: worker)")
	other := writeKimiSession(t, root, "h1", "u2", "# pact reviewer — seat `claude`")
	user := writeKimiSession(t, root, "h2", "u3", "fix the login bug")

	deleted, err := CleanupKimiSeat(root, "kimi-worker")
	if err != nil {
		t.Fatalf("CleanupKimiSeat: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "u1" {
		t.Fatalf("deleted = %v, want [u1]", deleted)
	}
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Error("the seat's pact session was not removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("a different seat's session was wrongly removed")
	}
	if _, err := os.Stat(user); err != nil {
		t.Error("a non-pact user session was wrongly removed")
	}
}

func TestSupported(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"gemini-cli", true},
		{"opencode", true},
		{"nope", false},
	}
	for _, tt := range tests {
		got := Supported(tt.kind)
		if got != tt.want {
			t.Errorf("Supported(%q) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestCanPrune(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"gemini-cli", true}, // list + --delete-session by index → bulk prune
		{"opencode", false},
		{"nope", false},
	}
	for _, tt := range tests {
		got := CanPrune(tt.kind)
		if got != tt.want {
			t.Errorf("CanPrune(%q) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

type fakeRun struct {
	calls []struct {
		name string
		args []string
	}
	out string
	err error
}

func (f *fakeRun) Run(_ /*dir*/, name string, args ...string) (string, error) {
	f.calls = append(f.calls, struct {
		name string
		args []string
	}{name, args})
	return f.out, f.err
}

func TestList(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		fake    *fakeRun
		wantOut string
		wantErr bool
		wantMsg string
	}{
		{
			name:    "gemini-cli lists with --list-sessions",
			kind:    "gemini-cli",
			fake:    &fakeRun{out: "session-1\nsession-2"},
			wantOut: "session-1\nsession-2",
			wantErr: false,
		},
		{
			name:    "opencode lists with session list",
			kind:    "opencode",
			fake:    &fakeRun{out: "Session ID  Title\nses_1  pact:dev"},
			wantOut: "Session ID  Title\nses_1  pact:dev",
			wantErr: false,
		},
		{
			name:    "nope unsupported",
			kind:    "nope",
			fake:    &fakeRun{},
			wantErr: true,
			wantMsg: `sessions: listing not supported for kind "nope"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Manager{Run: tt.fake.Run}
			got, err := m.List(tt.kind)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err.Error() != tt.wantMsg {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantOut {
				t.Errorf("output = %q, want %q", got, tt.wantOut)
			}
			if tt.kind == "gemini-cli" {
				if len(tt.fake.calls) != 1 {
					t.Fatalf("expected 1 Run call, got %d", len(tt.fake.calls))
				}
				call := tt.fake.calls[0]
				if call.name != "gemini" {
					t.Errorf("Run name = %q, want %q", call.name, "gemini")
				}
				wantArgs := []string{"--list-sessions"}
				if !equalSlice(call.args, wantArgs) {
					t.Errorf("Run args = %v, want %v", call.args, wantArgs)
				}
			}
			if tt.kind == "opencode" {
				if len(tt.fake.calls) != 1 {
					t.Fatalf("expected 1 Run call, got %d", len(tt.fake.calls))
				}
				call := tt.fake.calls[0]
				if call.name != "opencode" || !equalSlice(call.args, []string{"session", "list"}) {
					t.Errorf("Run = %q %v, want opencode [session list]", call.name, call.args)
				}
			}
			if tt.kind == "nope" {
				if len(tt.fake.calls) != 0 {
					t.Errorf("expected no Run calls for unsupported kind, got %d", len(tt.fake.calls))
				}
			}
		})
	}
}

func TestPrune(t *testing.T) {
	t.Run("gemini-cli index-prunes from highest index down", func(t *testing.T) {
		list := "Available sessions for this project (2):\n" +
			"  1. foo (4 days ago) [uuid1]\n" +
			"  2. bar (3 days ago) [uuid2]\n"
		fake := &fakeRun{out: list}
		m := Manager{Run: fake.Run}
		out, skipped, err := m.Prune("gemini-cli")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if skipped {
			t.Error("expected skipped=false (gemini has index-delete)")
		}
		// 1 list + 2 deletes; deletes go HIGH→LOW so indices don't shift mid-prune.
		if len(fake.calls) != 3 {
			t.Fatalf("expected 3 Run calls, got %d: %v", len(fake.calls), fake.calls)
		}
		if fake.calls[0].name != "gemini" || !equalSlice(fake.calls[0].args, []string{"--list-sessions"}) {
			t.Errorf("call 0 should list: %q %v", fake.calls[0].name, fake.calls[0].args)
		}
		if !equalSlice(fake.calls[1].args, []string{"--delete-session", "2"}) {
			t.Errorf("delete should start at highest index 2, got %v", fake.calls[1].args)
		}
		if !equalSlice(fake.calls[2].args, []string{"--delete-session", "1"}) {
			t.Errorf("then index 1, got %v", fake.calls[2].args)
		}
		if !strings.Contains(out, "pruned 2") {
			t.Errorf("output = %q, want 'pruned 2'", out)
		}
	})

	t.Run("nope returns skipped", func(t *testing.T) {
		fake := &fakeRun{}
		m := Manager{Run: fake.Run}
		out, skipped, err := m.Prune("nope")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !skipped {
			t.Error("expected skipped=true")
		}
		if out != "" {
			t.Errorf("expected empty output, got %q", out)
		}
		if len(fake.calls) != 0 {
			t.Errorf("expected no Run calls, got %d", len(fake.calls))
		}
	})

	t.Run("opencode returns skipped", func(t *testing.T) {
		fake := &fakeRun{}
		m := Manager{Run: fake.Run}
		out, skipped, err := m.Prune("opencode")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !skipped {
			t.Error("expected skipped=true")
		}
		if out != "" {
			t.Errorf("expected empty output, got %q", out)
		}
		if len(fake.calls) != 0 {
			t.Errorf("expected no Run calls, got %d", len(fake.calls))
		}
	})

	t.Run("list verify gemini-cli calls Run with correct args", func(t *testing.T) {
		fake := &fakeRun{out: "session-abc"}
		m := Manager{Run: fake.Run}
		got, err := m.List("gemini-cli")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "session-abc" {
			t.Errorf("output = %q, want %q", got, "session-abc")
		}
		if len(fake.calls) != 1 {
			t.Fatalf("expected 1 Run call, got %d", len(fake.calls))
		}
		call := fake.calls[0]
		if call.name != "gemini" {
			t.Errorf("Run name = %q, want gemini", call.name)
		}
		wantArgs := []string{"--list-sessions"}
		if !equalSlice(call.args, wantArgs) {
			t.Errorf("Run args = %v, want %v", call.args, wantArgs)
		}
	})
}

func TestPruneWithKind(t *testing.T) {
	// Add a test kind with a Prune command, then remove it after the test.
	specs["test-prune"] = Spec{Command: "prunecli", List: []string{"--list"}, Prune: []string{"--prune"}}
	defer delete(specs, "test-prune")

	t.Run("prune calls Run with prune args", func(t *testing.T) {
		fake := &fakeRun{out: "pruned 5 sessions"}
		m := Manager{Run: fake.Run}
		out, skipped, err := m.Prune("test-prune")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if skipped {
			t.Error("expected skipped=false")
		}
		if out != "pruned 5 sessions" {
			t.Errorf("output = %q, want %q", out, "pruned 5 sessions")
		}
		if len(fake.calls) != 1 {
			t.Fatalf("expected 1 Run call, got %d", len(fake.calls))
		}
		call := fake.calls[0]
		if call.name != "prunecli" {
			t.Errorf("Run name = %q, want %q", call.name, "prunecli")
		}
		wantArgs := []string{"--prune"}
		if !equalSlice(call.args, wantArgs) {
			t.Errorf("Run args = %v, want %v", call.args, wantArgs)
		}
	})

	t.Run("CanPrune returns true for test-prune", func(t *testing.T) {
		if !CanPrune("test-prune") {
			t.Error("expected CanPrune=true for test-prune")
		}
	})

	t.Run("prune error propagates", func(t *testing.T) {
		fake := &fakeRun{err: fmtError("prune failed")}
		m := Manager{Run: fake.Run}
		_, skipped, err := m.Prune("test-prune")
		if skipped {
			t.Error("expected skipped=false")
		}
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "prune failed") {
			t.Errorf("error = %q, want containing %q", err.Error(), "prune failed")
		}
	})
}

func TestCanCleanup(t *testing.T) {
	if !CanCleanup("opencode") {
		t.Error("opencode has list+delete → CanCleanup should be true")
	}
	if CanCleanup("gemini-cli") {
		t.Error("gemini-cli has no delete-by-id → CanCleanup should be false")
	}
	if CanCleanup("nope") {
		t.Error("unknown kind → CanCleanup should be false")
	}
}

func TestCleanupByTitle(t *testing.T) {
	t.Run("opencode deletes only rows matching the tag", func(t *testing.T) {
		list := "Session ID   Title         Updated\n" +
			"────────────────────────────────\n" +
			"ses_aaa111  pact:dev      11:00\n" +
			"ses_bbb222  someone-else  10:00\n" +
			"ses_ccc333  pact:dev      09:00\n"
		fake := &fakeRun{out: list}
		m := Manager{Run: fake.Run}
		deleted, skipped, err := m.CleanupByTitle("opencode", SessionTag("dev"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if skipped {
			t.Fatal("expected skipped=false for opencode")
		}
		if !equalSlice(deleted, []string{"ses_aaa111", "ses_ccc333"}) {
			t.Fatalf("deleted = %v, want [ses_aaa111 ses_ccc333]", deleted)
		}
		// 1 list + 2 deletes, in order.
		if len(fake.calls) != 3 {
			t.Fatalf("expected 3 Run calls, got %d", len(fake.calls))
		}
		if fake.calls[0].name != "opencode" || !equalSlice(fake.calls[0].args, []string{"session", "list"}) {
			t.Errorf("list call = %q %v", fake.calls[0].name, fake.calls[0].args)
		}
		if !equalSlice(fake.calls[1].args, []string{"session", "delete", "ses_aaa111"}) {
			t.Errorf("delete 1 args = %v", fake.calls[1].args)
		}
		if !equalSlice(fake.calls[2].args, []string{"session", "delete", "ses_ccc333"}) {
			t.Errorf("delete 2 args = %v", fake.calls[2].args)
		}
	})

	t.Run("unsupported kind → skipped no-op", func(t *testing.T) {
		fake := &fakeRun{}
		m := Manager{Run: fake.Run}
		deleted, skipped, err := m.CleanupByTitle("claude-code", SessionTag("dev"))
		if err != nil || !skipped || deleted != nil {
			t.Fatalf("want nil,true,nil; got %v,%v,%v", deleted, skipped, err)
		}
		if len(fake.calls) != 0 {
			t.Errorf("expected no Run calls, got %d", len(fake.calls))
		}
	})

	t.Run("no matching tag → only the list call, no deletes", func(t *testing.T) {
		fake := &fakeRun{out: "ses_zzz999  other-title  10:00\n"}
		m := Manager{Run: fake.Run}
		deleted, _, err := m.CleanupByTitle("opencode", SessionTag("dev"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(deleted) != 0 {
			t.Errorf("expected 0 deletes, got %v", deleted)
		}
		if len(fake.calls) != 1 {
			t.Errorf("expected only the list call, got %d calls", len(fake.calls))
		}
	})
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fmtError is a helper to create an error for tests without importing fmt in tests.
type strErr string

func (e strErr) Error() string { return string(e) }

func fmtError(s string) error { return strErr(s) }
