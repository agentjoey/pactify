package agent

import "strings"

// PermPosture is the permission stance a headless agent runs under. The default
// (zero value, Scoped=false) is blanket auto-approve — the only posture verified
// to let an agent develop/review without a human approving each tool call. Scoped
// trades that blanket grant for an explicit AllowedTools allowlist (#4 scoped
// permissions): safer, but the caller must enumerate every tool the task needs.
type PermPosture struct {
	Scoped       bool
	AllowedTools []string
}

// RunnerProfile is the parametric headless-launch profile for a drivable kind. It
// separates the launch concern (model + permission posture + briefing → argv)
// from the MCP-wiring metadata held by spec. BuildArgs renders the full argument
// list; pass the literal "{briefing}" placeholder (orchestrate substitutes it
// later) or the real prompt text directly.
type RunnerProfile struct {
	Command      string
	DefaultModel string
	// Models is the curated list of known/recommended model IDs for this kind,
	// surfaced as the agent-config model dropdown (the UI always keeps a
	// "custom…" escape hatch, so this need not be exhaustive). DefaultModel,
	// when non-empty, is expected to appear in the list.
	Models    []string
	BuildArgs func(model string, perm PermPosture, briefing string) []string
	// EffortArgs renders this CLI's reasoning-effort flag(s). nil means the kind
	// has no effort control — its launch stays byte-for-byte unchanged. It is a
	// pure side channel: agentcfg appends EffortArgs(effort) AFTER BuildArgs, so
	// BuildArgs' default output (locked by launch_test/agent_test) is untouched.
	EffortArgs func(effort string) []string
}

