# Pactify — Operations

> Last updated: **2026-10-18** | Status: local harness router; governance mode retired (AJ-744).

## Development and diagnostics

```bash
git pull
cat .agent/CURRENT.md          # local, ignored handoff notes when available
pactify sub status --all
pactify sub logs <job-id> --tail 20
pactify sub result <job-id>
pactify sub doctor <harness>
```

`sub doctor` checks harness compatibility; some probes execute real harness
requests. See [sub-doctor.md](sub-doctor.md) before running probes.

## Local server

```bash
pactify serve --addr 127.0.0.1:17082
```

The launchd plist should pass only `serve --addr 127.0.0.1:17082`.
`--seat`, `--project`, `--relay-url`, `--relay-token` and `--remote-control` were
removed in AJ-744 and now fail as unknown flags. SIGINT shuts the server down.

Serve exposes `/api/sub` and static resources, including job event SSE. Read
locally with `curl http://127.0.0.1:17082/api/sub/jobs`. Browser writes check
Origin; configure `PACTIFY_ALLOWED_ORIGINS` for the reverse proxy domain.
Keep serve on loopback and use Cloudflare Tunnel + Access for authenticated
external access. Origin checks are not login authentication. Deployment details
are in [jobs-view.md](jobs-view.md).

## Release and launchd

The published release is v0.12.0; router M1 is merged but unreleased and v0.13.0
is on hold. Release artifacts use GoReleaser and the `v*` tag workflow in
`.github/workflows/release.yml`. Release only after the Owner decides to do so.
See [deployment.md](deployment.md) and [CHANGELOG.md](CHANGELOG.md).

A locally built macOS binary needs signing before launchd runs it:

```bash
go build -o pactify ./cmd/pactify
codesign --force -s - ./pactify
```

Without that signing step, launchd can terminate it with `OS_REASON_CODESIGNING`.
The release installer signs its downloaded binary; that does not cover local
builds. Governance mode and the old orchestrator are available in tag `v0.12.0`.
