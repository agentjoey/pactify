# Pactify — Deployment

> Last updated: 2026-10-18 | Status: 本地安装与开发；官网独立维护

## 安装

```bash
# macOS / Linux
curl -fsSL https://pactify.dev/install.sh | sh

# 或用 Go 安装
go install github.com/agentjoey/pactify/cmd/pactify@latest
```

当前发布版为 v0.12.0。harness 路由器功能已合入 main、尚未发布；内部自用请从本 repo 构建。
可选治理模式的项目接入见 [onboarding.md](onboarding.md)。

## 开发环境（本 repo）

Go 版本以 `go.mod` 为准；前端使用 Node.js 与 npm。

```bash
go build -o pactify ./cmd/pactify
PACTIFY_HOME="$(mktemp -d)" go test ./...
cd web && npm ci
npm run dev
```

Vite proxy 连接 `PACTIFY_SERVE_URL`（默认 `http://127.0.0.1:17082`）指向的本地 `pactify serve`。
本地重建的二进制交给 launchd 前需 `codesign --force -s - ./pactify`；详见 [operations.md](operations.md)。

## 本地 serve

```bash
pactify serve --addr 127.0.0.1:17082
```

serve 只提供 `/api/sub`（Jobs、Dashboard、Settings、Live，含 job events SSE）和嵌入静态资源。
AJ-723 已删除旧治理 API；治理命令继续使用 CLI/MCP。CSRF Origin guard 保留，
反向代理域名通过 `PACTIFY_ALLOWED_ORIGINS` 配置；GET 不要求额外 header。
`--seat`、可重复的 `--project` 与 `--relay-url`、`--relay-token`、`--remote-control`
均为隐藏的空操作兼容参数。旧 launchd plist 可继续启动 serve。

AJ-735 已删除 scheduler 和 orchestrator 及其 CLI 命令；回滚锚点为 tag `v0.12.0`。
serve 不再读取定时任务或发现治理项目。

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
