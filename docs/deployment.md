# Pactify — Deployment

> Last updated: 2026-10-18 | Status: 本地安装与开发；官网独立维护

## 安装

```bash
# v0.13.0 发布前：从 main 安装（路由器只在 main 上）
go install github.com/agentjoey/pactify/cmd/pactify@main

# 发布后：macOS / Linux 安装发布版
curl -fsSL https://pactify.dev/install.sh | sh
```

当前发布版为 v0.12.0，早于路由器，没有 `pactify sub`。安装脚本和 `go install …@latest` 目前都会装到它，所以 v0.13.0 发布前请用 `@main` 或从本 repo 构建。
治理模式已退役（AJ-744）；历史实现见 tag `v0.12.0`。

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
浏览器写请求受 CSRF Origin guard 保护；反向代理域名通过
`PACTIFY_ALLOWED_ORIGINS` 配置。GET 不要求额外 header。保持 loopback 监听，
公网入口使用 Cloudflare Tunnel + Access 认证；见 [jobs-view.md](jobs-view.md)。
launchd plist 只传 `serve --addr 127.0.0.1:17082`；旧 `--seat`、`--project` 和 relay
兼容参数已删除。serve 不发现治理项目，也不运行定时编排。

## pactify.dev (Astro site on Vercel)

The site lives in its **own repo** `agentjoey/pactify-website` (Vercel project `pactify-website`,
Root Directory = repo root, framework Astro, production branch `main`). It was split out of this
monorepo's `site/` on 2026-07-03 so the product repo and the website deploy independently — a
push here no longer triggers a pactify.dev rebuild (that was the whole point of the old
`site/vercel.json` `ignoreCommand`, now retired along with `site/`).

The website is maintained separately. `install.sh` remains the release installer
source here; governance protocol and onboarding documents moved to `docs/archive/`
in AJ-744 and no longer describe the current product. Website changes and vendor
refreshes are made in the website repository.
