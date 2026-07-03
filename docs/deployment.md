# Pactify — Deployment

> Last updated: 2026-06-09 | Status: Placeholder（CLI 实现后补充）

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
