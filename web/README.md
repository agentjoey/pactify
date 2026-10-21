# Pactify web

Shared frontend for `pactify serve` and the read-only `pactify sub panel`, using React, TypeScript and Vite.

In `web/`, run: `npm install`; `npm run dev` (the API proxy defaults to `127.0.0.1:7777` and can be overridden with `PACTIFY_SERVE_URL`).
Verify: `npx tsc --noEmit && npx vitest run`; after building both outputs, run `npx playwright test --retries 0`.
Build: `npm run build && npm run build:panel`, producing `internal/serve/dist` and `internal/subpanel/dist` respectively; `npm run e2e` builds both in order and runs the browser tests.
