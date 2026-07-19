package serve

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/registry"
	"github.com/agentjoey/pactify/internal/remoteexec"
)

// serveProvisioner clones a repo onto this machine and registers it
// (pact.provision). Machine-level gate: it needs PACTIFY_PROVISION_DIR set (the
// base directory for clones); unset ⇒ provisioning disabled (the safe default).
// Cloning fetches remote code that later runs under orchestration, so it is
// opt-in at the machine level AND only wired when serve runs with
// --remote-control.
type serveProvisioner struct{ s *Server }

func (pv *serveProvisioner) Provision(repoURL, name string) (string, error) {
	base := strings.TrimSpace(os.Getenv("PACTIFY_PROVISION_DIR"))
	if base == "" {
		return "", errors.New("remote provisioning disabled (set PACTIFY_PROVISION_DIR to enable)")
	}
	slug := registry.Slug(name)
	if slug == "" {
		return "", fmt.Errorf("invalid project name %q", name)
	}
	dest := filepath.Join(base, slug)
	// Confine the clone to base/<slug> — Slug already strips separators, this is
	// belt-and-suspenders against a traversal via a crafted name.
	if rel, err := filepath.Rel(base, dest); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid destination for %q", name)
	}
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("destination already exists: %s", dest)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	// `git clone --` stops repoURL being parsed as an option (no --upload-pack
	// injection). repoURL is otherwise opaque to us and arrives over the relay
	// (untrusted), so also restrict git to safe transports: a crafted
	// `ext::sh -c '…'` / `fd::` URL would otherwise reach a remote helper and run
	// an arbitrary command (RCE), and `git://` enables SSRF. GIT_ALLOW_PROTOCOL is
	// git's native allowlist and also governs submodule URLs fetched during the
	// clone. Default file:https:ssh (local + the two real remote transports);
	// overridable for deployments that need http/git.
	protocols := strings.TrimSpace(os.Getenv("PACTIFY_PROVISION_GIT_PROTOCOLS"))
	if protocols == "" {
		protocols = "file:https:ssh"
	}
	cmd := exec.Command("git", "clone", "--", repoURL, dest)
	// Drop any inherited GIT_ALLOW_PROTOCOL so our value is authoritative, then set it.
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_ALLOW_PROTOCOL=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "GIT_ALLOW_PROTOCOL="+protocols)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(out)))
	}
	// Ensure a .pact/ dir so the live watch attaches even before the first
	// assign lands (the driver populates the ledger).
	_ = os.MkdirAll(filepath.Join(dest, ".pact"), 0o755)

	reg, err := registry.Load()
	if err != nil {
		return "", err
	}
	if err := reg.Add(slug, dest, "remote"); err != nil {
		return "", err
	}
	if err := reg.Save(); err != nil {
		return "", err
	}
	added := reg.Projects[len(reg.Projects)-1]
	if err := pv.s.AddProject(added); err != nil {
		_ = reg.Remove(added.Name)
		_ = reg.Save()
		return "", err
	}
	return added.Name, nil
}

// newProvisioner builds the machine-gated remote provisioner.
func (s *Server) newProvisioner() remoteexec.Provisioner { return &serveProvisioner{s: s} }

// serveTaskAuthor writes a task draft (pact.task) on a resolved project. A
// coordination write (no code runs), so it's gated like the pact verbs (wired
// only under --remote-control), not by the per-project remote-exec policy.
type serveTaskAuthor struct{ s *Server }

func (ta *serveTaskAuthor) AuthorTask(project, id, specMD string) error {
	ta.s.pmu.RLock()
	p, ok := ta.s.projects[project]
	ta.s.pmu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown project %q", project)
	}
	if _, err := authorTask(p.Path, id, specMD); err != nil {
		return err
	}
	return nil
}

// newTaskAuthor builds the remote task-author handler.
func (s *Server) newTaskAuthor() remoteexec.TaskAuthor { return &serveTaskAuthor{s: s} }