// runnerProfiles holds the per-kind launch builders for the three verified
// headless kinds. Each builder's DEFAULT output (default model, blanket posture)
// is byte-for-byte the historical hardcoded Runner() args — locked by
// launch_test and agent_test. The variability that differs per CLI (flag syntax,
// argument ordering — e.g. gemini requires the briefing to immediately follow
// -p) lives inside each builder, where it can be read and tested in one place.
var runnerProfiles = map[string]RunnerProfile{
	// opencode has no permission concept (it runs tools directly); posture is
	// ignored. -m pins the model.
	"opencode": {
		Command:      "opencode",
		DefaultModel: "deepseek/deepseek-v4-pro",
		Models:       []string{"deepseek/deepseek-v4-pro"},
		BuildArgs: func(model string, _ PermPosture, briefing string) []string {
			return []string{"run", "-m", model, briefing}
		},
	},
	// claude-code: -p is headless. Blanket → --dangerously-skip-permissions;
	// scoped → --allowedTools <csv> (no blanket grant). --model pins the model.
	"claude-code": {
		Command:      "claude",
		DefaultModel: "claude-opus-4-8",
		Models:       []string{"claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5"},
		BuildArgs: func(model string, perm PermPosture, briefing string) []string {
			// --no-session-persistence: the cmd-transport WORKER/REVIEWER stint is a
			// one-shot "execute the task and exit" that pactify never resumes, so not
			// writing a transcript to disk means there is nothing to clean up (claude
			// has no per-session delete CLI — this avoids accumulation entirely,
			// cleaner than opencode's create-then-delete). The interactive
			// deep-integration path (cockpit, which DOES resume) is separate and does
			// not use this BuildArgs. Requires --print, which -p is.
			args := []string{"-p", "--no-session-persistence"}
			if perm.Scoped {
				args = append(args, "--allowedTools", strings.Join(perm.AllowedTools, ","))
			} else {
				args = append(args, "--dangerously-skip-permissions")
			}
			return append(args, "--model", model, briefing)
		},
		// claude exposes --effort <level> (verified `claude --help` 2026-08:
		// "Effort level for the current session"; levels low/medium/high).
		EffortArgs: func(effort string) []string {
			return []string{"--effort", effort}
		},
	},
	// gemini-cli: the briefing MUST immediately follow -p (else -p swallows the
	// next flag). Blanket → --approval-mode yolo; scoped → --approval-mode default
	// + --allowed-tools <csv>. --skip-trust trusts the workspace either way.
	"gemini-cli": {
		Command:      "gemini",
		DefaultModel: "gemini-3.1-pro-preview",
		Models:       []string{"gemini-3.1-pro-preview", "gemini-3-flash-preview"},
		BuildArgs: func(model string, perm PermPosture, briefing string) []string {
			args := []string{"-p", briefing, "-m", model}
			if perm.Scoped {
				args = append(args, "--approval-mode", "default", "--allowed-tools", strings.Join(perm.AllowedTools, ","))
			} else {
				args = append(args, "--approval-mode", "yolo")
			}
			return append(args, "--skip-trust")
		},
	},
	// kimi-cli: -p runs one prompt non-interactively and ALREADY auto-approves
	// every tool (verified on kimi-code 0.18: `kimi -p "<task>"` created a file
	// unattended). kimi-code 0.18 actively REJECTS approval flags in -p mode
	// ("Cannot combine --prompt with --yolo/--auto"), so -y must NOT be passed — it
	// was a v1.44-ism that breaks the worker on 0.18. -m pins the model; kimi has no
	// per-tool allowlist, so a scoped posture can't be expressed (always blanket,
	// like opencode). NB: the rich/streaming path for kimi 0.18 is ACP (`kimi acp`)
	// — see docs/backlog; this headless -p form is the minimal worker.
	// EffortArgs stays nil: this kimi-code build exposes NO reasoning-effort /
	// thinking flag (verified `kimi --help` 2026-08 — no --effort/--thinking/
	// --no-thinking), so the safe default is a byte-unchanged launch rather than
	// a guessed flag name.
	"kimi-cli": {
		Command:      "kimi",
		DefaultModel: "kimi-code/kimi-for-coding",
		Models:       []string{"kimi-code/kimi-for-coding"},
		BuildArgs: func(model string, _ PermPosture, briefing string) []string {
			return []string{"-p", briefing, "-m", model}
		},
	},
	// codex-cli: `codex exec` is headless. Blanket (the orchestrate default) maps
	// to --sandbox danger-full-access — codex's workspace-write sandbox
	// specifically protects `.git/` (it permits `git` commands but blocks direct
	// writes into .git/), which blocks pactify's base merge lock
	// (.git/pactify-base.lock via lockx) and the tracked-ledger auto-commit, so a
	// worker/orchestrator can't advance pact/git state (confirmed codex 0.142.5;
	// --add-dir .git does NOT override it). Full-access matches the full-trust
	// posture the other blanket agents get (claude --dangerously-skip-permissions,
	// gemini --approval-mode yolo). Scoped keeps workspace-write (explicit opt-in;
	// note scoped codex cannot take the .git base lock, so it cannot merge). codex
	// has no per-tool allowlist, so AllowedTools is not expressible. -m omitted
	// when no model is pinned → codex uses its configured default.
	"codex-cli": {
		Command:      "codex",
		DefaultModel: "",
		BuildArgs: func(model string, perm PermPosture, briefing string) []string {
			sandbox := "danger-full-access"
			if perm.Scoped {
				sandbox = "workspace-write"
			}
			args := []string{"exec", "--json", "--sandbox", sandbox}
			if model != "" {
				args = append(args, "-m", model)
			}
			return append(args, briefing)
		},
		// codex takes reasoning effort as a -c config override:
		// `-c model_reasoning_effort=<low|medium|high>` (codex exec config key).
		EffortArgs: func(effort string) []string {
			return []string{"-c", "model_reasoning_effort=" + effort}
		},
	},
}

// RunnerProfileFor returns the launch profile for a drivable kind; ok=false for
// kinds with no verified headless runner (GUI/desktop, unknown).
func RunnerProfileFor(kind string) (RunnerProfile, bool) {
	p, ok := runnerProfiles[kind]
	return p, ok
}

// CandidateModels returns the curated model IDs for a kind, for the agent-config
// model dropdown. Empty for kinds with no runner profile or no curated list
// (the UI then falls back to a free-text model field). The returned slice is a
// copy, safe for the caller to mutate.
func CandidateModels(kind string) []string {
	p, ok := runnerProfiles[kind]
	if !ok || len(p.Models) == 0 {
		return nil
	}
	out := make([]string, len(p.Models))
	copy(out, p.Models)
	return out
}

// Drivable reports whether a kind can be launched headlessly (#8): true iff it
// has a runner profile. Surfaced in scan/registry so users see which agents
// orchestrate can drive autonomously vs which need a human/escalation hand-off.
func Drivable(kind string) bool {
	_, ok := runnerProfiles[kind]
	return ok
}
