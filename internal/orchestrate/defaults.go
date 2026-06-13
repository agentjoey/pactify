package orchestrate

import (
	"context"
	"os/exec"
)

// shellExec is the production gate executor: it runs a verify command through the
// shell in dir and reports the exit code + combined output. A non-zero exit is
// reported as a code (the command ran but failed), distinct from a spawn error.
type shellExec struct{}

func (shellExec) Run(ctx context.Context, dir, command string) (int, string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
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
	return opts
}
