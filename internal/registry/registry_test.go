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
