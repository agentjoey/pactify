// Package agentcfg resolves the effective headless-launch configuration for an
// agent kind: it overlays per-agent overrides (model, scoped permissions) from
// the machine agent registry on top of the kind's built-in RunnerProfile. This
// is the seam that decouples orchestrate's launcher from hardcoded model pins and
// permission postures (#10 per-agent model, #9 permission posture, #4 scoped
// permissions).
package agentcfg

import (
	"fmt"

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

// KindSource is the PROVENANCE of the kind handed to ResolveSeatFrom. It exists
// because provenance cannot be recovered from the string itself: the driver's
// opts.kind() returns the operator's `--seat-kind` value when there is one and
// the live roster's value otherwise, and both arrive here as the same
// `"claude-code"`. Without this parameter the resolver cannot tell "the operator
// typed it" from "the roster happens to say it", and those two cases must
// resolve differently against a bound role profile (KIND-2).
type KindSource int

const (
	// KindFromRoster: the kind was derived from configuration — the ledger
	// roster's Agents[].Kind, a role-derived default, or any other fallback. A
	// bound role profile's Kind OUTRANKS it; that precedence is the entire point
	// of role binding and is unchanged by KIND-2.
	KindFromRoster KindSource = iota
	// KindExplicit: the kind is the operator's explicit `--seat-kind seat=kind`
	// flag. It OUTRANKS a bound role profile's Kind — a typed flag beats stored
	// configuration, which is the CLI convention orchestrate's Options.SeatKind
	// has always documented. Displacing a DIFFERENT profile kind also yields a
	// warning so the override is never silent.
	KindExplicit
)

// ResolveSeat resolves the launch profile for a SEAT rather than just a kind,
// treating kind as roster-provenance (KindFromRoster). Callers that hold the
// operator's explicit `--seat-kind` must use ResolveSeatFrom so the flag can win
// and its warning can be surfaced.
func ResolveSeat(seat, kind, tierEffort string) (Effective, bool) {
	eff, _, ok := ResolveSeatFrom(seat, kind, KindFromRoster, tierEffort)
	return eff, ok
}

// ResolveSeatFrom is ResolveSeat with the kind's provenance made explicit.
//
// When the seat is bound to a role, that role's profile normally decides both
// the agent kind and the model — which is what lets two seats of the same kind
// run different models (per-kind Resolve cannot express that). An unbound seat,
// or a binding whose role was deleted, falls through to the pre-roles behavior
// so an unconfigured machine is unaffected.
//
// Kind precedence (highest first):
//  1. an EXPLICIT operator kind (src == KindExplicit, kind non-empty) — a typed
//     `--seat-kind` flag beats stored configuration;
//  2. the bound role profile's Kind;
//  3. the passed-in kind (roster/default).
//
// When (1) displaces a different (2), the profile's MODEL pin is dropped and the
// overriding kind's own registry/built-in default applies: a model name is
// vendor-specific (`gemini-3.7-flash-medium` handed to `claude --model` just
// hard-fails), so a pin chosen for the displaced kind must not travel across the
// override. The profile's Effort pin DOES survive — it is a vendor-neutral tier
// word, and a per-seat effort pin stays absolute.
//
// tierEffort is the reasoning-effort budget the caller derived from the task's
// tier (execution-tiering §4.5); "" means "no effort injection". A role
// profile's EXPLICIT Effort always wins over tierEffort — an operator's
// per-seat setting must remain absolute.
//
// The permission posture stays per-kind: it is a machine trust decision about
// an agent binary, not a property of the role someone plays.
//
// The second return value is an operator-facing warning, non-empty ONLY when an
// explicit kind actually displaced a different profile kind (an override that
// agrees with the profile, or lands on an unbound seat, displaces nothing and
// stays silent). agentcfg is a pure library with no output channel of its own,
// so the caller decides where the warning surfaces.
// SeatModelPin reports the model a seat's role binding pins, and the role that
// pins it. ok=false when the seat is unbound or its profile names no model.
//
// It exists for transports that CANNOT honor the pin: they must be able to say
// so instead of silently running on the vendor's own default ([ACP-MODEL]).
// Callers that can honor it should go through ResolveSeatFrom, which returns the
// whole launch configuration rather than this one field.
func SeatModelPin(seat string) (model, role string, ok bool) {
	cfg, err := roles.Load()
	if err != nil {
		return "", "", false
	}
	p, r, found := cfg.Lookup(seat)
	if !found || p.Model == "" {
		return "", "", false
	}
	return p.Model, r, true
}

func ResolveSeatFrom(seat, kind string, src KindSource, tierEffort string) (Effective, string, bool) {
	if cfg, err := roles.Load(); err == nil {
		if p, role, ok := cfg.Lookup(seat); ok {
			k, model, warning, displaced := p.Kind, p.Model, "", false
			switch {
			case k == "":
				// Kind-agnostic profile: it decorates whatever kind the caller
				// supplies, so its model pin still describes the launching kind.
				k = kind
			case src == KindExplicit && kind != "" && kind != p.Kind:
				// KIND-2: the operator typed a kind for this seat and it differs
				// from the binding. Honor the flag, drop the now cross-vendor
				// model pin, and say so.
				k, model, displaced = kind, "", true
				warning = fmt.Sprintf(
					"seat %s: explicit --seat-kind %s=%s overrides role %q (kind %s); the role's model pin is dropped and %s's own defaults apply",
					seat, seat, kind, role, p.Kind, kind)
			}
			effort := tierEffort
			if p.Effort != "" {
				effort = p.Effort
			}
			reg, _ := agentreg.Load()
			regModel, tools, restricted := reg.Config(k)
			if displaced {
				// The profile's pin named the DISPLACED kind's model, so this
				// launch takes the overriding kind's own configuration: its
				// registry override, else "" → ResolveWith's built-in default.
				// Exactly what an unbound seat of that kind would get. Only the
				// displaced branch does this — an unpinned model on a profile
				// that was NOT displaced keeps its pre-KIND-2 meaning (built-in
				// default, registry model deliberately not consulted).
				model = regModel
			}
			e, ok := ResolveWith(k, Override{Model: model, AllowedTools: tools, Restricted: restricted, Effort: effort})
			return e, warning, ok
		}
	}
	// Per-kind path (unbound seat): same registry overlay as Resolve, plus the
	// tier-derived budget. ResolveWith no-ops an empty Effort, so a "" tierEffort
	// keeps this byte-for-byte identical to Resolve(kind). No binding exists, so
	// an explicit kind displaces nothing and there is nothing to warn about.
	reg, _ := agentreg.Load()
	model, tools, restricted := reg.Config(kind)
	e, ok := ResolveWith(kind, Override{Model: model, AllowedTools: tools, Restricted: restricted, Effort: tierEffort})
	return e, "", ok
}
