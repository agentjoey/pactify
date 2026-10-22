# Plugin source and generator

Edit this directory, then run from the repository root:

```sh
go run ./internal/plugingen/cmd/plugingen
go run ./internal/plugingen/cmd/plugingen -check
```

Never hand-edit generated skills. Commit the source and generated outputs together.
`-check` reads without writing, lists sorted missing, changed or obsolete output
paths (including permission changes), and exits nonzero if regeneration is needed.
Write mode removes obsolete files only within the generated skill/distribution trees.
Run the generator from the repository root. It uses only Go's standard library;
it is a separate development command, outside the shipped pactify binary.

- `config.json` holds the single plugin version, minimum CLI version, license and
  manifest metadata. Bump its version when changing the plugin, then regenerate.
- `skills/<name>.md.tmpl` holds each skill once. Names match output folders.
- `shared-delegation.md.tmpl` holds the complete shared brief and review rules once.
- `fragments/` holds host-specific waiting and forwarding instructions, prerequisites
  and portable installation documentation.
- `pactify-check.sh.tmpl` generates the same POSIX shell checker for every distribution.
  It never uses the network or runs installers. Older CLI versions fail with an upgrade
  hint; unknown versions (including dev) warn without failing. Valid compatible releases
  are silent in quiet mode. Version-command and missing-sub failures still fail.
- `install-commands.txt` supplies the install commands shown to the user by skills
  and checkers. These commands must never be run by the agent.
- `portable-README.md.tmpl` documents the generated marketplace repository root.

Templates use Go `text/template`: `{{include "relative/file.md.tmpl"}}` expands
another source file; `{{if eq .Host "claude"}}...{{else}}...{{end}}` selects a
host fragment. `.Host` is `claude` or `portable`. `{{command "delegate"}}` renders
Claude's short and namespaced forms, or the portable short invocation. `.Version`,
`.MinCLI`, `.License` and other configuration fields are available to templates.
Includes cannot escape this directory or recurse. Keep common rules outside host
branches so the two distributions preserve the same review meaning.

Generated outputs:

- `plugins/pactify/skills/*/SKILL.md`, `.claude-plugin/plugin.json` and `bin/pactify-check`.
  Claude skill bytes are preserved without a generated marker. Hooks and forwarding
  agents remain hand-maintained, as does the existing Claude marketplace.
- `plugins/agent-plugin/`: a marketplace repository root with Codex, Copilot and Cursor
  marketplace files, README and the `pactify/` Agent Plugins 1.0 plugin. Its plugin
  has no hooks, forwarding agents, bin or Claude manifest. JSON uses only allowed
  fields; generated markers appear in portable Markdown and shell scripts.
- `plugins/agents-skills/<name>/`: identical portable skills and scripts ready to copy
  into `.agents/skills/`. Helpers are invoked with `sh scripts/pactify-check`
  relative to the skill directory, without relying on a plugin-specific PATH.

Build an Agensi archive without rewriting other outputs:

```sh
go run ./internal/plugingen/cmd/plugingen -agensi /tmp/pactify-delegate.zip
```

It merges the portable delegate guide into one `pactify-delegate/` skill folder,
with a matching frontmatter name and a text-only checker. Entries are sorted,
UTF-8 text, mode 0644, timestamp 1980-01-01 UTC. The archive is reproducible.
Release CI builds it after tests in a temporary directory, then attaches it to
the release. GoReleaser uses its default `dist/` scratch directory; committed
plugin distributions live under `plugins/`. No repository publishing is automated.

Validation: `go test ./internal/plugingen/...`, `bats tests/`, generator `-check`,
`claude plugin validate plugins/pactify`, and `claude plugin validate .`. See the entry guides for full Go checks.
Portable format references: [Agent Plugins 1.0](https://agent-plugins.org/spec/1.0.0.md)
and [Agent Skills](https://agentskills.io/specification).
