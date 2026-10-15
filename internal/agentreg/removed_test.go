package agentreg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemovedGeminiRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "agents.json"), []byte(`{"agents":[{"kind":"gemini-cli","model":"old-model"},{"kind":"antigravity"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Load()
	if err != nil || len(r.Agents) != 2 || !r.Has("gemini-cli") || !r.Has("antigravity") {
		t.Fatalf("legacy registry load: %+v, %v", r, err)
	}
	const message = "gemini-cli has been removed; use antigravity (agy) instead"
	if err := r.Register("gemini-cli", "", ""); err == nil || !strings.Contains(err.Error(), message) {
		t.Errorf("Register error = %v", err)
	}
	if err := r.SetConfig("gemini-cli", "new-model", nil, false); err == nil || !strings.Contains(err.Error(), message) {
		t.Errorf("SetConfig error = %v", err)
	}
	if r.Agents[0].Model != "old-model" {
		t.Error("removed config was modified")
	}
	if err := r.Unregister("gemini-cli"); err != nil || r.Has("gemini-cli") {
		t.Fatalf("legacy entry must remain removable: %v", err)
	}
}
