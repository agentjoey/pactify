// Package agentcfg resolves the effective headless-launch configuration for an
// agent kind: it overlays per-agent overrides (model, scoped permissions) from
// the machine agent registry on top of the kind's built-in RunnerProfile. This
// is the seam that decouples orchestrate's launcher from hardcoded model pins and
// permission postures (#10 per-agent model, #9 permission posture, #4 scoped
// permissions).
package agentcfg

import (
	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentreg"
	"github.com/agentjoey/pactify/internal/roles"
)

// Placeholder is the briefing token the resolved Args carry; orchestrate
// substitutes the real prompt for it at launch time. Kept identical to the
// runner's substitution token so the seam stays consistent.
const Placeholder = "{briefing}"

// Override is a per-agent launch override (typically loaded from the machine
// registry). The zero value means "use the kind's built-in defaults".
type Override struct {
	Model        string
	AllowedTools []string
	Restricted   bool
	// Effort is the reasoning-effort budget: "", "low", "medium", "high".
	// "" = inject nothing (the CLI's own default stands). Only honored for kinds
	// whose RunnerProfile declares EffortArgs; other kinds launch unchanged.
	Effort string
}

// Effective is the resolved launch config for a kind: command + fully-resolved
// args (model + permission posture applied) carrying the briefing Placeholder.
// Model and Scoped are surfaced for display/telemetry (e.g. orchestrate status,
// scan output). Effort is the resolved reasoning-effort budget ("" = none
// injected), likewise surfaced for display/telemetry.
type Effective struct {
	Kind    string
	Command string
	Args    []string
	Model   string
	Scoped  bool
	Effort  string
}

// ResolveWith resolves the effective config for kind, overlaying the supplied
// override on the kind's RunnerProfile. ok=false for non-drivable kinds.
func ResolveWith(kind string, ov Override) (Effective, bool) {
	p, ok := agent.RunnerProfileFor(kind)
	if !ok {
		return Effective{}, false
	}
	model := p.DefaultModel
	if ov.Model != "" {
		model = ov.Model
	}
	perm := agent.PermPosture{}
	if ov.Restricted {
		perm = agent.PermPosture{Scoped: true, AllowedTools: ov.AllowedTools}
	}
	args := p.BuildArgs(model, perm, Placeholder)
	effort := ""
	// Reasoning effort is an APPEND-ONLY side channel (execution-tiering §4.5):
	// with no declared EffortArgs or an empty Effort, Args stay byte-for-byte
	// what BuildArgs rendered.
	if p.EffortArgs != nil && ov.Effort != "" {
		args = append(args, p.EffortArgs(ov.Effort)...)
		effort = ov.Effort
	}
	return Effective{
		Kind:    kind,
		Command: p.Command,
		Args:    args,
		Model:   model,
		Scoped:  ov.Restricted,
		Effort:  effort,
	}, true
}

// Resolve loads the per-kind override from the machine agent registry and
// resolves. A missing/unreadable registry yields the built-in defaults (the
// override is simply empty), so orchestrate never fails to launch just because
// no overrides were configured.
func Resolve(kind string) (Effective, bool) {
	reg, _ := agentreg.Load()
	model, tools, restricted := reg.Config(kind)
	return ResolveWith(kind, Override{Model: model, AllowedTools: tools, Restricted: restricted})
}

// ResolveSeat resolves the launch profile for a SEAT rather than just a kind.
// When the seat is bound to a role, that role's profile decides both the agent
// kind and the model — which is what lets two seats of the same kind run
// different models (per-kind Resolve cannot express that). An unbound seat, or
// a binding whose role was deleted, falls through to the pre-roles behavior so
// an unconfigured machine is unaffected.
//
// tierEffort is the reasoning-effort budget the caller derived from the task's
// tier (execution-tiering §4.5); "" means "no effort injection". A role
// profile's EXPLICIT Effort always wins over tierEffort — an operator's
// per-seat setting must remain absolute.
//
// The permission posture stays per-kind: it is a machine trust decision about
// an agent binary, not a property of the role someone plays.
func ResolveSeat(seat, kind, tierEffort string) (Effective, bool) {
	if cfg, err := roles.Load(); err == nil {
		if p, _, ok := cfg.Lookup(seat); ok {
			k := p.Kind
			if k == "" {
				k = kind
			}
			effort := tierEffort
			if p.Effort != "" {
				effort = p.Effort
			}
			reg, _ := agentreg.Load()
			_, tools, restricted := reg.Config(k)
			return ResolveWith(k, Override{Model: p.Model, AllowedTools: tools, Restricted: restricted, Effort: effort})
		}
	}
	// Per-kind path (unbound seat): same registry overlay as Resolve, plus the
	// tier-derived budget. ResolveWith no-ops an empty Effort, so a "" tierEffort
	// keeps this byte-for-byte identical to Resolve(kind).
	reg, _ := agentreg.Load()
	model, tools, restricted := reg.Config(kind)
	return ResolveWith(kind, Override{Model: model, AllowedTools: tools, Restricted: restricted, Effort: tierEffort})
}
