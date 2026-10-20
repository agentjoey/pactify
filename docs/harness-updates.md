# Upstream harness compatibility

Pactify tracks codex, kimi, opencode and agy contracts. Unsupported CLI versions
must fail before a harness process starts, with a clear explanation and recovery
suggestion. Track successor packages as well as the original installation channel:
a package's `latest` tag alone does not reveal a migration to a new package.

## Channels and baselines

| Harness | Official channel | Contract baseline | Weekly checks |
| --- | --- | --- | --- |
| codex | npm `@openai/codex` | `internal/cockpit/codexschema/CODEX_VERSION` | `codex-schema.yml`: latest schema fields |
| kimi | npm `@moonshot-ai/kimi-code` | `internal/cockpit/harnessdrift/versions.json` | `harness-drift.yml`: arguments and ACP handshake |
| opencode | npm `opencode-ai` (1.x), `@opencode/cli` (2.x) | `versions.json`: 1.19.0 / 2.1.0 | `opencode-api.yml`: pinned and latest OpenAPI contracts for both channels |
| agy | Installer manifest | `versions.json` | `harness-drift.yml`: arguments and stream events |

Local doctor verification is recorded in `$PACTIFY_HOME/verified-versions.json`.
Jobs warn when the CLI version changes or has not passed doctor checks.

## Monitoring and response

`harness-drift.yml` runs `internal/cockpit/harnessdrift/releases.py` weekly.
It reads public release metadata without installing CLIs or running inference.
An untracked major version, deprecated latest release or unparseable version
fails the check. Same-major updates appear in the summary; registry outages
are reported as skipped. Codex 0.x minor updates are guarded by schema checks.
Run the script locally when investigating upstream compatibility.

- Local job failure after an upgrade: identify the installed version, reproduce
  in an isolated environment, and repair support or document a working version.
- Same-major update with passing contracts: run `pactify sub doctor <harness>`
  after upgrading the local CLI.
- Contract drift: reproduce in isolated HOME, update the adapter, golden records
  and version baselines together, then run the checks below.
- New major version, package or deprecation: assess startup, protocol, events,
  permissions, read-only enforcement, configuration, authentication locations,
  session hiding, usage and model IDs. Decide whether to support it, explicitly
  reject it with `Harness.Supported`, or remove support for that harness.

Record the supported installation channels in `releases.py` (`CHANNELS` and
`TRACKED`). Keep `versions.json`, `CODEX_VERSION` and workflow pins consistent.
Assess migration effort, upstream maintenance windows, user impact and recovery
commands. Do not silently upgrade a contributor's installed CLI; use isolated
installs for compatibility experiments.

## Verification by increasing cost

1. Offline checks: `python3 internal/cockpit/harnessdrift/check.py --static` and
   Python unit tests beside the checker.
2. Isolated, non-inference probes: `check.py --harness <harness>` and opencode
   API probes. Check arguments, handshake and permissions without model calls.
3. Contract snapshots: codex schema and opencode OpenAPI projections.
4. Golden replay: `go test ./internal/cockpit/...`.
5. Local doctor: `pactify sub doctor <harness>`; an explicit `--model` overrides
   probe defaults. See [doctor](sub-doctor.md).
6. Live CLI lifecycle and isolation tests: follow the
   [E2E guide](../internal/sub/e2e/README.md), including its configurable model policy.
7. Representative long-running tasks that exercise multi-file changes, review
   and recovery, with concrete acceptance commands and a quota budget.

The opencode 2.x CI pin is 2.1.0. Golden transcripts retain their recorded
`v2.0.32` version as capture provenance; changing a CI pin does not re-record
traffic. The reviewed OpenAPI projection also retains its 2.0.32 baseline label
in `opencodeapi/check.py`: pinned 2.1.0 and latest probes are compared against
that contract to detect changes in consumed fields. Synthetic 2.0.32 fixtures
and version-selection tests retain coverage of the earlier supported release.

Contract fixes require offline replay; adapter changes also need live lifecycle
coverage in an environment that supports the harness. Record failures and skipped
checks explicitly. Live checks consume quota; never treat environment failures
as proof of compatibility. Keep authentication material and raw user configuration
out of committed evidence.
