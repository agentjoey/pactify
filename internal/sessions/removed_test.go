package sessions

import "testing"

func TestRemovedGeminiSessions(t *testing.T) {
	const kind = "gemini-cli"
	if Supported(kind) || CanPrune(kind) || CanCleanup(kind) {
		t.Error("removed kind advertises session capabilities")
	}
	m := Manager{Run: func(_, _ string, _ ...string) (string, error) { t.Fatal("removed CLI must not run"); return "", nil }}
	if _, err := m.List(kind); err == nil {
		t.Error("removed listing must be unsupported")
	}
	if _, skipped, err := m.Prune(kind); err != nil || !skipped {
		t.Errorf("Prune skipped=%v error=%v", skipped, err)
	}
}
