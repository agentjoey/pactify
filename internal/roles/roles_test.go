package roles

import (
	"path/filepath"
	"testing"
)

func TestSetProfileBindLookupRoundtrip(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())

	c, err := Load()
	if err != nil {
		t.Fatalf("Load on a missing file must be empty, not an error: %v", err)
	}
	if err := c.SetProfile("frontend", Profile{Kind: "claude-code", Model: "claude-opus-4-8", Fallback: []string{"cheap"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("cheap", Profile{Kind: "kimi-cli", Model: "kimi-code/kimi-for-coding"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w2", "frontend"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	p, role, ok := got.Lookup("w2")
	if !ok || role != "frontend" || p.Kind != "claude-code" || p.Model != "claude-opus-4-8" {
		t.Fatalf("Lookup(w2) = (%+v, %q, %v)", p, role, ok)
	}
	if len(p.Fallback) != 1 || p.Fallback[0] != "cheap" {
		t.Fatalf("fallback chain lost in round-trip: %+v", p.Fallback)
	}
	if _, _, ok := got.Lookup("nobody"); ok {
		t.Fatal("an unbound seat must report ok=false")
	}
}

func TestBindUnknownRoleRejected(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := Load()
	if err := c.Bind("w2", "nope"); err == nil {
		t.Fatal("binding a seat to an undefined role must error")
	}
}

func TestPathHonorsPactifyHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	if want := filepath.Join(home, "roles.json"); Path() != want {
		t.Fatalf("Path() = %q, want %q", Path(), want)
	}
}
