package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{"Pactify": "pactify", "my repo!": "my-repo", "TradeLinks": "tradelinks"}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q)=%q want %q", in, got, want)
		}
	}
}

func TestAddListRemoveRoundtrip(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add("pactify", "/abs/pactify", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("", "/abs/TradeLinks", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	r2, _ := Load()
	if len(r2.Projects) != 2 || r2.Projects[1].Name != "tradelinks" {
		t.Fatalf("bad reload: %+v", r2.Projects)
	}
	if err := r2.Add("pactify", "/other", ""); err == nil {
		t.Fatal("duplicate name must error")
	}
	if err := r2.Remove("pactify"); err != nil {
		t.Fatal(err)
	}
	if len(r2.Projects) != 1 || r2.Projects[0].Name != "tradelinks" {
		t.Fatalf("remove failed: %+v", r2.Projects)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	t.Setenv("PACTIFY_HOME", filepath.Join(t.TempDir(), "nope"))
	r, err := Load()
	if err != nil || len(r.Projects) != 0 {
		t.Fatalf("want empty,nil got %+v,%v", r.Projects, err)
	}
}

func TestAddGroupPersists(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add("proj", "/abs/proj", "mygroup"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	r2, _ := Load()
	if len(r2.Projects) != 1 {
		t.Fatalf("want 1 project, got %d", len(r2.Projects))
	}
	if r2.Projects[0].Group != "mygroup" {
		t.Fatalf("group=%q want %q", r2.Projects[0].Group, "mygroup")
	}
}

func TestRenamePreservesPathAndGroup(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var r Registry
	if err := r.Add("old-name", "/tmp/proj", "team-a"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := r.Rename("old-name", "new-name"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if len(r.Projects) != 1 {
		t.Fatalf("want 1 project, got %d", len(r.Projects))
	}
	p := r.Projects[0]
	if p.Name != "new-name" {
		t.Errorf("name = %q, want new-name", p.Name)
	}
	if p.Group != "team-a" {
		t.Errorf("group = %q, want team-a (preserved)", p.Group)
	}
	if p.Path != "/tmp/proj" {
		t.Errorf("path = %q, want /tmp/proj (preserved)", p.Path)
	}
}

func TestRenameEmptyNewNameIsError(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var r Registry
	_ = r.Add("a", "/tmp/a", "")
	if err := r.Rename("a", "!!!"); err == nil {
		t.Fatal("new name that slugs to empty must error")
	}
}

func TestRenameUnknownIsError(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var r Registry
	if err := r.Rename("ghost", "x"); err == nil {
		t.Fatal("renaming an unknown project must error")
	}
}

func TestRenameToExistingNameIsError(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var r Registry
	_ = r.Add("a", "/tmp/a", "")
	_ = r.Add("b", "/tmp/b", "")
	if err := r.Rename("a", "b"); err == nil {
		t.Fatal("renaming onto an existing name must error")
	}
}

// EnsureRegistered idempotently registers a path (spec: auto-register at init/
// orchestrate so agent-started projects are visible without a manual step).
func TestEnsureRegisteredIdempotent(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()

	name, added, err := EnsureRegistered(dir)
	if err != nil || !added || name != Slug(filepath.Base(dir)) {
		t.Fatalf("first call must register: name=%q added=%v err=%v", name, added, err)
	}
	// Second call on the SAME path is a no-op (already registered), same name.
	name2, added2, err := EnsureRegistered(dir)
	if err != nil || added2 || name2 != name {
		t.Fatalf("re-register must be a no-op: name=%q added=%v err=%v", name2, added2, err)
	}
	// Only one entry.
	r, _ := Load()
	if len(r.Projects) != 1 {
		t.Fatalf("expected exactly one registered project, got %d", len(r.Projects))
	}
}

// A different path whose basename slugs to an already-taken name is a real
// conflict — EnsureRegistered surfaces the error (the caller decides whether to
// warn or fail; init must warn, not block).
func TestEnsureRegisteredNameConflict(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	base := t.TempDir()
	a := filepath.Join(base, "proj")
	b := filepath.Join(base, "sub", "proj") // same basename "proj", different path
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	if _, _, err := EnsureRegistered(a); err != nil {
		t.Fatal(err)
	}
	if _, added, err := EnsureRegistered(b); err == nil || added {
		t.Fatalf("same-basename different-path must conflict, got added=%v err=%v", added, err)
	}
}
