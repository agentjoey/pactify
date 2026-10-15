# Pactify

**A portable, vendor-neutral protocol for multiple AI agents to collaborate in one git repo.**

Any agent that can read files and run git — Claude, opencode, Gemini, Cursor, … — can play a role
(orchestrator, worker, reviewer) and coordinate through plain files in `.pact/`. No SDK, no server,
no external dependency. The repo is the single source of truth.

> Status: **early**, actively developed. Latest release **v0.11.0** (2026-08-24) — see
> [`docs/CHANGELOG.md`](docs/CHANGELOG.md). Protocol **v1 is frozen**
> (see [`docs/specs/pact-protocol.md`](docs/specs/pact-protocol.md)); a bash reference
> implementation and the `pactify` Go CLI both drive it today.
> Site: [pactify.dev](https://pactify.dev) · Part of the **Pact-Base** open core.
>
> The **hosted relay is not currently running** — `pactify serve` (local mode) is the
> supported path; the dashboard's hosted mode is offline.

## Install

**curl | sh** (macOS / Linux, amd64 / arm64):

```bash
curl -fsSL https://pactify.dev/install.sh | sh
```

**go install:**

```bash
go install github.com/agentjoey/pactify/cmd/pactify@latest
```

Then get started:

```bash
pactify setup    # guided setup in your repo
```

Verify your install anytime with `pactify doctor`.

## Quickstart

In your repo, run:

```bash
pactify setup
```

`setup` scaffolds `.pact/`, wires your agent (opencode / Claude / Gemini / Codex), and sets
your seat. Then launch your agent — it joins via the pact MCP tools. Run `pactify doctor`
anytime to verify install + wiring.

**New to Pactify?** [`docs/onboarding.md`](docs/onboarding.md) walks a fresh project from zero
to shipped in 5 minutes (register agents → setup → plan → run → ship), with seat definitions and
dashboard deployment. Running the dashboard? `pactify serve` needs `--seat <id>` for write
endpoints (assign / orchestrate / ship); without it they fail closed (read-only still works).

## Claude Code (one-click)

Inside Claude Code:

```
/plugin marketplace add agentjoey/pactify
/plugin install pact@pactify
```

This adds the `pact` skill and MCP server. If the `pactify` binary isn't installed yet,
the plugin reminds you on session start — run the curl|sh installer above, then `pactify setup`.

For harness delegation, install `/plugin install pactify@pactify` and use `/delegate`
(or `/pactify:delegate`). `pactify sub run [harness]` defaults to **codex** when the
harness is omitted; `/codex` and `/kimi` select one explicitly. **kimi is experimental**:
Phase 0 large tasks completed in one attempt 6/10, and read-only work did not converge.
kimi results include an informational warning; `kimi --read-only` still exits 2.
See the [harness plugin README](plugins/pactify/README.md) for usage and review rules.

Delegated codex sessions use a minimal configuration by default: user MCP servers,
plugins, skills and memories are disabled for that process. Authentication, model
provider, sandbox settings and global/repository `AGENTS.md` discovery are retained.
Use `pactify sub run --with-user-config` to load all user integrations, including
when resuming a codex session. Kimi delegations also default to minimal config
(`kimi (minimal config)`): `KIMI_CODE_HOME` points to the persistent shared
`$PACTIFY_HOME/harness-homes/kimi` directory (0700; `PACTIFY_HOME` defaults to
`~/.pactify`). Only `config.toml`, `credentials/` and `device_id` are symlinked
from the pactify process's `KIMI_CODE_HOME`, or `~/.kimi-code` when unset.
Pactify never reads or copies their contents; missing originals require `kimi login`.
The managed home has no `mcp.json`, and delegated sessions stay outside your own
session list without starting `kimi web`. Links are repaired before each launch;
if kimi replaces one with a file or directory, the job warns and deletes that
private copy afterward. Keep the shared directory for resume. Kimi resume retains
the recorded home and requires the original config mode; use `--with-user-config`
for user-config sessions, or start fresh to switch modes. User-config kimi jobs
use your home and the existing archive workflow. `sub doctor` verifies the default
managed-home path, session-index isolation and absence of MCP discovery events.
Job records and JSON expose `configMode` (`minimal` or
`user`); the result's Harness line shows the same mode. Existing jobs without
this field are treated as user config.

## The idea in 30 seconds

- **`.pact/log.jsonl`** is an append-only event log — the authoritative source of truth and the
  communication bus between agents.
- **`.pact/STATE.yml`** is a *regenerable projection* of the log (for reading; the log is authoritative).
- **Two rules (the pact):** a worker cannot self-accept its own work (only the assigned reviewer can,
  and only after a checkpoint); a feature cannot merge until all its tasks are accepted.
- **Seats:** each agent is a self-declared, stable *seat* (`orchestrator` / `worker` / `reviewer`),
  enforcing separation of duties by name.
- **Pull-based:** a worker boots from its entry file (`AGENTS.md` / `CLAUDE.md`), reads the log, and
  knows what to do — the human is a *start button*, not a context courier.

## Try the reference implementation

```bash
source .pact/bin/pact.sh
pact_help          # the verb reference + the two rules
```

The verbs (`pact_<verb>` in bash today, `pactify <verb>` in the Go CLI) are one contract:
`init · join · assign · checkpoint · accept · changes · merge · status · log · validate`.

## Docs

- **Protocol spec (v1):** [`docs/specs/pact-protocol.md`](docs/specs/pact-protocol.md) — independently implementable.
- **JSON Schemas:** [`schemas/`](schemas/) — `event` / `seat` / `task`.
- **Architecture:** [`docs/architecture.md`](docs/architecture.md) · **Changelog:** [`docs/CHANGELOG.md`](docs/CHANGELOG.md)
- **Onboarding:** [`docs/onboarding.md`](docs/onboarding.md) · **Operations:** [`docs/operations.md`](docs/operations.md) · **Deployment:** [`docs/deployment.md`](docs/deployment.md)
- **Contributing & git workflow:** [`CONTRIBUTING.md`](CONTRIBUTING.md)

## License

[MIT](LICENSE) © 2026 Joey (agentjoey).
