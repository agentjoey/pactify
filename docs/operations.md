# Operations

## Diagnostics

```sh
pactify sub status --all
pactify sub logs <job-id> --tail 20
pactify sub result <job-id>
pactify sub doctor <harness>
```

Doctor checks harness compatibility and runs real probes that may consume model
quota. See [doctor](sub-doctor.md) and [upstream compatibility](harness-updates.md).

## Local server

Run `pactify serve` (default `127.0.0.1:7777`), or specify `--addr`. SIGINT shuts
it down. Read jobs with `curl http://127.0.0.1:7777/api/sub/jobs`.

Every request, including static assets and SSE, validates Host. Loopback binds
accept `127.0.0.1`, `localhost` and `[::1]` with the listening port. Browser writes
require an exact `http` loopback Origin on that port or an entry in
`PACTIFY_ALLOWED_ORIGINS`. CLI requests without Origin remain allowed.

`PACTIFY_ALLOWED_ORIGINS` is a comma-separated list of full origins for use
behind an authenticating reverse proxy. Their hosts are accepted with
or without explicitly configured ports. Host and Origin checks do not authenticate users.

Non-loopback binds, including `0.0.0.0`, `::` and an omitted host, require
`PACTIFY_SERVE_TOKEN` of at least 32 characters. Host must match the bound
IP/hostname and port or an allowed proxy host. Setting a token on any bind makes
all `/api/` requests, including reads and SSE, require `Authorization: Bearer
<token>`. Static resources remain public behind Host validation. The dashboard
requires an authenticating proxy that injects this header; it does not send the
token itself. Workers inherit neither the token nor allowed origins.

Responses include `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`
and `Content-Security-Policy: frame-ancestors 'none'`. Header timeout is 10
seconds and idle timeout is 60 seconds; SSE has no overall write deadline.
See [Jobs dashboard](jobs-view.md) for deployment details.

## macOS background services

Locally built binaries installed as macOS background services need ad-hoc signing:
`codesign --force -s - ./pactify`. This applies to local service installations,
not ordinary foreground use. Release installation and local builds are separate.

See [installation](deployment.md) and [CHANGELOG](CHANGELOG.md).
