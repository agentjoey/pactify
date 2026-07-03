# @pactify-apps/web — Pactify Mission Control (U2)

A read-only cross-machine board for pact projects. SvelteKit, deployed on Vercel.

The browser holds the account master secret (never sent), derives the account key
to log into the relay, lists projects, folds each project's **cleartext** pact-event
headers into a board, and subscribes to live `pact-event` broadcasts. The event
body (spec/evidence) is E2E-encrypted; the relay is a blind courier. Drill-down
detail decrypts locally via `deriveProjectKey` (byte-identical to the Go uploader).

## Develop

```bash
pnpm install
PUBLIC_PACTIFY_RELAY_URL=https://pactify-relay-staging.fly.dev pnpm dev
pnpm test    # board projection unit tests
pnpm check   # svelte-check
```

## Deploy (Vercel project `pactify`)

- Root Directory: `cloud/web`, framework SvelteKit (adapter-vercel).
- Env: `PUBLIC_PACTIFY_RELAY_URL` = the relay base URL (e.g. the prod relay).

Requires the relay to have the U2 endpoints deployed (`/v1/pact/*`) — see the
coordinated relay rollout.
