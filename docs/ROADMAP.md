# Pactify — Roadmap

> Last updated: 2026-06-09 | Status: Locked v1 | Owner: Joey

三产品、一个 monorepo、先开源后商业化。商业化边界 = **守 Team**（见 [ADR-001](decisions/ADR-001-open-core-boundary.md)）。

## 产品阶梯

```
Pact-Base   →  读 + 协议机制     →  monitor       →  免费开源
Pact-Squad  →  写 + 可视化编排   →  author        →  主功能免费 + 部分付费 feature（后定义）
Pact-Team   →  协作 + 云         →  collaborate   →  付费商业化
```

**核心红线**：Phase 0 dogfood gate 是所有后续前提。skill 跑不通"消灭人肉中继"，Phase 1-6 都不开工。

---

## Phase 0 — 流程验证（Skill）｜当前 Sprint 001

- Claude skill 实现 pact 协议（手写文件 + 状态转换）
- **Exit Gate**：Pactify 自身走完完整 6 段，人只说"开始"不传上下文
- ✅ 过 → 进 Phase 1 ｜ ❌ → pivot

---

## PACT-BASE（免费开源）

### Phase 1 — 协议地基 + CLI + 本地监控
- **M1.1 通讯约定冻结**
  - `log.jsonl` 事件 schema（与 MCP 工具语义对齐）
  - `.pact/` 文件契约 JSON Schema
  - `docs/specs/pact-protocol.md`（他人可独立实现）
- **M1.2 CLI v1（Go）**
  - `init/join/status/checkpoint/accept/merge/log/validate`
  - Claude skill 退化为薄封装（调 CLI）
- **M1.3 `pactify serve`（MCP server + 只读 dashboard）**
  - MCP：事件订阅 + 工具暴露
  - fsnotify → SSE → Vite/React 本地 dashboard（`go:embed`）
  - 展示：active agents / task 看板 / 事件时间线 / blocked
  - 验收：Pactify 自身开发全程 CLI + serve 监控

### Phase 2 — 多 agent 覆盖 + 开源分发
- **M2.1** 跨 agent 薄封装（opencode / Gemini / Codex）+ Claude↔opencode 端到端
- **M2.2** 分发：GitHub releases + brew tap + `curl|sh` + `go install`
- **M2.3** Claude marketplace 上架（skill 打包）
- **M2.4** pactify.dev 官网 v1（协议文档 + 5 分钟快速开始）

---

## PACT-SQUAD（主功能免费 + 部分付费 feature）

### Phase 3 — 可视化编排
- **M3.1** 任务编排画布（React Flow：构建/拆解/依赖）
- **M3.2** 可视化派发 + 实时追踪（拖拽派发、checkpoint 提醒）
- **M3.3** agent 通讯可视化（事件流实时图、谁在等谁）
- **M3.4** 本地优先；为 Team 云同步预留事件 relay 接口
- 可视化编排**主功能免费**；**部分高级 feature 付费**（具体后期定义）
- 验收：不写命令，纯 UI 跑完一个 feature 的编排

---

## PACT-TEAM（付费 · 商业化）

### Phase 4 — 云平台（自建）
- **M4.1** 云端事件 relay（daemon 上报，复用 log schema 零改动）
- **M4.2** 托管 Mission-control（多 repo / 多设备 / 团队视图）
- **M4.3** 推送式 daemon（拉取→推送升级，建在 MCP/A2A 上）
- **M4.4** Auth + RBAC + 团队邀请 + 审计导出
- ⚠ **架构约束**：Next.js standalone output，**不绑定 Vercel 专属特性**（KV/Edge Config/Vercel-only middleware），保证可迁自托管 / Cloudflare / 阿里云
- Supabase 在大规模商业化前可继续使用

### Phase 5 — 增长 + 变现
- **M5.1** 多市场入驻（Claude / Cursor / VS Code / JetBrains）
- **M5.2** 企业版（SSO / 私有部署 / SLA / 合规）
- **M5.3** 生态（GitHub Actions native / 第三方 agent adapter）
- **M5.4** 开源社区（贡献者体系 / adapter 生态）

### Phase 6 — 中国 Go-to-Market（独立）
- 云端迁出 Vercel（自托管 / 阿里云 / Cloudflare）
- 本地化分发（国内市场入驻、合规、支付）
- 中文官网 + 社区

---

## 技术地基决策（贯穿全程）

| 决策 | 选型 |
|---|---|
| CLI 语言 | **Go**（单静态二进制，零 runtime 依赖，agent shell 调用） |
| 事件总线 | `log.jsonl`（append-only，git 友好，shell + MCP 写同一种事件） |
| 实时通讯 | `pactify serve` = MCP server + SSE/WS（本地）→ 云端 relay（Phase 4） |
| 本地 dashboard | Vite + React SPA，`go:embed` 嵌入 binary |
| 云端 app | Next.js standalone + Tailwind + shadcn/ui（与本地共享 React 组件）|
| 可视化 | React Flow（编排画布）+ 时间线 |
| 后端（Phase 4）| Supabase（可自托管，相对安全）|
