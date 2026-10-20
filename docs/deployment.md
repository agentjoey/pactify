# Pactify — Deployment

> Last updated: 2026-10-20 | Status: 本地安装与开发；官网独立维护

## 安装

```bash
# macOS / Linux 安装发布版
curl -fsSL https://raw.githubusercontent.com/agentjoey/pactify/main/install.sh | sh

# 或用 Go 安装
go install github.com/agentjoey/pactify/cmd/pactify@latest
```

当前发布版为 v0.13.0，首个带 `pactify sub` 的版本；v0.12.0 及更早的版本没有路由器。
治理模式已退役（AJ-744）；历史实现见 tag `v0.12.0`。

## 开发环境（本 repo）

Go 版本以 `go.mod` 为准；前端使用 Node.js 与 npm。

```bash
go build -o pactify ./cmd/pactify
PACTIFY_HOME="$(mktemp -d)" go test ./...
cd web && npm ci
npm run dev
```

Vite proxy 连接 `PACTIFY_SERVE_URL`（默认 `http://127.0.0.1:7777`）指向的本地 `pactify serve`，同时将转发的 Host 和已有 Origin 改为目标地址，支持开发时写入。
本地重建的二进制交给 launchd 前需 `codesign --force -s - ./pactify`；详见 [operations.md](operations.md)。

## 本地 serve

```bash
pactify serve --addr 127.0.0.1:7777
```

serve 只提供 `/api/sub`（Jobs、Dashboard、Settings、Live，含 job events SSE）和嵌入静态资源。
所有请求（含静态资源和 SSE）均校验 Host：loopback 监听仅接受 `127.0.0.1`、`localhost`、`[::1]` 加监听端口。
浏览器写请求的 Origin 必须精确匹配该端口的 `http` loopback origin，或列入 `PACTIFY_ALLOWED_ORIGINS`；无 Origin 的 CLI / 脚本请求继续允许。
`PACTIFY_ALLOWED_ORIGINS` 是逗号分隔的完整 origin 列表，用于执行用户认证的反向代理；也允许这些 origin 的 Host，带或不带其显式配置的端口。Host / Origin 校验不代替用户认证。

非 loopback 监听（含 `0.0.0.0`、`::` 和省略主机）必须设置至少 32 字符的 `PACTIFY_SERVE_TOKEN`，Host 仅接受绑定 IP / 主机名加端口或上述代理 Host。
任何监听地址只要设置该 token，所有 `/api/` 请求（含 GET 和 SSE）均须携带 `Authorization: Bearer <token>`；静态资源仍公开，但受 Host 校验。
Dashboard UI 自身不发送此 token；token 模式用于认证后注入该 header 的反向代理。详见 [jobs-view.md](jobs-view.md)。
launchd plist 只传 `serve --addr 127.0.0.1:7777`；旧 `--seat`、`--project` 和 relay
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
