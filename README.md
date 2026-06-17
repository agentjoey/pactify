# Pactify

**A portable, vendor-neutral protocol for multiple AI agents to collaborate in one git repo.**

Any agent that can read files and run git — Claude, opencode, Gemini, Cursor, … — can play a role
(orchestrator, worker, reviewer) and coordinate through plain files in `.pact/`. No SDK, no server,
no external dependency. The repo is the single source of truth.

> Status: **early.** Protocol **v1 is frozen** (see [`docs/specs/pact-protocol.md`](docs/specs/pact-protocol.md)).
> A bash reference implementation and the `pactify` Go CLI both drive the protocol today.
> Site: [pactify.dev](https://pactify.dev) · Part of the **Pact-Base** open core (see [ROADMAP](docs/ROADMAP.md)).

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
- **Architecture:** [`docs/architecture.md`](docs/architecture.md) · **Roadmap:** [`docs/ROADMAP.md`](docs/ROADMAP.md)
- **Contributing & git workflow:** [`CONTRIBUTING.md`](CONTRIBUTING.md)

## License

[MIT](LICENSE) © 2026 Joey (agentjoey).
