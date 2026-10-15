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
harness is omitted; `/codex`, `/kimi`, `/agy` and `/opencode` select one explicitly. **agy and opencode are experimental (evaluation pending)**. **kimi is experimental**:
Phase 0 large tasks completed in one attempt 6/10, and read-only work did not converge.
kimi results include an informational warning; `kimi --read-only` still exits 2.
See the [harness plugin README](plugins/pactify/README.md) for usage and review rules.

Use `/jobs` (or `/pactify:jobs`) in Claude Code to see the latest 10 delegation jobs,
running jobs first, with activity, tokens and next-step suggestions. `/jobs all` shows a
bounded overview of all jobs; `/jobs <id>` shows result excerpts, a log tail, diff statistics
and resume links. This skill is read-only and never applies, discards, cancels or resumes
jobs. Suggestions require reviewing changes and personally rerunning acceptance, including
the full test suite, before apply. See the [jobs skill](plugins/pactify/skills/jobs/SKILL.md)
and [status JSON contract](docs/sub-status.md).

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

agy uses the CLI stream-json backend (Antigravity CLI 1.3.5). Prompts go through stdin;
`agy models` supplies the allowed model ids and `init.model` verifies the actual model.
Write access uses `--dangerously-skip-permissions`; read-only uses `--mode=plan` without skip.
agy read-only reviews must use built-in file tools only: any shell command or write
is auto-denied and ends the turn without a reply. Use codex `--read-only` for commands;
plan mode alone does not block writes when skip-permissions is enabled.
Headless permission auto-denial fails the job even if agy reports SUCCESS.
The default `HOME=$PACTIFY_HOME/harness-homes/agy` (0700) mirrors top-level user HOME
entries with symlinks, excluding `.gemini`. Its private `.gemini/antigravity-cli`
links only `antigravity-oauth-token` to the original; token contents are never read
or copied by pactify. User sessions and integrations stay separate. Links refresh
under a lock before launch; replaced copies are removed with a warning after exit.
Resume keeps the recorded home. `--with-user-config` restores real HOME and warns
that agy sessions are visible in the user's list (no archive API).
Run `pactify sub doctor agy` for compatibility checks.

### Local job panel (read-only)

```sh
pactify sub panel                       # random loopback port; opens the browser
pactify sub panel --port 17100 --no-open # print the URL without opening it
```

Open the printed `http://127.0.0.1:<port>/?token=…` URL. Each launch generates a
new random token; the first browser visit exchanges it for an HttpOnly,
SameSite=Strict session cookie and redirects to a URL without the token. All
APIs require that cookie or `Authorization: Bearer <token>`. The server only
listens on `127.0.0.1`; Ctrl-C stops it. Job data comes from `$PACTIFY_HOME/jobs`
(default `~/.pactify/jobs`).

Filter jobs by status, harness or repository; running jobs appear first and
attention markers highlight pending worktrees, failures and warnings. Open a job
for its brief, readable result, live logs, file-folded diff and resume links.
The panel supports light/dark appearance and phone widths. It is read-only:
run `pactify sub apply <job>`, `pactify sub discard <job>` or
`pactify sub cancel <job>` in the CLI or Claude Code; the detail page provides
copyable commands. Panel reads never recover orphan workers or change job state.
Logs start with the last 100 readable lines, follow the job, then close when its
recorded state becomes terminal. The page retains at most 1,000 log lines.

Worktree diffs compare the current files (including untracked files) against the
recorded base commit, without staging or modifying the repository. Patches are
limited to 512 KiB with an explicit truncation notice. Non-worktree jobs retain
only their recorded file change list; the current result format does not save a
full patch. Missing/discarded worktrees fall back to the recorded list.

The panel is separate from the frozen `pactify serve` dashboard. Its assets are
checked into `internal/subpanel/dist`, so `go build ./...` needs no Node runtime.
To rebuild and capture the final panel build:

```sh
cd web
npm ci
npm run build:panel
node scripts/shots-panel.mjs # prints temporary paths: list/detail × light/dark × desktop/mobile
```

opencode 1.19.0 uses ACP with dynamic provider/model ids. `/opencode` (or
`/pactify:opencode`) selects this experimental harness; evaluation is pending.
An omitted model keeps opencode's default; `opencode export <sessionId>` independently
verifies the actual model in the same isolated environment. Input tokens include
cached reads. Cost uses only this job's new assistant messages, including on resume.
Write access sets `build` mode and process-local edit/bash/webfetch allow permissions.
Read-only access sets edit/webfetch deny and bash ask, then rejects every permission
request. It supports read tools only; shell commands cannot run, and a denial may
leave an empty final reply. Plan mode alone does not enforce read-only access.

Minimal configuration mirrors the user's XDG data and config directories under
`$PACTIFY_HOME/harness-homes/opencode/{share,config}`, excluding `opencode` in both.
Only `share/opencode/auth.json` links to the original login file; its contents are
never read or copied. Other XDG entries remain available through symlinks.
`OPENCODE_DISABLE_CLAUDE_CODE=1`, `OPENCODE_DISABLE_EXTERNAL_SKILLS=1` and
`OPENCODE_PURE=1` reduce optional integrations. Known sibling vendor API keys are
cleared for sub's opencode processes; login comes from the linked auth file.
`--with-user-config` restores both original XDG directories and optional integrations,
with a warning that sessions remain in the user's list. Resume requires the same
config mode and retains both recorded directories. Run `pactify sub doctor opencode`
for model, permission, session hiding and managed directory checks.


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
