package agentreg

import "testing"

func TestSetConfigRequiresRegistered(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.SetConfig("opencode", "x/y", nil, false); err == nil {
		t.Fatal("expected error setting config on unregistered agent")
	}
}

func TestSetConfigModelAndScope(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.Register("claude-code", "", "2026-06-14T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetConfig("claude-code", "claude-sonnet-4-6", []string{"Read", "Edit"}, true); err != nil {
		t.Fatal(err)
	}
	model, tools, restricted := r.Config("claude-code")
	if model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", model)
	}
	if len(tools) != 2 || tools[0] != "Read" || tools[1] != "Edit" {
		t.Errorf("tools = %v, want [Read Edit]", tools)
	}
	if !restricted {
		t.Error("restricted = false, want true")
	}
}

func TestConfigUnsetIsZero(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.Register("opencode", "", "2026-06-14T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	model, tools, restricted := r.Config("opencode")
	if model != "" || tools != nil || restricted {
		t.Errorf("unset config = (%q,%v,%v), want zero", model, tools, restricted)
	}
	// Unknown kind also returns zero, not a panic.
	if m, _, _ := r.Config("no-such"); m != "" {
		t.Errorf("unknown kind model = %q, want empty", m)
	}
}

func TestConfigSurvivesRoundtrip(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.Register("gemini-cli", "", "2026-06-14T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetConfig("gemini-cli", "gemini-2.5-flash", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	r2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if m, _, _ := r2.Config("gemini-cli"); m != "gemini-2.5-flash" {
		t.Errorf("after roundtrip model = %q, want gemini-2.5-flash", m)
	}
}

// Re-registering an already-configured agent preserves its config (only label
// updates), mirroring how Register preserves RegisteredAt.
func TestRegisterPreservesConfig(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r := &Registry{}
	if err := r.Register("opencode", "first", "2026-06-14T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetConfig("opencode", "x/custom", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("opencode", "second", "2026-06-15T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := r.Config("opencode"); m != "x/custom" {
		t.Errorf("config model after re-register = %q, want x/custom (preserved)", m)
	}
}
