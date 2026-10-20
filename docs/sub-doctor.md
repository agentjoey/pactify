# `pactify sub doctor`

```sh
pactify sub doctor                 # All registered harnesses
pactify sub doctor codex           # One harness
pactify sub doctor opencode --model provider/model
```

Doctor launches real harness requests and may consume quota. Install and log in
to the relevant CLI first. Codex and kimi use the harness's native default model
unless `--model` is supplied. Doctor still requires a nonempty, independently
verified actual model; explicitly requested models must match it. Opencode
prefers `defaults.opencode.model` from the router, then a native default reported
by model discovery, then its existing
name-based heuristic: models ending in `-free`, favoring flash/mini/lite/nano,
then other compact or non-pro names. Names are not a price guarantee.
Agy uses an explicit supported flash model because its probe profile requires
one. `--model` requires exactly one explicit harness and overrides default
selection; invalid or unverified models fail rather than silently falling back.

Explicit models use the same backend validation as `sub run --model`, including
model discovery where the backend requires it. Diagnostic candidate lists show
at most ten models, preferring the requested provider and similar names, with a
hint for listing the complete catalog. A read-only failure identical to the
write failure is shown as `same as above (Write access)`.

## Checks and results

Each harness has ten aligned rows. PASS/FAIL record checked outcomes; SKIP means
not applicable or not run after a prerequisite failure; WARN describes a
limitation. Storage diagnostics appear separately.

| Check | Evidence |
| --- | --- |
| Installed | Supported CLI version, captured before probes |
| Write access | Successful job, output file, final reply, usage and matching CLI version |
| Model | Actual model reported and independently verified; explicit requests must match |
| Read-only access | Successful tool use and absent write target, plus harness-specific denial evidence |
| Read-only hook | Agy hook must independently reject a write; otherwise not applicable |
| Network | Write probe must produce valid local-bind and external-HTTPS evidence |
| Sandbox | Native OS confinement for codex; a warning for harnesses without it |
| Failure classification | Offline injected protocol error with the expected class |
| Session hiding | Normal lifecycle hides probe sessions without concealment failures |
| Managed home | Harness-specific private storage and configuration checks |

Network probes use Python's standard library to bind a random loopback port and
request `https://example.com`. Missing Python or unreachable HTTPS fails this
check; a model's verbal claim is insufficient. Codex write mode enables network
access for dependency downloads and tests. The read-only probe must actually
invoke tools; claiming that a write was denied does not pass.

Kimi cannot enforce read-only access, so its read-only rows are skipped. Agy uses
plan mode, managed hooks, argument-checked commands and a repository baseline;
these controls do not provide an OS sandbox for writes. Opencode 1.x and 2.x
remove shell and edit tools from read-only sessions. Doctor reads the completed
job's own session permissions and requires shell, edit and external-directory
denials rather than asking a model to invoke removed tools. Repository changes
or an unverifiable baseline fail with `readonly_violation`.

## Configuration and isolated state

Probes follow `sub run`'s default minimal configuration. Ordinary jobs can opt in
to `--with-user-config`; doctor verifies the default path. Codex disables optional
MCP, plugins, skills and memory through native process-local overrides while
retaining authentication, providers, sandbox settings and repository instructions.
Kimi uses `$PACTIFY_HOME/harness-homes/kimi`, agy uses a managed HOME, and opencode
uses private XDG directories. Necessary authentication links are managed without
printing credential contents. Session stores remain private and persistent for
resume. Changed configuration copies are discarded with warnings; credential
refreshes follow the normal preservation/quarantine rules.

Temporary repositories and probe jobs are removed after completion, failed checks
and handled cancellation. Shared harness homes and build caches remain. Avoid
SIGKILL when graceful cancellation is available. Resume retains the recorded
configuration mode; records without `configMode` are treated as user config.

Only a harness that passes every applicable check updates
`$PACTIFY_HOME/verified-versions.json` (default `~/.pactify/verified-versions.json`).
Updates are atomic and locked; failed probes preserve previous successful records.
The table records version and UTC verification time, not continuing proof that
login, network or model services remain available. Ordinary jobs warn when a
version is unchecked or changed; these warnings do not change job status.

Exit codes: 0 for passed applicable checks, 1 for probe/infrastructure failure,
2 for invalid arguments or flags.

## Shared caches and disk space

| Child environment | Default directory |
| --- | --- |
| `GOCACHE` | `$PACTIFY_HOME/cache/go-build` |
| `GOMODCACHE` | `$PACTIFY_HOME/cache/go-mod` |
| `npm_config_cache` | `$PACTIFY_HOME/cache/npm` |

Explicit nonempty cache variables take precedence, including `GOCACHE=off`.
Codex write jobs probe cache writability before adding writable roots, falling
back to shared caches with a warning when needed. Other harnesses retain explicit
variables; read-only jobs gain no extra writable roots. These settings apply to
child processes without rewriting user configuration.

Each job owns `TMPDIR=$PACTIFY_HOME/jobs/<id>/tmp`, removed only after its
registered processes stop. Cleanup does not follow symlinks or take ownership of
preexisting directories. Shared caches are not removed with a job.

Storage warns below 5 GiB free or when disk/cache measurements are unavailable.
Cache size counts regular files without following symlinks. These warnings do
not fail harness verification. Stop all cache users before clearing caches;
use Go's module-cache cleanup for its read-only directories. User-specified
caches require separate management.

See [status](sub-status.md), [routing](router.md) and
[upstream compatibility](harness-updates.md). Live test coverage and its limits
are described in the [E2E guide](../internal/sub/e2e/README.md).
