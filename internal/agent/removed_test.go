package agent

import (
	"strings"
	"testing"
)

func TestRemovedGemini(t *testing.T) {
	const kind = "gemini-cli"
	const message = "gemini-cli has been removed; use antigravity (agy) instead"
	if _, ok := Get(kind); ok {
		t.Error("removed kind is registered")
	}
	if _, ok := RunnerProfileFor(kind); ok {
		t.Error("removed kind has a launch profile")
	}
	if _, _, err := Render(kind, "w", "worker", t.TempDir()); err == nil || !strings.Contains(err.Error(), message) {
		t.Errorf("Render error = %v", err)
	}
	if err := WireAt(t.TempDir(), kind, "w", "worker", t.TempDir()); err == nil || !strings.Contains(err.Error(), message) {
		t.Errorf("WireAt error = %v", err)
	}
	if err := RegisterExternal(External{Kind: kind}); err == nil || !strings.Contains(err.Error(), message) {
		t.Errorf("RegisterExternal error = %v", err)
	}
	t.Cleanup(func() { UnregisterExternal(kind) })
}
