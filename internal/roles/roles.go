// Package roles stores role profiles — named (agent kind, model) combinations —
// and the seat→role bindings that let the orchestrate driver resolve a seat to a
// concrete (kind, model). It is machine-level advisory configuration: it guides
// task assignment and fallback, and is deliberately NOT part of the pact
// protocol (no ledger events, no audit role).
package roles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Profile is a named (agent, model) combination, plus an ordered list of role
// names to fall back to when this profile cannot work (quota/auth/etc).
type Profile struct {
	Kind     string   `json:"kind"`
	Model    string   `json:"model,omitempty"`
	Fallback []string `json:"fallback,omitempty"`
}

// Config is the whole machine-level role configuration.
type Config struct {
	Profiles map[string]Profile `json:"profiles"`
	Bindings map[string]string  `json:"bindings"`
}

// Path is the config file (honors PACTIFY_HOME, mirroring internal/agentreg).
func Path() string {
	if h := os.Getenv("PACTIFY_HOME"); h != "" {
		return filepath.Join(h, "roles.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pactify", "roles.json")
}

// Load reads the config. A missing file is an EMPTY config, not an error: an
// unconfigured machine must drive exactly as it did before roles existed.
func Load() (Config, error) {
	c := Config{Profiles: map[string]Profile{}, Bindings: map[string]string{}}
	b, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("roles: %s is not valid JSON: %w", Path(), err)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	if c.Bindings == nil {
		c.Bindings = map[string]string{}
	}
	return c, nil
}

// Save writes the config, creating the parent directory if needed.
func (c Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), append(b, '\n'), 0o644)
}

// SetProfile creates or replaces a profile.
func (c *Config) SetProfile(name string, p Profile) error {
	if name == "" {
		return fmt.Errorf("roles: profile name required")
	}
	if p.Kind == "" {
		return fmt.Errorf("roles: profile %q needs a --kind", name)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	c.Profiles[name] = p
	return nil
}

// Bind points a seat at a role. The role must already be defined, so a typo
// fails loudly instead of silently leaving the seat unrouted.
func (c *Config) Bind(seat, role string) error {
	if seat == "" || role == "" {
		return fmt.Errorf("roles: bind needs a seat and a role")
	}
	if _, ok := c.Profiles[role]; !ok {
		return fmt.Errorf("roles: unknown role %q (define it with `pactify role set %s --kind <kind>`)", role, role)
	}
	if c.Bindings == nil {
		c.Bindings = map[string]string{}
	}
	c.Bindings[seat] = role
	return nil
}

// Unbind drops a seat's binding (the seat falls back to its roster kind).
func (c *Config) Unbind(seat string) error {
	if _, ok := c.Bindings[seat]; !ok {
		return fmt.Errorf("roles: seat %q is not bound to a role", seat)
	}
	delete(c.Bindings, seat)
	return nil
}

// Lookup resolves a seat to its profile and role name. ok=false when the seat is
// unbound or its role was deleted — callers then use the pre-roles behavior.
func (c Config) Lookup(seat string) (Profile, string, bool) {
	role, ok := c.Bindings[seat]
	if !ok {
		return Profile{}, "", false
	}
	p, ok := c.Profiles[role]
	if !ok {
		return Profile{}, "", false
	}
	return p, role, true
}
