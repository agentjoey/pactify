// Package agentmanifest loads user-declared custom-agent manifests
// (~/.pactify/agents/*.toml) and maps them onto the agent registry (add-only).
package agentmanifest

import (
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
)

// Permission is the manifest's [runner.permission] block: the arg fragments
// {permission} expands to per posture. Scoped fragments may contain {tools}.
type Permission struct {
	Blanket []string `toml:"blanket"`
	Scoped  []string `toml:"scoped"`
}

// RenderArgs substitutes the manifest argv placeholders, producing the same kind
// of []string the built-in RunnerProfile.BuildArgs closures produce:
//
//	{briefing}   → briefing
//	{model}      → model, OR dropped (with a directly-preceding lone -m/--model)
//	             when model == "" (cleared/default model runs without an empty -m)
//	{permission} → the blanket OR scoped fragment (spliced; {tools} comma-joined)
//	{seat}       → left literal (the orchestrate runner substitutes lc.Seat at exec)
//	{repoDir}    → left literal (the orchestrate runner substitutes lc.RepoDir at exec)
func RenderArgs(tmpl []string, perm Permission, posture agent.PermPosture, model, briefing string) []string {
	out := make([]string, 0, len(tmpl)+len(perm.Blanket)+len(perm.Scoped))
	for _, tok := range tmpl {
		switch tok {
		case "{briefing}":
			out = append(out, briefing)
		case "{model}":
			if model == "" {
				if n := len(out); n > 0 && (out[n-1] == "-m" || out[n-1] == "--model") {
					out = out[:n-1]
				}
				continue
			}
			out = append(out, model)
		case "{permission}":
			frag := perm.Blanket
			if posture.Scoped {
				frag = perm.Scoped
			}
			tools := strings.Join(posture.AllowedTools, ",")
			for _, f := range frag {
				out = append(out, strings.ReplaceAll(f, "{tools}", tools))
			}
		default:
			out = append(out, tok)
		}
	}
	return out
}
