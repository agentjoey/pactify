package orchestrate

import (
	"context"
	"os/exec"
)

// shellExec is the production gate executor: it runs a verify command through the
// shell in dir and reports the exit code + combined output. A non-zero exit is
// reported as a code (the command ran but failed), distinct from a spawn error.
type shellExec struct{}

func (shellExec) Run(ctx context.Context, dir, command string, env map[string]string) (int, string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = filteredEnviron()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out), nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out), nil // ran, non-zero — not a spawn failure
	}
	return -1, string(out), err // could not spawn the command at all
}

// withDefaults fills nil collaborators with their production implementations, so
// the CLI only has to set Dir / Feature / Th / SeatKind. The unexported cmdExec
// interface cannot be constructed from outside this package, so defaulting it
// here is the only way the command layer can use the real gate executor.
func (opts Options) withDefaults() Options {
	if opts.Run == nil {
		opts.Run = NewCmdRunner(opts.IdleTimeout)
	}
	if opts.Exec == nil {
		opts.Exec = shellExec{}
	}
	if opts.Notify == nil {
		opts.Notify = StdoutNotifier{}
	}
	// Default the pre-review fix-until-green bound (spec §1 WS-F). 0-unset ==
	// 0-explicit here (same shape as the Thresholds), so the default stands unless a
	// caller sets a positive bound; the CLI plumbs --max-fix-rounds.
	if opts.MaxFixRounds == 0 {
		opts.MaxFixRounds = 2
	}
	// Wire the real session-management runner only when cleanup is enabled, so the
	// loop never spawns a session CLI unless the operator asked for it (and tests,
	// which leave both unset, stay hermetic).
	if opts.CleanupSessions && opts.SessionRun == nil {
		opts.SessionRun = execSessionRun
	}
	return opts
}

// execSessionRun is the production session-CLI runner: spawn the binary in dir
// and return combined output. dir is the agent's repo/worktree — required because
// opencode scopes its session store to the cwd, so cleanup must list/delete in the
// same dir the agent ran in (empty inherits the process cwd). Used by accept-time
// session cleanup.
func execSessionRun(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// DefaultTransportModes returns the default per-kind transport routing.
//
// A/B verdict (orchestrator 2026-07-09, two isomorphic projects, full round):
// ACP delivered the same result as cmd, recorded TOK usage in tokens.json, and
// produced 18 audit records vs 0 for cmd. Equal delivery + better telemetry +
// built-in audit → opencode defaults to ACP. The cmd transport remains available
// via --transport opencode=cmd as an escape hatch.
func DefaultTransportModes() map[string]string {
	return map[string]string{"opencode": "acp"}
}
