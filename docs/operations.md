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
pactify serve --addr 127.0.0.1:7777
```

The launchd plist should pass only `serve --addr 127.0.0.1:7777`.
`--seat`, `--project`, `--relay-url`, `--relay-token` and `--remote-control` were
removed in AJ-744 and now fail as unknown flags. SIGINT shuts the server down.

Serve exposes `/api/sub` and static resources, including job event SSE. Read
locally with `curl http://127.0.0.1:7777/api/sub/jobs`. Every request validates
Host: a loopback bind accepts `127.0.0.1`, `localhost` or `[::1]` only with the
listening port. Browser writes require an exact `http` origin on that port;
requests without Origin remain allowed. Configure `PACTIFY_ALLOWED_ORIGINS`
(comma-separated full origins) for a reverse proxy that performs authentication.
Its hosts are accepted with or without their explicit configured ports. Host
and Origin checks do not authenticate users.

Non-loopback addresses (including `0.0.0.0`, `::` and an omitted host) require
`PACTIFY_SERVE_TOKEN` with at least 32 characters. Host must then match the bound
IP/hostname and port or an allowed proxy host. If a token is set on any address,
every `/api/` request, including reads and SSE, needs `Authorization: Bearer
<token>`; static assets stay public behind the Host check. The dashboard UI does
not send this token: use a reverse proxy that authenticates users and injects
the header. Dashboard workers receive neither the token nor
`PACTIFY_ALLOWED_ORIGINS`.

Responses include `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`
and `Content-Security-Policy: frame-ancestors 'none'`. The server allows 10 seconds
for request headers and 60 seconds for idle connections, without an overall SSE
write deadline. Deployment details are in [jobs-view.md](jobs-view.md).

## Release and launchd

The published release is v0.13.0, the first router release. Release artifacts
use GoReleaser and the `v*` tag workflow in `.github/workflows/release.yml`.
Release only after the Owner decides to do so.
See [deployment.md](deployment.md) and [CHANGELOG.md](CHANGELOG.md).

A locally built macOS binary needs signing before launchd runs it:

```bash
go build -o pactify ./cmd/pactify
codesign --force -s - ./pactify
```

Without that signing step, launchd can terminate it with `OS_REASON_CODESIGNING`.
The release installer signs its downloaded binary; that does not cover local
builds. Governance mode and the old orchestrator are available in tag `v0.12.0`.
