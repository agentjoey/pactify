package sub

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/pelletier/go-toml/v2"
)

var kimiLinkedNames = []string{"config.toml", "credentials", "device_id", "oauth"}

func managedKimiHome() (string, error)         { return managedHarnessHome("kimi") }
func managedKimiReadOnlyHome() (string, error) { return managedHarnessHome("kimi-readonly") }

func kimiReadOnlyHome(home, source string) *managedHome {
	m := kimiHome(home, source)
	delete(m.Links, "config.toml")
	m.GenerateConfig = func() ([]byte, error) {
		f, err := os.Open(filepath.Join(source, "config.toml"))
		if err != nil {
			return nil, fmt.Errorf("kimi read-only config unavailable; run kimi login first: %w", err)
		}
		defer f.Close()
		const maxConfigSize = 1 << 20
		data, err := io.ReadAll(io.LimitReader(f, maxConfigSize+1))
		if err != nil {
			return nil, fmt.Errorf("kimi read-only config unavailable: cannot read user config.toml")
		}
		if len(data) > maxConfigSize {
			return nil, fmt.Errorf("kimi read-only config unavailable: user config.toml exceeds 1 MiB")
		}
		var user map[string]any
		if err := toml.Unmarshal(data, &user); err != nil {
			// TOML diagnostics may quote a secret-bearing source line.
			return nil, fmt.Errorf("kimi read-only config unavailable: user config.toml is invalid TOML")
		}
		cfg := map[string]any{}
		// Only these documented top-level fields locate/authenticate models.
		// providers retains api_key, api_key_env, env and OAuth storage/key
		// references. All other user sections (including future keys) are omitted.
		for _, key := range []string{"default_model", "models", "providers"} {
			if value, ok := user[key]; ok {
				cfg[key] = value
			}
		}
		cfg["default_permission_mode"] = "manual"
		cfg["tools"] = map[string]any{"enabled": cockpit.KimiReadOnlyTools()}
		var rules []map[string]string
		for _, pattern := range []string{"Write", "Edit", "Bash", "Agent*", "Cron*", "TaskStop"} {
			rules = append(rules, map[string]string{"decision": "deny", "pattern": pattern})
		}
		cfg["permission"] = map[string]any{"rules": rules}
		data, err = toml.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("kimi read-only config generation failed")
		}
		return data, nil
	}
	return m
}

func kimiReadOnlyConfigError(req Request) error {
	if req.Harness == "kimi" && req.Access == "read-only" && req.WithUserConfig {
		return fmt.Errorf("kimi --read-only cannot be used with --with-user-config; read-only requires a generated private config")
	}
	return nil
}
func kimiHome(home, source string) *managedHome {
	links := map[string]homeLink{}
	for _, name := range kimiLinkedNames {
		links[name] = homeLink{Target: filepath.Join(source, name), Directory: name == "credentials" || name == "oauth", Credential: name == "credentials", CreateDirectory: name == "oauth"}
	}
	return &managedHome{Name: "kimi", Home: home, Source: source, Links: links, Remove: []string{"mcp.json"}}
}

// checkKimiLinks verifies the managed home used by doctor.
func checkKimiLinks(home, source string) error { return kimiHome(home, source).Check() }

// Keep authentication/model overrides, but neutralize execution-related runtime
// switches. The 2.2.0 tools section itself has no environment binding; its
// global allowlist also constrains tools registered by projects/plugins/agents.
func kimiReadOnlyEnv() []string {
	out := []string{
		"KIMI_CODE_DANGEROUS_COMMAND_GUARD=true",
		"KIMI_CODE_BACKGROUND_KEEP_ALIVE_ON_EXIT=false",
		"KIMI_CODE_TRUST_WORKSPACE=0",
		"KIMI_DISABLE_CRON=1",
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "KIMI_CODE_EXPERIMENTAL_") {
			out = append(out, key+"=0")
		}
	}
	return out
}
