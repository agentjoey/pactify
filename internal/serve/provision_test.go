package serve

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

func TestServeProvisioner(t *testing.T) {
	root := t.TempDir()
	// A source repo to clone.
	srcRepo := filepath.Join(root, "src")
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(srcRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(srcRepo, "init")
	run(srcRepo, "commit", "--allow-empty", "-m", "init")

	// Isolate the registry file to this test.
	provDir := filepath.Join(root, "provisioned")
	t.Setenv("PACTIFY_PROVISION_DIR", provDir)
	t.Setenv("PACTIFY_HOME", filepath.Join(root, "pactify-home"))

	srv := New(nil)
	pv := &serveProvisioner{s: srv}

	// Disabled when the dir env is empty.
	t.Setenv("PACTIFY_PROVISION_DIR", "")
	if _, err := pv.Provision(srcRepo, "demo"); err == nil {
		t.Fatal("provision should be disabled without PACTIFY_PROVISION_DIR")
	}
	t.Setenv("PACTIFY_PROVISION_DIR", provDir)

	name, err := pv.Provision(srcRepo, "My Demo")
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}
	if name != registry.Slug("My Demo") {
		t.Fatalf("registered name %q, want slug of 'My Demo'", name)
	}
	// The clone exists + is registered in the live server.
	if _, err := os.Stat(filepath.Join(provDir, name, ".git")); err != nil {
		t.Fatalf("clone not present: %v", err)
	}
	srv.pmu.RLock()
	_, ok := srv.projects[name]
	srv.pmu.RUnlock()
	if !ok {
		t.Fatal("provisioned project not added to the live server")
	}
	// A second provision of the same name → rejected (dest exists).
	if _, err := pv.Provision(srcRepo, "My Demo"); err == nil {
		t.Fatal("re-provisioning an existing dest should fail")
	}
}
