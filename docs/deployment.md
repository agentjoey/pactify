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

One-time setup (maintainer):
1. Vercel → Add New Project → import `agentjoey/pactify`.
   - **Root Directory: `site/`** · Framework preset: Astro · production branch `main`.
   - Settings → General → 确认 **"Include source files outside of the Root Directory"** 为开启
     （默认开启；构建依赖 `../docs/*` 与 `../install.sh`，关掉会直接构建失败）。
2. Build gating: `site/vercel.json`'s `ignoreCommand` (committed, NOT a dashboard toggle —
   a dashboard-only "Ignored Build Step" was tried first and silently never took effect: every
   push, including pure-Go PRs touching zero docs/site files, kept triggering a full site
   rebuild for months, confirmed via the Vercel API deployment history and build logs showing
   no ignore-step evaluation before "Cloning..."). The committed command:
   ```bash
   git diff --quiet "${VERCEL_GIT_PREVIOUS_SHA:-HEAD^}" HEAD -- . ../docs/specs/pact-protocol.md ../docs/agent-onboarding.md ../install.sh
   ```
   (exit 0 = skip build; builds only when site/, the rendered docs, or install.sh change.)
   `VERCEL_GIT_PREVIOUS_SHA` — the last successfully deployed commit, exposed only when an
   `ignoreCommand` is set — diffs the FULL span since the last real site deploy, not just the
   pushed HEAD's immediate parent; a multi-commit push where an earlier (non-HEAD) commit
   touches a trigger path is still caught (`HEAD^` alone would miss it). Falls back to `HEAD^`
   only on the very first deployment, when there is no previous SHA yet.
3. Project → Settings → Domains → add `pactify.dev` (+ `www.pactify.dev` redirect).
   Add the records Vercel shows at the registrar (apex A / CNAME for www — use the exact
   values from the domain panel).
4. Post-deploy verification:
   ```bash
   curl -fsSL https://pactify.dev/install.sh | head -2     # shebang + comment
   curl -s https://pactify.dev/protocol | grep -c "Pact Protocol v1"
   ```
   Then the canonical install one-liner (`curl -fsSL https://pactify.dev/install.sh | sh`)
   replaces the raw.githubusercontent URL in README/plugin hook (separate follow-up PR).

Constraint: the site is fully static — no Vercel-exclusive features — so it can move to
any static host (Phase 6 China GTM) by re-pointing DNS.
