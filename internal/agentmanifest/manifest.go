package agentmanifest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
	toml "github.com/pelletier/go-toml/v2"
)

// agentsDir resolves ~/.pactify/agents honoring PACTIFY_HOME (mirrors internal/registry).
func agentsDir() (string, error) {
	if h := os.Getenv("PACTIFY_HOME"); h != "" {
		return filepath.Join(h, ".pactify", "agents"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".pactify", "agents"), nil
}

// PathFor returns the manifest file path for a kind (~/.pactify/agents/<kind>.toml).
func PathFor(kind string) (string, error) {
	dir, err := agentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, kind+".toml"), nil
}

// toExternal maps a validated manifest onto the agent.External DTO.
func (m Manifest) toExternal() agent.External {
	e := agent.External{Kind: m.Kind, Entry: m.Entry, Binary: m.Binary}
	if m.MCP != nil {
		e.HasMCP = true
		e.MCPConfigPath = m.MCP.ConfigPath
		e.MCPScope, _ = agent.ParseScope(m.MCP.Scope)
		e.MCPFormat, _ = agent.ParseFormat(m.MCP.Format)
	}
	if m.Runner != nil {
		r := *m.Runner
		rp := agent.RunnerProfile{
			Command: m.Binary, DefaultModel: r.DefaultModel, Models: r.Models,
			BuildArgs: func(model string, posture agent.PermPosture, briefing string) []string {
				return RenderArgs(r.Args, r.Permission, posture, model, briefing)
			},
		}
		e.Runner = &rp
	}
	return e
}

// Load reads every ~/.pactify/agents/*.toml, parses + validates each, returning the
// valid manifests and a warning per skipped (unreadable/invalid) file.
func Load() (manifests []Manifest, warnings []string) {
	dir, err := agentsDir()
	if err != nil {
		return nil, []string{"agentmanifest: " + err.Error()}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // no manifests dir → nothing to load (not an error)
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".toml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", ent.Name(), err))
			continue
		}
		m, err := Parse(b)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", ent.Name(), err))
			continue
		}
		if errs := Validate(m); len(errs) != 0 {
			warnings = append(warnings, fmt.Sprintf("%s: %s", ent.Name(), strings.Join(errs, "; ")))
			continue
		}
		manifests = append(manifests, m)
	}
	return manifests, warnings
}

// Install validates a manifest and writes it to ~/.pactify/agents/<kind>.toml.
func Install(b []byte) (string, error) {
	m, err := Parse(b)
	if err != nil {
		return "", err
	}
	if errs := Validate(m); len(errs) != 0 {
		return "", fmt.Errorf("invalid manifest: %s", strings.Join(errs, "; "))
	}
	p, err := PathFor(m.Kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return m.Kind, os.WriteFile(p, b, 0o644)
}

// Remove deletes a custom manifest by kind (refuses built-ins).
func Remove(kind string) error {
	if builtinSet[kind] {
		return fmt.Errorf("%q is a built-in kind", kind)
	}
	p, err := PathFor(kind)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no custom manifest for %q", kind)
		}
		return err
	}
	return nil
}

// LoadAndRegister loads valid manifests and registers them (add-only), returning
// all warnings (invalid files + registration collisions). Never fatal.
func LoadAndRegister() []string {
	manifests, warnings := Load()
	for _, m := range manifests {
		if err := agent.RegisterExternal(m.toExternal()); err != nil {
			warnings = append(warnings, err.Error())
		}
	}
	return warnings
}

// Manifest is a parsed custom-agent TOML manifest.
type Manifest struct {
	Kind     string `toml:"kind"`
	Binary   string `toml:"binary"`
	Entry    string `toml:"entry"`
	Identity struct {
		Via string `toml:"via"` // "" | "env" | "arg"
	} `toml:"identity"`
	MCP *struct {
		ConfigPath string `toml:"config_path"`
		Scope      string `toml:"scope"`
		Format     string `toml:"format"`
	} `toml:"mcp"`
	Runner *struct {
		Args         []string   `toml:"args"`
		DefaultModel string     `toml:"default_model"`
		Models       []string   `toml:"models"`
		Permission   Permission `toml:"permission"`
	} `toml:"runner"`
}

// Parse decodes manifest TOML with strict (unknown-key-rejecting) decoding.
func Parse(b []byte) (Manifest, error) {
	var m Manifest
	dec := toml.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("manifest TOML: %w", err)
	}
	return m, nil
}

var kindRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// builtinSet is the fixed set of compiled-in kinds (manifests may not shadow them).
var builtinSet = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range agent.Kinds() {
		m[k] = true
	}
	return m
}()

// Validate returns all rule violations (empty = valid).
func Validate(m Manifest) []string {
	var errs []string
	add := func(s string) { errs = append(errs, s) }

	switch {
	case m.Kind == "":
		add("kind: required")
	case !kindRe.MatchString(m.Kind):
		add("kind: must match [a-z0-9-]+")
	case builtinSet[m.Kind]:
		add(fmt.Sprintf("kind %q: collides with a built-in kind (add-only)", m.Kind))
	}
	if m.Binary == "" {
		add("binary: required")
	}
	if strings.Contains(m.Entry, "/") || strings.Contains(m.Entry, "..") {
		add("entry: must be a bare filename (no / or ..)")
	}
	if m.MCP != nil {
		if _, ok := agent.ParseFormat(m.MCP.Format); !ok {
			add("mcp.format: must be one of mcpServers|opencode|toml")
		}
		if _, ok := agent.ParseScope(m.MCP.Scope); !ok {
			add("mcp.scope: must be project|global")
		}
		if m.MCP.ConfigPath == "" {
			add("mcp.config_path: required when [mcp] is present")
		}
	}
	if m.Runner != nil {
		if n := count(m.Runner.Args, "{briefing}"); n != 1 {
			add("runner.args: must contain exactly one {briefing}")
		}
		if m.Identity.Via == "arg" && count(m.Runner.Args, "{seat}") == 0 {
			add("runner.args: must contain {seat} when identity.via=arg")
		}
	} else if m.Identity.Via == "arg" {
		add("identity.via=arg requires a [runner] with {seat}")
	}
	return errs
}

func count(xs []string, want string) int {
	n := 0
	for _, x := range xs {
		if x == want {
			n++
		}
	}
	return n
}
