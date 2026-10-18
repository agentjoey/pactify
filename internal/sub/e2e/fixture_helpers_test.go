package e2e

import (
	"context"
	"fmt"
	"github.com/agentjoey/pactify/internal/sub"
	"os"
	"path/filepath"
	"strings"
)

// Fixture selection delegates to exactly the same version probe and backend
// selection as jobs/doctor; no independent semver parsing lives in this suite.
func fixtureHarness(ctx context.Context, h sub.Harness) (sub.Harness, string, error) {
	if h.Version == nil {
		return h, "", fmt.Errorf("missing harness version probe")
	}
	version, err := h.Version(ctx)
	if err != nil {
		return h, "", err
	}
	if h.Supported != nil {
		if err := h.Supported(version); err != nil {
			return h, version, err
		}
	}
	return h.ForVersion(version), version, nil
}

func setenv(env []string, key, value string) []string {
	out := []string{}
	for _, v := range env {
		if !strings.HasPrefix(v, key+"=") {
			out = append(out, v)
		}
	}
	return append(out, key+"="+value)
}

// Build only from runtime metadata, never inherited credential/config variables.
func isolatedFixtureEnv(root string, inherited []string) ([]string, error) {
	var env []string
	for _, key := range []string{"PATH", "LANG", "LC_ALL", "TERM", "SHELL", "USER", "LOGNAME", "SYSTEMROOT", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value := envValue(inherited, key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	for key, rel := range map[string]string{"HOME": "home", "PACTIFY_HOME": "pactify", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "share", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state", "CODEX_HOME": "home/.codex", "KIMI_CODE_HOME": "kimi", "TMPDIR": "tmp"} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
		env = setenv(env, key, path)
	}
	return append(env, "KIMI_CODE_NO_AUTO_UPDATE=1", "CHOKIDAR_USEPOLLING=1", "OPENCODE_DISABLE_AUTOUPDATE=true"), nil
}

func opencodeFixtureEnv(h sub.Harness, env []string) ([]string, error) {
	env = append([]string{}, env...)
	if !h.Opencode2 {
		return env, nil
	}
	home := envValue(env, "PACTIFY_HOME")
	if home == "" || !filepath.IsAbs(home) {
		return nil, fmt.Errorf("v2 fixture requires an absolute PACTIFY_HOME")
	}
	for key, path := range sub.Opencode2ManagedHomes(home) {
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
		env = setenv(env, key, path)
	}
	// Discovery must use the same private home as jobs, without user config or login links.
	env = setenv(env, "OPENCODE_CONFIG_DIR", filepath.Join(envValue(env, "XDG_CONFIG_HOME"), "opencode"))
	env = setenv(env, "OPENCODE_CONFIG", "")
	env = setenv(env, "OPENCODE_CONFIG_CONTENT", "")
	env = setenv(env, "OPENCODE_DISABLE_PROJECT_CONFIG", "1")
	env = setenv(env, "OPENCODE_DISABLE_CLAUDE_CODE", "1")
	return env, nil
}

func opencodeUserRoots(user string, env []string) []string {
	var roots []string
	// Data and config hold the user's credentials, sessions and settings. The
	// cache and state roots are omitted: a user's own long-running opencode
	// server refreshes cache/opencode/models.json and state/opencode/locks on
	// its own schedule (observed mid-suite on 2026-10-18), and jobs never see
	// those roots because all four XDG variables point at pactify's home.
	for _, item := range []struct{ key, fallback string }{{"XDG_DATA_HOME", ".local/share"}, {"XDG_CONFIG_HOME", ".config"}} {
		path := envValue(env, item.key)
		if path == "" {
			path = filepath.Join(user, item.fallback)
		}
		roots = append(roots, filepath.Join(path, "opencode"))
	}
	return append(roots, filepath.Join(roots[0], "opencode.db"))
}

func envValue(env []string, key string) string {
	for _, v := range env {
		if strings.HasPrefix(v, key+"=") {
			return strings.TrimPrefix(v, key+"=")
		}
	}
	return ""
}
