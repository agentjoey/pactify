# SP3：新项目 onboarding 一键化 — 设计 Spec

> **隶属**：[Pactify v1 总纲](2026-06-17-pactify-v1-definition-design.md) 子项目 SP3（推广前置，与 SP2 并行）。
> **日期**：2026-06-17 · 现状锚点经探索核对。
> **执行**：后端 → **opencode-worker**；前端 → **kimi**；文档 → **claude**；claude orchestrator+reviewer。

## 1. 目标
把「新项目从零到能跑」从手工 copy-paste CLI 降到 dashboard 一键 + 一页文档。
针对**全新项目**（无 `.pact`）；已有项目用现有 wire 加 agent（不 re-init，引擎禁止重 init）。

## 2. 后端（opencode-worker，Go）

现状：`GET /api/setup/suggest`（serve/setup.go:31，`wizard.Suggest`→ seat/角色建议）；
`pact.Init`（engine.go:61）；`agent.WireAt`（briefing.go:90，烤 entry + merge MCP）；
单个 wire endpoint `POST .../wiring/{kind}`（wiring.go:50）。**setup apply endpoint 不存在**。

新建 `POST /api/setup/apply`（machine-scoped，过 `requireSeat`）：
- body：`{path:string, project:string, seats:[{id,roles,entry,kind}]}`。
- 步骤（事务感）：① 校验 `path` 存在且**无 `.pact`**（已 init → 409，不 re-init）；
  ② `pact.At(path).As(seat).Init(project, seatSpecs)`（seatSpecs = `id:roles:entry:kind` 格式，复用 `seat.ParseSeat`）；
  ③ 对每个带 kind 的 seat `agent.WireAt(path, kind, id, roles, path)`，收集结果；
  ④ 返回 `{inited:true, wired:[{kind,seat,wrote,path,docOnly,snippet?}], notes}`（TOML kind = docOnly 提示手工）。
- Init 失败 → 不 wire，返回错误；wire 部分失败 → 报告 per-kind 结果（Init 已成功，wire 是 best-effort，前端显示哪些要手工补）。

### 后端测试点
- 无 seat → 422；path 已有 `.pact` → 409；合法 → init + wire，STATE 含 roster，wired 结果正确。
- TOML kind → docOnly=true + snippet。

## 3. 前端（kimi，React/TS）

现状：`Setup.tsx` 能 suggest + validate + 生成 copy-paste 命令，**无 Apply 按钮**；`api.ts` 有 `getSetupSuggest`。

`Setup.tsx` 改造：
- 项目路径输入框 + 项目名（默认 basename）。
- 沿用现有 suggest 列表 + 角色编辑 + validate 徽章。
- 「Apply」按钮（roles 完整时启用）→ `POST /api/setup/apply` → 显示结果：哪些 wire 落地、哪些 TOML doc-only（带 snippet 复制）、错误提示。
- `api.ts` 加 `setupApply(body)`（复用 writeJSON）。

### 前端测试点（vitest）
- Apply 在角色不全时禁用；点击 POST 正确 body。
- 结果区渲染 wired 列表 + docOnly 提示 + 错误。
- 双绿门 vitest（+ 必要 e2e）。

## 4. 文档（claude）
- 新建 `docs/onboarding.md`：**5 分钟接入**全流程——装 pactify → `agent register`/扫描 → dashboard Setup 一键 apply（或 CLI）→ plan → run → ship。含 seat 定义详解（id/roles/entry/kind）。
- 更新 `README.md` quickstart：指向 onboarding.md + UI 一键分支。
- 补**常驻 serve `--seat` 部署说明**（SP1 暴露的债）：写端点需 acting-seat，部署 serve 时配 `--seat <id>`；更新 `~/.pactify/cloudflare-tunnel-pactify.md` 备注。

## 5. 执行映射
| 任务 | 座席 | verify |
|---|---|---|
| t-sp3-backend（setup/apply + 测试） | opencode-worker | `go test ./internal/serve/` |
| t-sp3-frontend（Setup apply UI + 测试） | kimi-worker | `cd web && npx vitest run` |
| t-sp3-docs（onboarding.md + README + 部署说明） | claude | 文档自检（链接/命令准确） |

reviewer=claude。t-frontend deps t-backend；t-docs 独立。

## 6. 验收
1. 后端 setup/apply 完成、`go test ./internal/serve/` 绿。
2. 前端 Setup Apply 完成、vitest 绿、tsc clean。
3. onboarding.md 走查准确（命令可跑通）。
4. 端到端：用一个全新空目录在 dashboard 一键 apply → init + wire 落地 → 能 plan/run（SP4 dogfood 时真验）。

## 7. 非目标
已有项目加座席（引擎不支持 re-init，超 v1）、向导多步动画、agent 自动安装。
