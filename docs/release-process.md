# Pactify Release Process

> Repo: `agentjoey/pactify` (private). Governs the product — the relay
> (`cloud/relay` → Fly) and Mission Control (`cloud/web` → Vercel `pactify`).
> The marketing site (`agentjoey/pactify-website`) has its own simple main→prod.

## Branch model

Three long-lived branches, promoted **fast-forward only** (never diverge):

| branch | role | deploys to |
|---|---|---|
| `main` | **trunk** — default branch, all dev/PRs land here, CI runs | nothing (integration only) |
| `staging` | **pre-release** — a snapshot of `main` you're validating | staging relay + Mission Control preview |
| `production` | **released** — validated + confirmed, the live line | prod relay + Mission Control prod |

Rule: **`main` → `staging` → (you confirm) → `production`.** Nothing reaches
production without first passing through staging and your sign-off.

## Environments

| | staging | production |
|---|---|---|
| relay (Fly) | `pactify-relay-staging` | `pactify-relay` |
| relay Neon branch | `staging` | `production` |
| Mission Control (Vercel) | preview (`pactify-git-staging-*`) | prod (`pactify` project) |
| `PUBLIC_PACTIFY_RELAY_URL` | staging relay URL | prod relay URL |

Both relays share ONE Neon project (`pactify-cloud`); staging/production are its
two branches. linx shares the same relays (coordinate deploys — see below).

## Promotion — step by step

### 1. Develop → `main`
Merge work to `main` (PR, CI green). `main` is never deployed directly.

### 2. Promote to staging + deploy
```bash
git push origin main:staging          # fast-forward staging to main
```
Then deploy the staging environment:
- **Relay:** `git checkout staging && cd cloud && fly deploy -c fly.staging.toml`
  (Claude runs this; secrets persist across deploys.)
- **Mission Control:** the push to `staging` auto-builds a Vercel preview.
- Additive Neon migrations apply on relay boot (`prisma migrate deploy`).

**Verify on staging** (you + linx): board/relay endpoints, linx functional +
no rejected traffic (`/metrics` `ingest_rejected_total`), pact pipeline E2E.

### 3. You confirm → promote to production + deploy
Only after staging is signed off:
```bash
git push origin staging:production     # fast-forward production to staging
```
Then deploy production:
- **Relay:** `git checkout production && cd cloud && fly deploy -c fly.toml`.
- **Mission Control:** the push to `production` auto-builds the Vercel prod deploy
  (requires the `pactify` project's Production Branch = `production` — one-time
  dashboard setting: Settings → Git → Production Branch).
- linx switches its prod relay URL after the prod relay is confirmed.

### 4. Rollback
`production` is fast-forward-only, so the previous prod commit is its parent:
```bash
git push -f origin <prev-prod-sha>:production   # then redeploy relay from production
```
Relay/Neon are unaffected by a Vercel rollback; a relay rollback is re-deploying
the prior image (`fly releases` / `fly deploy` an earlier commit).

## Coordination with linx (shared relay)

The relay is shared with linx. Before promoting a relay change that alters
accepted traffic (e.g. the #7 wire validation), verify linx's staging shows no
rejected traffic first, then promote to production. linx must push its own repo
(`agentjoey/pactify-linx`) up to date before a prod relay cutover — its GitHub
origin can lag its local work.

## One-time setup checklist

- [x] `main` / `staging` / `production` branches exist; stale feature branches removed.
- [x] Vercel `pactify`: Root Directory `cloud/web`, framework SvelteKit,
      `PUBLIC_PACTIFY_RELAY_URL` env, `vercel.json` build/ignore commands.
- [ ] **Vercel `pactify` Production Branch → `production`** (dashboard; API-gated).
- [ ] Prod relay `pactify-relay` created + secrets + first deploy from `production`.
- [ ] `PUBLIC_PACTIFY_RELAY_URL` production target → prod relay URL (currently staging).
