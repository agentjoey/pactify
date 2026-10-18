//go:build harness_e2e

package e2e

import (
	"context"
	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/sub"
	"os"
	"path/filepath"
	"testing"
)

// Offline: constructs a git fixture but never launches a harness or model.
func TestOpencode2LiveFixtureIsolation(t *testing.T) {
	user := t.TempDir()
	auth := filepath.Join(user, ".local/share/opencode/auth.json")
	if err := os.MkdirAll(filepath.Dir(auth), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, []byte("synthetic-login"), 0600); err != nil {
		t.Fatal(err)
	}
	h, _ := sub.Lookup("opencode")
	s := &suite{owner: t, harness: "opencode", user: user, adapter: h.ForVersion("v2.0.32"), quarantine: t.TempDir()}
	f := s.newFixture(t, true)
	if len(f.credentials) != 0 {
		t.Fatal("v2 inherited a credential link")
	}
	if _, err := os.Lstat(filepath.Join(f.root, "share/opencode/auth.json")); !os.IsNotExist(err) {
		t.Fatal("v2 fixture linked auth.json", err)
	}
	if envValue(f.env, "PACTIFY_HOME") != filepath.Join(f.root, "pactify") {
		t.Fatal("job fixture did not get its own pactify home")
	}
}

func TestOpencode2FixtureFreeModelDiscovery(t *testing.T) {
	h, _ := sub.Lookup("opencode")
	h = h.ForVersion("v2.0.32")
	root := t.TempDir()
	env, err := isolatedFixtureEnv(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	h.Models = func(_ context.Context, opts cockpit.StartOpts) ([]string, error) {
		calls++
		if !opts.MinimalConfig {
			t.Fatal("discovery must disable user configuration")
		}
		for key, want := range sub.Opencode2ManagedHomes(filepath.Join(root, "pactify")) {
			if got := envValue(opts.Env, key); got != want {
				t.Fatalf("%s=%s want=%s", key, got, want)
			}
		}
		return []string{"opencode/zeta-free", "opencode/flash-free", "paid/model"}, nil
	}
	s := &suite{harness: "opencode", adapter: h}
	f := &fixture{s: s, env: env, repo: root}
	if err := s.selectModel(f); err != nil || s.model != "opencode/flash-free" || calls != 1 {
		t.Fatalf("model=%s calls=%d err=%v", s.model, calls, err)
	}
	s.model = "paid/model"
	if err := s.selectModel(f); err == nil {
		t.Fatal("paid override accepted")
	}
}
