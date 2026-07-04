# Pactify — Deployment

> Last updated: 2026-07-05 | Status: Live (staging + production)

## Environments, Branches & CI/CD

Pactify's hosted stack is **one Vercel project (web) + two Fly apps (relay), split
into staging and production by git branch**. The web is git-connected (auto-deploy
on push); the relay is deployed with `flyctl` (currently manual — see automation
note below).

### Topology

| Env | Web domain | Web build (branch) | Relay (Fly app) | Relay URL |
|-----|-----------|--------------------|-----------------|-----------|
| **staging** | `orx.agentjoey.ai` | `staging` branch (preview) | `pactify-relay-staging` | `pactify-relay-staging.fly.dev` |
| **production** | `orx.pactify.dev` | `production` branch (target=production) | `pactify-relay` | `pactify-relay.fly.dev` |
| _preview_ | `*.vercel.app` | `main` + PRs | — (uses env fallback) | — |

- Vercel project `pactify` (`prj_nWlPZNeZMZNPk5og8mRJhVLvta7J`, team `agentjoey's projects`). Build config in `web/vercel.json` (`npm ci` + `pnpm install` cloud, then `tsc -b && vite build`).
- **Relay selection is by HOSTNAME at runtime** (`web/src/lib/source.ts` `RELAY_BY_HOST`), not the build-time `VITE_PACTIFY_RELAY_URL`. Vercel bakes a single env var per project, so staging and prod web builds would otherwise both point at one relay; the hostname map (`orx.agentjoey.ai→staging`, `orx.pactify.dev→prod`) makes **one bundle serve both domains correctly**. `VITE_PACTIFY_RELAY_URL` remains the fallback for localhost / previews / the serve-embedded build. When adding a new hosted domain, add it to `RELAY_BY_HOST`.
- Both relays are **single-instance** (`--ha=false`, `min_machines_running=1`): the relay holds in-process state (PairStore, rate limiters, live sockets) that a load balancer would split. Multi-instance needs a Redis adapter (backlog). Prod relay is **shared with linx** — deploy only additive/compatible changes; see `docs/linx-coordination.md`.

### Branch flow (the agreement)

```
feature branch ──PR──▶ main   (CI gate must be green; main = preview only)
                         │  fast-forward
                         ▼
                       staging  ──▶ auto-deploys staging web (orx.agentjoey.ai)
                         │  verify on staging (web + relay)
                         ▼
                       production ──▶ auto-deploys prod web (orx.pactify.dev)
```

- `main`, `staging`, `production` are **kept as fast-forwards of each other** (no divergent commits on staging/production — they only ever advance to a tested `main` commit). Promote with `git push origin main:staging` then, after verifying, `git push origin main:production`.
- The **relay** is deployed separately per env (it's not on Vercel):
  ```bash
  cd cloud
  flyctl deploy -c fly.staging.toml --ha=false   # staging relay
  flyctl deploy -c fly.toml         --ha=false   # production relay (shared w/ linx — additive only)
  ```
  Deploy the relay when `cloud/relay` or `cloud/wire` changed. Post-deploy verify: `curl -s <relay>/socket.io/?EIO=4&transport=polling` returns a `0{...}` handshake; `flyctl logs` shows `relay listening` + `No pending migrations`.
- **serve (the machine daemon)** is a separate binary users install; it connects to whichever relay its `--relay-url` names. The launchd-run serve on the dev box points at staging. Note: a rebuilt Go binary must be **ad-hoc codesigned** (`codesign --force -s - <bin>`) or launchd kills it (`OS_REASON_CODESIGNING`).

### CI gate (`.github/workflows/ci.yml`)

Runs on every PR + push to `main`. A PR must be green before it reaches `main` (and
therefore before it can be promoted to staging/production). The gate covers:
Go (`mod tidy -diff`, `vet`, `test`, `build`), **cloud workspace** (`wire` + `relay`
type-check & tests), web (`vitest` + `tsc`), `bats` protocol e2e, Playwright web e2e,
and the embedded-`dist`-in-sync check. Releases (tags) are handled by `release.yml`.

**CI/CD automation TODO** (backlog): (1) a `deploy-relay.yml` GitHub Action that runs
`flyctl deploy` for the matching relay on push to `staging`/`production` when
`cloud/**` changed (needs `FLY_API_TOKEN` secret) — removes the manual relay step;
(2) a promotion action (or documented `gh` one-liner) to fast-forward `staging`←`main`
and `production`←`staging` behind a manual approval; (3) a post-deploy smoke check
(relay handshake + web bundle relay-URL assertion) as a required status.



## 安装（规划）

```bash
# Go（单静态二进制）
brew install pactify    # 或
go install github.com/agentjoey/pactify@latest

# Node
npm install -g pactify
```

## 在项目中使用

```bash
cd your-repo
pactify init          # 生成 .pact/ 骨架
# 编辑 .pact/PROJECT.md 填写章程
# 用 git 管理 .pact/
git add .pact/ && git commit -m "chore: init pactify"
```

## 开发环境（本 repo）

CLI 语言待 Sprint 001 T1 决策后补充。

## pactify.dev (Astro site on Vercel)

The site lives in its **own repo** `agentjoey/pactify-website` (Vercel project `pactify-website`,
Root Directory = repo root, framework Astro, production branch `main`). It was split out of this
monorepo's `site/` on 2026-07-03 so the product repo and the website deploy independently — a
push here no longer triggers a pactify.dev rebuild (that was the whole point of the old
`site/vercel.json` `ignoreCommand`, now retired along with `site/`).

The three canonical files the site renders — `install.sh`, `docs/specs/pact-protocol.md`,
`docs/agent-onboarding.md` — are the source of truth **here**; the website repo vendors copies
into its `vendor/` via `scripts/sync-from-pactify.mjs`. When you change any of them, refresh the
website's vendor and commit it there:

```bash
# in a pactify-website checkout
node scripts/sync-from-pactify.mjs                 # from a local ../pactify checkout
node scripts/sync-from-pactify.mjs --ref v1.0.0    # or pin a released pactify ref (GitHub)
git add vendor && git commit -m "chore: sync vendored docs from pactify@<ref>"
```

Post-deploy verification:
```bash
curl -fsSL https://pactify.dev/install.sh | head -2     # shebang + comment
curl -s https://pactify.dev/protocol | grep -c "Pact Protocol v1"
```

Constraint: the site is fully static — no Vercel-exclusive features — so it can move to
any static host (Phase 6 China GTM) by re-pointing DNS.
