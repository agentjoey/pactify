package registry

import (
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
