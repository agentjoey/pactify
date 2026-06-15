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
			args := []string{"-p"}
			if perm.Scoped {
				args = append(args, "--allowedTools", strings.Join(perm.AllowedTools, ","))
			} else {
				args = append(args, "--dangerously-skip-permissions")
			}
			return append(args, "--model", model, briefing)
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
	// kimi-cli: -p supplies the prompt; -y is the blanket auto-approve. kimi has
	// no per-tool allowlist flag, so a scoped posture can't be expressed — posture
	// is ignored (always blanket), like opencode. -m pins the model. Verified
	// against the installed `kimi` v1.44.0 (--help + package source).
	"kimi-cli": {
		Command:      "kimi",
		DefaultModel: "kimi-for-coding",
		Models:       []string{"kimi-for-coding"},
		BuildArgs: func(model string, _ PermPosture, briefing string) []string {
			return []string{"-p", briefing, "-y", "-m", model}
		},
	},
	// codex-cli: `codex exec` is headless. The blanket posture maps to
	// --sandbox workspace-write — the worker can edit the repo tree without
	// per-command approval while staying sandboxed from network/system
	// (--dangerously-bypass-approvals-and-sandbox is the full-trust escalation,
	// not used by default). codex has no per-tool allowlist, so a scoped posture
	// can't be expressed; posture is otherwise ignored. -m is omitted when no
	// model is pinned, so codex falls back to its own configured default (the
	// current default model name isn't asserted here). Verified against
	// codex-cli v0.139.0 (codex exec --help).
	"codex-cli": {
		Command:      "codex",
		DefaultModel: "",
		BuildArgs: func(model string, _ PermPosture, briefing string) []string {
			args := []string{"exec", "--sandbox", "workspace-write"}
			if model != "" {
				args = append(args, "-m", model)
			}
			return append(args, briefing)
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
