# SP3 onboarding 一键化 Implementation Plan

> **执行**：orchestrate（pact 任务图）。t-sp3-backend（opencode）→ t-sp3-frontend（kimi，deps backend）；t-sp3-docs（claude 直接做）。reviewer=claude。

**Goal:** 新项目从零到能跑降到 dashboard 一键 + 一页文档：setup apply（init + batch wire）+ Setup UI + onboarding.md。

**Architecture:** 新建 `POST /api/setup/apply`（init 新项目 + 逐 kind wire）；前端 Setup 加 Apply；文档新建 onboarding.md。针对全新项目（无 .pact），已有项目不 re-init。

**Tech Stack:** Go（internal/serve/setup.go、pact.Init、agent.WireAt）；React+vitest；Markdown。

**Spec:** `docs/superpowers/specs/2026-06-17-sp3-onboarding-design.md`

---

## Task t-sp3-backend：setup apply endpoint（opencode）

**Files:**
- Modify: `internal/serve/setup.go`（加路由 + `handleSetupApply`）
- Create: `internal/serve/setup_apply_test.go`
- 复用：`internal/pact`（Init/ParseSeat）、`internal/agent`（WireAt）、`author.go`（requireSeat）

**实现要点（TDD）：**
- [ ] `POST /api/setup/apply`，body `{path:string, project:string, seats:[{id,roles,entry,kind}]}`：
  - `s.requireSeat(w)`（machine-scoped，新项目无 roster）→ 422 if 无 seat。
  - 校验 `path` 存在且 **无 `.pact`** → 已存在返回 409。
  - 组 seat spec 串 `id:roles:entry:kind`（roles 逗号连接），`pact.At(path).As(<seat0 或 s.seat>).Init(project, specs)`。
  - 逐个带 kind 的 seat：`agent.WireAt(path, kind, id, roles, path)`，收集 `{kind,seat,wrote,path,docOnly,snippet}`。
  - 返回 `{inited:true, wired:[...], notes}`。Init 失败 → 错误不 wire；wire 失败 → per-kind 结果报告（best-effort）。
- [ ] verify 绿 → checkpoint。

**测试骨架（setup_apply_test.go）：**
```go
// 无 seat → 422；path 已有 .pact → 409；合法 → init（STATE 含 roster）+ wired 结果；TOML kind → docOnly=true。
```

**verify: go test ./internal/serve/**

---

## Task t-sp3-frontend：Setup apply UI（kimi，deps t-sp3-backend）

**Files:**
- Modify: `web/src/components/Setup.tsx`（路径输入 + Apply 按钮 + 结果区）
- Modify: `web/src/lib/api.ts`（`setupApply`）
- Modify: `web/src/components/Setup.test.tsx`

**实现要点（TDD）：**
- [ ] `api.ts` 加 `setupApply(body)`（writeJSON `/api/setup/apply`）。
- [ ] `Setup.tsx`：项目路径输入框 + 项目名（默认 basename）；沿用 suggest 列表 + 角色编辑 + validate；「Apply」按钮（roles 完整启用）→ `setupApply` → 结果区显示 wired 落地 + TOML docOnly snippet + 错误。
- [ ] vitest：Apply 禁用条件、POST body、结果渲染（wired + docOnly + error）。
- [ ] 双绿 → checkpoint。

**verify: cd web && npx tsc --noEmit && npx vitest run**

---

## Task t-sp3-docs：onboarding 文档（claude 直接做）

**Files:**
- Create: `docs/onboarding.md`
- Modify: `README.md`（quickstart 指向 onboarding.md）
- Modify: `~/.pactify/cloudflare-tunnel-pactify.md`（serve --seat 备注）

**要点：**
- [ ] `docs/onboarding.md`：5 分钟接入——装 → `agent register`/扫描 → dashboard Setup 一键 apply（或等价 CLI）→ plan → run → ship；seat 定义详解（id/roles/entry/kind）。
- [ ] README quickstart 更新 + 部署「常驻 serve 需 `--seat`」说明。
- [ ] 命令走查准确（能真跑）。

---

## Self-Review
- Spec 覆盖：setup/apply（backend）+ Setup UI（frontend）+ onboarding.md/README/部署债（docs）全覆盖。
- 无 placeholder：每 task files + 要点 + verify。
- 一致：`/api/setup/apply` body 字段前后端一致（path/project/seats）。
