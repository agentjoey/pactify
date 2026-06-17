package agentmanifest

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
	toml "github.com/pelletier/go-toml/v2"
)

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
