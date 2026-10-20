# Installation and deployment

Install the published binary on macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/agentjoey/pactify/main/install.sh | sh
# Or install with Go:
go install github.com/agentjoey/pactify/cmd/pactify@latest
```

The current release is v0.13.0. See [CHANGELOG](CHANGELOG.md).
Install and authenticate a supported harness CLI, then run
`pactify sub doctor <harness>`; probes may consume model quota.

## Development

Use the Go version in `go.mod`, Node.js and npm:

```sh
go build -o pactify ./cmd/pactify
PACTIFY_HOME="$(mktemp -d)" go test ./...
cd web
npm ci
npm run dev
```

The Vite proxy connects to `PACTIFY_SERVE_URL`, default
`http://127.0.0.1:7777`, and rewrites Host and any Origin to the target.
Rebuild the embedded dashboard and panel with `npm run build` and
`npm run build:panel` before rebuilding the Go binary.

## Local server

```sh
pactify serve
```

The default address is `127.0.0.1:7777`; use `--addr` to override it.
The server provides Dashboard, Jobs and Settings, live job events and `/api/sub`.
Host validation applies to all requests. Browser writes require an exact allowed
Origin; CLI requests without Origin remain allowed. Non-loopback binds require
`PACTIFY_SERVE_TOKEN` with at least 32 characters. If set on any bind, all API
requests, including reads and SSE, require a bearer token. The dashboard does not
send this token itself; use an authenticating reverse proxy that injects it.
See [operations](operations.md) and [Jobs dashboard](jobs-view.md) for details.
