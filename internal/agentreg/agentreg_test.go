package agentreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterUnknownKind(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	err := r.Register("bogus-agent", "", "2026-06-12T00:00:00Z")
	if err == nil {
		t.Fatal("expected error for unknown kind, got nil")
	}
}

func TestRegisterKnownKind(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	err := r.Register("opencode", "OpenCode Worker", "2026-06-12T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Has("opencode") {
		t.Fatal("expected Has to be true after register")
	}
	if len(r.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(r.Agents))
	}
	if r.Agents[0].Kind != "opencode" || r.Agents[0].Label != "OpenCode Worker" || r.Agents[0].RegisteredAt != "2026-06-12T00:00:00Z" {
		t.Fatalf("unexpected agent: %+v", r.Agents[0])
	}
}

func TestRegisterDuplicateKind(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.Register("opencode", "First Label", "2026-06-12T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("opencode", "Updated Label", "2026-06-13T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if len(r.Agents) != 1 {
		t.Fatalf("expected 1 agent (no duplicates), got %d", len(r.Agents))
	}
	if r.Agents[0].Label != "Updated Label" {
		t.Fatalf("expected label 'Updated Label', got %q", r.Agents[0].Label)
	}
	if r.Agents[0].RegisteredAt != "2026-06-12T00:00:00Z" {
		t.Fatalf("expected RegisteredAt preserved, got %q", r.Agents[0].RegisteredAt)
	}
}

func TestUnregister(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.Register("opencode", "OC", "2026-06-12T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := r.Unregister("opencode"); err != nil {
		t.Fatal(err)
	}
	if r.Has("opencode") {
		t.Fatal("expected Has to be false after unregister")
	}
	if len(r.Agents) != 0 {
		t.Fatalf("expected 0 agents, got %d", len(r.Agents))
	}
}

func TestUnregisterMissing(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.Unregister("nonexistent"); err != nil {
		t.Fatalf("expected nil error for unregistering missing kind, got %v", err)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	t.Setenv("PACTIFY_HOME", filepath.Join(t.TempDir(), "nope"))
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Agents) != 0 {
		t.Fatalf("expected empty registry, got %d agents", len(r.Agents))
	}
}

func TestLoadSaveRoundtrip(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.Register("opencode", "OC", "2026-06-12T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("claude-code", "Claude Code", "2026-06-12T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}

	r2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(r2.Agents))
	}
	if !r2.Has("opencode") || !r2.Has("claude-code") {
		t.Fatal("expected both agents after roundtrip")
	}
	if r2.Agents[0].Kind != "opencode" {
		t.Fatalf("expected opencode first, got %q", r2.Agents[0].Kind)
	}
	if r2.Agents[0].Label != "OC" {
		t.Fatalf("expected label OC, got %q", r2.Agents[0].Label)
	}
}

func TestRegisterUpdatesFile(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.Register("opencode", "OC", "2026-06-12T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(file())
	if err != nil {
		t.Fatal(err)
	}
	var saved Registry
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.Has("opencode") {
		t.Fatal("expected 'opencode' in saved file")
	}
}

func TestHasUnknownKind(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if r.Has("bogus") {
		t.Fatal("expected Has to be false for unknown kind")
	}
}
