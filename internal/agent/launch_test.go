package agent

import (
	"reflect"
	"testing"
)

// The default (blanket-approve, default-model) output of each drivable kind's
// launch profile MUST byte-for-byte equal the historical hardcoded Runner() args
// — that contract is what TestRunnerCLIKinds locks, and what the gemini -p
// ordering fix depends on. These tests pin the profile builders to that baseline.
func TestLaunchProfile_DefaultsMatchRunner(t *testing.T) {
	cases := []struct {
		kind string
		cmd  string
		args []string
	}{
		{"opencode", "opencode", []string{"run", "-m", "deepseek/deepseek-v4-pro", "{briefing}"}},
		{"claude-code", "claude", []string{"-p", "--dangerously-skip-permissions", "--model", "claude-opus-4-8", "{briefing}"}},
		{"gemini-cli", "gemini", []string{"-p", "{briefing}", "-m", "gemini-3.1-pro-preview", "--approval-mode", "yolo", "--skip-trust"}},
		{"kimi-cli", "kimi", []string{"-p", "{briefing}", "-y", "-m", "kimi-for-coding"}},
		{"codex-cli", "codex", []string{"exec", "--sandbox", "workspace-write", "{briefing}"}},
	}
	for _, c := range cases {
		p, ok := RunnerProfileFor(c.kind)
		if !ok {
			t.Fatalf("%s: RunnerProfileFor returned ok=false", c.kind)
		}
		if p.Command != c.cmd {
			t.Errorf("%s: Command = %q, want %q", c.kind, p.Command, c.cmd)
		}
		got := p.BuildArgs(p.DefaultModel, PermPosture{}, "{briefing}")
		if !reflect.DeepEqual(got, c.args) {
			t.Errorf("%s: default BuildArgs = %v, want %v", c.kind, got, c.args)
		}
	}
}

// A model override replaces the default model in the rendered args, while the
// rest of the arg structure (and crucially gemini's briefing-after-`-p` ordering)
// is preserved.
func TestLaunchProfile_ModelOverride(t *testing.T) {
	p, _ := RunnerProfileFor("gemini-cli")
	got := p.BuildArgs("gemini-2.5-flash", PermPosture{}, "do it")
	want := []string{"-p", "do it", "-m", "gemini-2.5-flash", "--approval-mode", "yolo", "--skip-trust"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("gemini model override = %v, want %v", got, want)
	}
}

// Scoped posture swaps blanket auto-approve for an explicit allowed-tools list:
// claude trades --dangerously-skip-permissions for --allowedTools; gemini trades
// --approval-mode yolo for --approval-mode default + --allowed-tools.
func TestLaunchProfile_ScopedPosture(t *testing.T) {
	pc, _ := RunnerProfileFor("claude-code")
	gotC := pc.BuildArgs("claude-opus-4-8", PermPosture{Scoped: true, AllowedTools: []string{"Read", "Edit", "Bash"}}, "B")
	wantC := []string{"-p", "--allowedTools", "Read,Edit,Bash", "--model", "claude-opus-4-8", "B"}
	if !reflect.DeepEqual(gotC, wantC) {
		t.Errorf("claude scoped = %v, want %v", gotC, wantC)
	}

	pg, _ := RunnerProfileFor("gemini-cli")
	gotG := pg.BuildArgs("gemini-3.1-pro-preview", PermPosture{Scoped: true, AllowedTools: []string{"ReadFile", "Edit"}}, "B")
	wantG := []string{"-p", "B", "-m", "gemini-3.1-pro-preview", "--approval-mode", "default", "--allowed-tools", "ReadFile,Edit", "--skip-trust"}
	if !reflect.DeepEqual(gotG, wantG) {
		t.Errorf("gemini scoped = %v, want %v", gotG, wantG)
	}
}

// opencode has no permission concept: posture is ignored, model still applies.
func TestLaunchProfile_OpencodeIgnoresPosture(t *testing.T) {
	p, _ := RunnerProfileFor("opencode")
	got := p.BuildArgs(p.DefaultModel, PermPosture{Scoped: true, AllowedTools: []string{"x"}}, "B")
	want := []string{"run", "-m", "deepseek/deepseek-v4-pro", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("opencode = %v, want %v", got, want)
	}
}

// Non-drivable kinds have no profile.
func TestLaunchProfile_NonDrivable(t *testing.T) {
	for _, k := range []string{"antigravity", "claude-desktop", "codex-app", "no-such"} {
		if _, ok := RunnerProfileFor(k); ok {
			t.Errorf("%s: expected no runner profile, got ok=true", k)
		}
	}
}

// CandidateModels returns each drivable kind's curated list (default first),
// nil for kinds with no curated list or no profile.
func TestCandidateModels(t *testing.T) {
	got := CandidateModels("claude-code")
	want := []string{"claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("claude-code candidates = %v, want %v", got, want)
	}
	// DefaultModel must appear in the curated list for kinds that pin one.
	for _, k := range []string{"opencode", "claude-code", "gemini-cli", "kimi-cli"} {
		p, _ := RunnerProfileFor(k)
		if p.DefaultModel == "" {
			continue
		}
		models := CandidateModels(k)
		found := false
		for _, m := range models {
			if m == p.DefaultModel {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: DefaultModel %q not in candidates %v", k, p.DefaultModel, models)
		}
	}
	// Returned slice is a copy — mutating it must not corrupt the profile.
	c := CandidateModels("claude-code")
	c[0] = "MUTATED"
	if CandidateModels("claude-code")[0] != "claude-opus-4-8" {
		t.Error("CandidateModels returned a shared slice; mutation leaked")
	}
	// codex-cli pins no default and curates nothing → nil.
	if got := CandidateModels("codex-cli"); got != nil {
		t.Errorf("codex-cli candidates = %v, want nil", got)
	}
	// Non-drivable / unknown → nil.
	if got := CandidateModels("antigravity"); got != nil {
		t.Errorf("antigravity candidates = %v, want nil", got)
	}
}
