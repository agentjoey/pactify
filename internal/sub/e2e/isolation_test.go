//go:build harness_e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/credentiallinks"
)

func (s *suite) newFixture(t *testing.T, auth bool) *fixture {
	root, err := os.MkdirTemp("", "pactify-e2e-")
	must(t, err)
	root, _ = filepath.EvalSymlinks(root)
	f := &fixture{s: s, t: t, root: root, repo: filepath.Join(root, "repo")}
	s.owner.Cleanup(func() {
		f.checkCredentials()
		if f.preserveRoot {
			s.owner.Errorf("CREDENTIAL REFRESH AT RISK: preserving fixture root %s", root)
			return
		}
		for path := range f.credentials {
			if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink == 0 {
				s.owner.Errorf("CREDENTIAL REFRESH AT RISK: preserving fixture root %s", root)
				return
			}
		}
		if err := os.RemoveAll(root); err != nil {
			s.owner.Error(err)
		}
	})
	f.env, err = isolatedFixtureEnv(root, os.Environ())
	must(t, err)
	if auth {
		f.credentials = map[string]string{}
		link := func(src, rel string) {
			if _, e := os.Lstat(src); os.IsNotExist(e) {
				return
			} else if e != nil {
				t.Fatal(e)
			}
			dst := filepath.Join(root, rel)
			must(t, os.MkdirAll(filepath.Dir(dst), 0700))
			must(t, os.Symlink(src, dst))
			if info, e := os.Stat(src); e == nil && info.Mode().IsRegular() && filepath.Base(src) != "config.toml" && filepath.Base(src) != "device_id" {
				f.credentials[dst] = src
			}
			if s.harness == "kimi" && (filepath.Base(src) == "credentials" || filepath.Base(src) == "oauth") {
				f.credentials[dst] = src
			}
		}
		switch s.harness {
		case "codex":
			link(filepath.Join(s.user, ".codex/auth.json"), "home/.codex/auth.json")
		case "opencode":
			// v2 never inherits a user login; each fixture's jobs own a private DB.
			if s.adapter.Opencode2 {
				break
			}
			link(filepath.Join(s.user, ".local/share/opencode/auth.json"), "share/opencode/auth.json")
		case "agy":
			link(filepath.Join(s.user, ".gemini/antigravity-cli/antigravity-oauth-token"), "home/.gemini/antigravity-cli/antigravity-oauth-token")
		case "kimi":
			for _, n := range []string{"config.toml", "device_id", "credentials", "oauth"} {
				if n == "oauth" {
					// Kimi creates this lazily. Link the shared source, never a
					// fixture-local lock directory next to shared credentials.
					if info, err := os.Stat(filepath.Join(s.user, ".kimi-code")); err == nil && info.IsDir() {
						if err := os.Mkdir(filepath.Join(s.user, ".kimi-code", "oauth"), 0700); err != nil && !os.IsExist(err) {
							t.Fatal(err)
						}
					}
				}
				link(filepath.Join(s.user, ".kimi-code", n), "kimi/"+n)
			}
		}
	}
	must(t, os.MkdirAll(f.repo, 0700))
	f.git("init", "-q")
	f.git("config", "user.email", "e2e@example.invalid")
	f.git("config", "user.name", "E2E")
	f.write("existing.txt", "before\n")
	f.git("add", ".")
	f.git("commit", "-qm", "fixture")
	return f
}

