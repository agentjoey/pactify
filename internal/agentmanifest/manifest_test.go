package agentmanifest

import (
	"strings"
	"testing"
)

const validTOML = `
kind = "myagent"
binary = "myagent"
entry = "AGENTS.md"

[mcp]
config_path = ".myagent/mcp.json"
scope = "project"
format = "mcpServers"

[runner]
args = ["run", "-m", "{model}", "{permission}", "{briefing}"]
default_model = "m1"
models = ["m1", "m2"]

[runner.permission]
blanket = ["--yolo"]
`

func TestParseAndValidateOK(t *testing.T) {
	m, err := Parse([]byte(validTOML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if errs := Validate(m); len(errs) != 0 {
		t.Fatalf("validate errors: %v", errs)
	}
	if m.Kind != "myagent" || m.Runner.DefaultModel != "m1" || len(m.Runner.Models) != 2 {
		t.Fatalf("bad manifest: %+v", m)
	}
}

func TestParseRejectsUnknownKey(t *testing.T) {
	if _, err := Parse([]byte("kind=\"x\"\nbinary=\"x\"\nbogus=1\n")); err == nil {
		t.Fatal("unknown key must error (strict decode)")
	}
}

func TestValidateViolations(t *testing.T) {
	cases := []struct{ name, toml, wantSub string }{
		{"no kind", `binary="x"`, "kind"},
		{"bad kind chars", "kind=\"My_Agent\"\nbinary=\"x\"", "kind"},
		{"builtin kind", "kind=\"opencode\"\nbinary=\"x\"", "built-in"},
		{"no binary", `kind="x"`, "binary"},
		{"entry traversal", "kind=\"x\"\nbinary=\"x\"\nentry=\"../e\"", "entry"},
		{"bad format", "kind=\"x\"\nbinary=\"x\"\n[mcp]\nconfig_path=\"a\"\nscope=\"project\"\nformat=\"bad\"", "format"},
		{"bad scope", "kind=\"x\"\nbinary=\"x\"\n[mcp]\nconfig_path=\"a\"\nscope=\"bad\"\nformat=\"toml\"", "scope"},
		{"runner no briefing", "kind=\"x\"\nbinary=\"x\"\n[runner]\nargs=[\"run\"]", "briefing"},
		{"arg-identity needs seat", "kind=\"x\"\nbinary=\"x\"\n[identity]\nvia=\"arg\"\n[runner]\nargs=[\"run\",\"{briefing}\"]", "seat"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Parse([]byte(c.toml))
			if err != nil {
				if !strings.Contains(err.Error(), c.wantSub) {
					t.Fatalf("parse err %v, want sub %q", err, c.wantSub)
				}
				return
			}
			errs := Validate(m)
			joined := strings.Join(errs, "; ")
			if !strings.Contains(joined, c.wantSub) {
				t.Fatalf("validate errs %q, want sub %q", joined, c.wantSub)
			}
		})
	}
}
