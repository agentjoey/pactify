package sessions

import (
	"strings"
	"testing"
)

func TestSupported(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"gemini-cli", true},
		{"opencode", false},
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
		{"gemini-cli", false},
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

func (f *fakeRun) Run(name string, args ...string) (string, error) {
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
			name:    "opencode unsupported",
			kind:    "opencode",
			fake:    &fakeRun{},
			wantErr: true,
			wantMsg: `sessions: listing not supported for kind "opencode"`,
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
			if tt.kind == "opencode" || tt.kind == "nope" {
				if len(tt.fake.calls) != 0 {
					t.Errorf("expected no Run calls for unsupported kind, got %d", len(tt.fake.calls))
				}
			}
		})
	}
}

func TestPrune(t *testing.T) {
	t.Run("gemini-cli returns skipped (Prune empty)", func(t *testing.T) {
		fake := &fakeRun{}
		m := Manager{Run: fake.Run}
		out, skipped, err := m.Prune("gemini-cli")
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
