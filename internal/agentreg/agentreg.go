package agentreg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/agent"
)

type Agent struct {
	Kind         string `json:"kind"`
	Label        string `json:"label,omitempty"`
	RegisteredAt string `json:"registered_at"`
	// Per-agent launch overrides (#10 model config, #4/#9 scoped permissions).
	// All omitempty so an un-configured agent serializes exactly as before.
	Model        string   `json:"model,omitempty"`         // overrides the kind's default model pin
	AllowedTools []string `json:"allowed_tools,omitempty"` // tool allowlist when Restricted
	Restricted   bool     `json:"restricted,omitempty"`    // true → scoped posture (use AllowedTools) instead of blanket auto-approve
}

type Registry struct {
	Agents []Agent `json:"agents"`
}

func file() string {
	if h := os.Getenv("PACTIFY_HOME"); h != "" {
		return filepath.Join(h, "agents.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pactify", "agents.json")
}

func Load() (Registry, error) {
	var r Registry
	b, err := os.ReadFile(file())
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	return r, nil
}

func (r Registry) Save() error {
	if err := os.MkdirAll(filepath.Dir(file()), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file(), b, 0o644)
}

func (r *Registry) Register(kind, label, ts string) error {
	if _, ok := agent.Get(kind); !ok {
		return fmt.Errorf("agentreg: unknown kind %q", kind)
	}
	for i, a := range r.Agents {
		if a.Kind == kind {
			r.Agents[i].Label = label
			return nil
		}
	}
	r.Agents = append(r.Agents, Agent{Kind: kind, Label: label, RegisteredAt: ts})
	return nil
}

func (r *Registry) Unregister(kind string) error {
	for i, a := range r.Agents {
		if a.Kind == kind {
			r.Agents = append(r.Agents[:i], r.Agents[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r Registry) Has(kind string) bool {
	for _, a := range r.Agents {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

// SetConfig sets per-agent launch overrides (model + scoped-permission posture)
// for an already-registered kind. Pass empty model / nil tools / restricted=false
// to clear those overrides. Returns an error if the kind is not registered (you
// configure an agent you've adopted, not an arbitrary kind).
func (r *Registry) SetConfig(kind, model string, allowedTools []string, restricted bool) error {
	for i := range r.Agents {
		if r.Agents[i].Kind == kind {
			r.Agents[i].Model = model
			r.Agents[i].AllowedTools = allowedTools
			r.Agents[i].Restricted = restricted
			return nil
		}
	}
	return fmt.Errorf("agentreg: %q is not registered — register it before setting config", kind)
}

// Config returns the per-kind launch override (model, allowed tools, restricted),
// or zero values if the kind is unregistered or unconfigured.
func (r Registry) Config(kind string) (model string, allowedTools []string, restricted bool) {
	for _, a := range r.Agents {
		if a.Kind == kind {
			return a.Model, a.AllowedTools, a.Restricted
		}
	}
	return "", nil, false
}