func (f *fixture) checkCredentials() {
	if f.credentialsChecked {
		return
	}
	f.credentialsChecked = true
	innerQuarantine := filepath.Join(f.root, "pactify", "credential-quarantine")
	if err := credentiallinks.VerifyFixtureQuarantine(innerQuarantine, f.s.quarantine); err != nil {
		f.failures = append(f.failures, err.Error())
		f.s.owner.Error(err)
		if _, err := os.Lstat(innerQuarantine); !os.IsNotExist(err) {
			f.preserveRoot = true
		}
	}
	for _, id := range f.jobs {
		out, _ := f.cli("", "result", id, "--json")
		var result struct {
			Warnings []string `json:"warnings"`
			Status   string   `json:"status"`
		}
		// Failed/canceled jobs intentionally return nonzero with valid JSON.
		if json.Unmarshal(out, &result) != nil || result.Status == "" || requiresCredentialRetention(result.Warnings) {
			// A failed production write-back/quarantine can leave a refresh in
			// a nested managed home, while the fixture's outer link is intact.
			f.preserveRoot = true
			message := fmt.Sprintf("credential preservation could not be confirmed for job %s; fixture retained at %s", id, f.root)
			f.failures = append(f.failures, message)
			f.s.owner.Error(message)
		}
	}
	for path, target := range f.credentials {
		if err := credentiallinks.VerifyFileLink(path, target, f.s.quarantine); err != nil {
			// The child test can have completed before the parent's final cleanup.
			f.failures = append(f.failures, err.Error())
			f.s.owner.Error(err)
		}
	}
	for _, path := range f.hostFallbacks {
		// Preserve the fixture too if a fallback was an unexpected symlink.
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			f.preserveRoot = true
		}
		if err := credentiallinks.VerifyNoFallback(path, f.s.quarantine); err != nil {
			f.failures = append(f.failures, err.Error())
			f.s.owner.Error(err)
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				f.preserveRoot = true
			}
		}
	}
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func (f *fixture) write(name, content string) {
	must(f.t, os.WriteFile(filepath.Join(f.repo, name), []byte(content), 0600))
}
func (f *fixture) git(args ...string) string {
	b, e := command(15*time.Second, f.env, f.repo, "", "git", args...)
	if e != nil {
		f.fatalf("git: %v %s", e, safe(b))
	}
	return string(b)
}
func (f *fixture) file(root, name, want string) {
	b, e := os.ReadFile(filepath.Join(root, name))
	if e != nil || string(b) != want {
		f.errorf("file %s missing or incorrect (read error=%v; got=%q want=%q)", name, e, string(b), want)
	}
}
func (f *fixture) clean() {
	if v := f.git("status", "--porcelain=v1", "--untracked-files=all"); v != "" {
		f.errorf("original repository changed: %s", safe([]byte(v)))
	}
}
func (s *suite) selectModel(f *fixture) error {
	if strings.Contains(strings.ToLower(s.model), "deepseek-v4-pro") {
		return fmt.Errorf("forbidden model")
	}
	switch s.harness {
	case "opencode":
		available, e := liveOpencodeModels(f)
		if e != nil {
			return e
		}
		if s.model != "" {
			if !allowedOpencode(s.model) || !available[s.model] {
				return fmt.Errorf("override must be live-listed opencode/*-free or minimax-coding-plan/MiniMax-M3")
			}
			return nil
		}
		models := []string{}
		for m := range available {
			if strings.HasPrefix(m, "opencode/") && strings.HasSuffix(m, "-free") && allowedOpencode(m) {
				models = append(models, m)
			}
		}
		sort.Strings(models)
		if len(models) == 0 {
			return fmt.Errorf("no live opencode/*-free model available")
		}
		s.model = models[0]
		for _, m := range models {
			if strings.Contains(m, "flash") {
				s.model = m
				break
			}
		}
	case "agy":
		// agy 1.4.3 names flash by effort tier; the bare name is no longer accepted.
		if s.model == "" {
			s.model = agyE2EModel
		}
		if !allowedAgy(s.model) {
			return fmt.Errorf("agy model must be a gemini-3.8-flash tier (-low, -medium or -high)")
		}
	case "kimi":
		if s.model != "" && !strings.HasPrefix(s.model, "kimi-code/") {
			return fmt.Errorf("kimi overrides must use the kimi-code subscription provider")
		}
	case "codex":
	}
	return nil
}

const agyE2EModel = "gemini-3.8-flash-low"

func allowedAgy(m string) bool {
	switch m {
	case "gemini-3.8-flash-low", "gemini-3.8-flash-medium", "gemini-3.8-flash-high":
		return true
	}
	return false
}
func allowedOpencode(m string) bool {
	return !strings.Contains(strings.ToLower(m), "deepseek-v4-pro") && (strings.HasPrefix(m, "opencode/") && strings.HasSuffix(m, "-free") || m == "minimax-coding-plan/MiniMax-M3")
}
func (f *fixture) quota() (json.RawMessage, error) {
	b, e := command(30*time.Second, f.env, f.repo, "", "agy", "-p", "/usage", "--model", agyE2EModel, "--output-format", "json")
	if e != nil {
		return nil, fmt.Errorf("agy /usage: %v", e)
	}
	for _, line := range strings.Split(string(b), "\n") {
		var v struct {
			Status  string `json:"status"`
			Turns   int    `json:"num_turns"`
			Command struct {
				Name string `json:"name"`
			} `json:"command"`
		}
		if json.Unmarshal([]byte(line), &v) == nil && v.Status == "SUCCESS" && v.Turns == 0 && v.Command.Name == "usage" {
			return json.RawMessage(line), nil
		}
	}
	return nil, fmt.Errorf("agy /usage returned no valid non-billable result")
}

func (s *suite) userRoots() []string {
	if s.adapter.Opencode2 {
		return opencodeUserRoots(s.user, os.Environ())
	}
	names := map[string][]string{"codex": {".codex"}, "opencode": {".config/opencode", ".local/share/opencode"}, "kimi": {".kimi-code"}, "agy": {".gemini"}}[s.harness]
	roots := []string{}
	for _, n := range names {
		roots = append(roots, filepath.Join(s.user, n))
	}
	return roots
}
