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
