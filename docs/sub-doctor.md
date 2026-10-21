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
access for dependency downloads and tests. Codex and agy read-only probes must
actually invoke tools; claiming that a write was denied does not pass.

Kimi read-only probes must succeed, leave `probe.txt` absent, and have every
agent's `llm.tools_snapshot` within the generated private config's tool allowlist:
Read, Grep, Glob, ReadMediaFile, TodoList, TaskList, TaskOutput and WaitFor.
Unexpected tools or missing snapshots fail the check. The execution-time policy
disables shell, writes, sub-agents, cron, MCP and network tools; additional deny
rules cover Write, Edit, Bash, Agent*, Cron* and TaskStop. ACP uses default mode,
not plan mode. An unchanged repository baseline is required. Kimi has no OS sandbox
for writes; permission requests in write jobs are approved by pactify. Agy uses plan mode, managed hooks, argument-checked
commands and a repository baseline;
these controls do not provide an OS sandbox for writes. Opencode 1.x and 2.x
remove shell and edit tools from read-only sessions. Doctor reads the completed
job's own session permissions and requires shell, edit and external-directory
denials rather than asking a model to invoke removed tools. Repository changes
or an unverifiable baseline fail with `readonly_violation`.
For write jobs, doctor verifies the completed opencode session denies outside
directories and asks for in-root paths, which require the adapter's resolved-path
checks. It reads stored rules on 1.x and evaluates the session policy on 2.x.

## Configuration and isolated state

Probes follow `sub run`'s default minimal configuration. Ordinary jobs can opt in
to `--with-user-config`; doctor verifies the default path. Codex disables optional
MCP, plugins, skills and memory through native process-local overrides while
retaining authentication, providers, sandbox settings and repository instructions.
Kimi uses `$PACTIFY_HOME/harness-homes/kimi` for write jobs and
`$PACTIFY_HOME/harness-homes/kimi-readonly` for read-only jobs. Before each job,
pactify renders a private 0600 config from at most 1 MiB of user TOML. The only
copied top-level settings are `default_model`, `models` and `providers` (including
provider API-key environment references and OAuth storage/key references).
Generated permission/tools sections replace all execution settings; unknown
sections are omitted. Config contents are never logged. Concurrent jobs share
an identical regular 0600 config under shared leases. A changed or unsafe config
requires the home to be idle; otherwise startup fails with an in-use diagnostic.
The last job removes the generated config, including any provider API keys.
Credentials and OAuth remain directory links. `--with-user-config` is incompatible
with kimi `--read-only`; resume requires the original access and config modes.
Write jobs use a separate home and lock.

Snapshot evidence is required for success. Missing evidence on failed or canceled
jobs preserves their original status, reason and failure class, including quota
fallback. Observed non-allowlisted tools or repository writes still fail every
terminal path with `readonly_violation`. Evidence must come from the recorded
managed read-only home. Paths are canonicalized; large wire events are decoded
without a scanner line limit. Empty agents without an LLM call may have no wire;
an existing wire without a snapshot, or a known LLM call without a wire, fails.

Kimi 2.2.0's project-local `.kimi-code/local.toml` schema contains only
`workspace.additional_dir`; it cannot override the global tools policy. Project
MCP/plugin/agent tools remain subject to that policy. The tools section has no
environment binding. Pactify forces the dangerous-command guard on, clears
inherited workspace trust and experimental switches, and disables cron and
background keep-alive overrides for read-only jobs. Model and authentication
environment variables remain available. These findings were checked against the
installed binary's `FileProjectLocalConfigService`, `ToolsConfigSchema` and
`AgentToolPolicyService`, plus the upstream [config reference](https://moonshotai.github.io/kimi-code/llms-full.txt).

Agy uses a managed HOME, and opencode
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

Opencode 1.x and 2.x are supported; agy remains experimental. Promotion does not
change the Sandbox check: opencode still reports WARN because it has no OS
sandbox. Write probes retain the file-tool directory guard checks; shell commands
are not confined. See [support policy](harness-updates.md) for version contracts.
