package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDSHRegistration(t *testing.T) {
	h, ok := Lookup("dsh")
	if !ok {
		t.Fatal("dsh missing")
	}
	for _, v := range []string{"0.2.1-rc.5", "0.2.1"} {
		if err := h.Supported(v); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"0.2.0", "0.2.2", "0.3.0", "garbage", "0.2.1-", "0.2.1-!!!"} {
		if err := h.Supported(v); err == nil || !strings.Contains(err.Error(), "npm install") {
			t.Fatalf("version %s: %v", v, err)
		}
	}
	if h.NoReadOnly != "" || !h.MinimalConfig || h.Home == nil || h.Conceal == nil {
		t.Fatal("missing capabilities")
	}
}
func TestDSHManagedHome(t *testing.T) {
	source := t.TempDir()
	home := filepath.Join(t.TempDir(), "managed")
	target := filepath.Join(source, ".credentials.yaml")
	if err := os.WriteFile(target, []byte("fixture-secret-never-read"), 0600); err != nil {
		t.Fatal(err)
	}
	m := dshHome(home, source)
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(filepath.Join(home, ".credentials.yaml"))
	if err != nil || link != target {
		t.Fatalf("link=%q err=%v", link, err)
	}
	if err = m.Check(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(target); err != nil {
		t.Fatal(err)
	}
	m = dshHome(filepath.Join(t.TempDir(), "env-only"), source)
	if err = m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
}
