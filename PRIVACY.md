# Privacy policy

Effective date: 2026-10-21

Pactify is a local harness router maintained by the pactify project. It runs on your machine and has no telemetry and no pactify-operated remote server. The project does not collect, receive or store your data.

## What data is involved

When you delegate a task, pactify handles the brief you write and the repository content the task needs. It stores job metadata, briefs, logs, diffs, results, private harness homes and caches locally under `PACTIFY_HOME` (default `~/.pactify`).

## Where data goes

Briefs and repository content are sent only by the harness CLI you installed and logged in yourself. That CLI sends data to its provider: codex to OpenAI, kimi to Moonshot AI, agy to Google, and opencode to whichever provider you configured in opencode. Those providers' own privacy policies apply to the data their CLIs send.

## Local storage, retention and deletion

All pactify data stays in `PACTIFY_HOME`. Retention follows the [storage lifecycle](docs/operations.md#storage-lifecycle). The dashboard defaults to a loopback address, and the read-only panel listens only on loopback. To delete data, run `pactify sub prune` (preview, then `--yes`; add `--include-worktrees` for pending worktrees), or remove `PACTIFY_HOME`.

## Sharing and sale

Pactify collects nothing, so it has nothing to sell or share. It does not include analytics, advertising or tracking.

## Contact

Questions or requests: [GitHub issues](https://github.com/agentjoey/pactify/issues).
