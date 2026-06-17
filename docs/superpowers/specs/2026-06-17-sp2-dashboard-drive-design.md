# SP2：dashboard 驱动闭环 — 设计 Spec

> **隶属**：[Pactify v1 总纲](2026-06-17-pactify-v1-definition-design.md) 子项目 SP2（依赖 SP1 acting-seat 基线）。
> **日期**：2026-06-17 · 现状锚点经探索核对。
> **执行**：后端 endpoint → **opencode-worker**；前端 UI → **kimi**；claude orchestrator+reviewer。

## 1. 目标
把 dashboard 从 observe-only + plan-review 升级为「浏览器里跑完整闭环」：
Plan apply → Run orchestrate → 暂停态介入（看 diff / 续跑）→ Ship。决策：orchestrate
经 **exec 子进程** 启动（与 serve 生命周期隔离、复用 CLI）；review-gate = resume + 看 diff。

## 2. 后端（opencode-worker，Go）

现状：`internal/serve/orchestrate.go` 已有 `GET .../orchestrate/status` + `/parallel`（只读）；
plan apply endpoint `POST .../plan/{feature}/apply` **SP1 已建**；`cmd_orchestrate.go`/`cmd_finish.go`
可复用；`internal/finish` 有 push/PR。新建 endpoint（均过 `actingProject` 授权，写 .pact）：

| Endpoint | 实现 |
|---|---|
| `POST .../orchestrate/run` | body `{feature?, max_concurrency?}`；从 roster 推断 `--seat-kind`（init seats 第 4 字段 kind；缺则 body 带）；`exec.Command("pactify","orchestrate",...)` 后台 `Start()`（不 Wait），`PACT_AGENT_ID`+env 注入；返回 **202** + status URL。前端轮询已有 status.json。 |
| `POST .../orchestrate/resume` | 同上加 `--resume`；从当前 escalation 态续跑。 |
| `POST .../orchestrate/ship` | body `{remote?,branch?,pr?,title?,body?}`；调 `internal/finish`（push/开 PR）；**同步**返回 `{pushed, pr_url?}`。 |
| `GET .../orchestrate/diff` | `git -C <dir> diff`（可选 `?staged`）；返回 `{diff:string}`，review-gate 看 diff 用。 |

> exec 子进程要求 `pactify` 在 PATH（serve 环境）。run endpoint 防重入：已有运行中（status.json 非 done/escalated 且进程在）拒绝 409。

### 后端测试点
- run：缺 seat → 422；roster 不全 → 422；合法 → 202 + 子进程 Start（测试用注入的 fake exec，断言命令行 + 不阻塞）。
- ship：调 finish（fake），返回 pushed。diff：返回 git diff 文本。resume：命令带 --resume。

## 3. 前端（kimi，React/TS）

现状：视图 canvas(1)/kanban(2)/live(3)/plan(4)/ops(5)/setup(6)/recipes(7)；`api.ts` 有 `writeJSON` POST 模式；
`PlanReview.tsx` 只读；`LiveOrchestrate.tsx` 有 status 轮询 + escalation 横幅；Toolbar 有禁用的 orchestrate 占位。

| UI | 文件 | 改动 |
|---|---|---|
| Plan Apply | `PlanReview.tsx` | 加「Apply」按钮 → `POST .../plan/{feature}/apply` → toast + 刷新 |
| Run | `LiveOrchestrate.tsx`/Toolbar | 「Run」按钮（roster 完整 + 无运行中）→ `POST .../orchestrate/run` |
| Resume + diff | `LiveOrchestrate.tsx` | escalated 时显 escalation 详情 + 「View diff」（`GET .../orchestrate/diff` 渲染）+「Resume」→ `POST .../orchestrate/resume` |
| Ship | `LiveOrchestrate.tsx` | shipped 后「Ship」按钮 → PR title/body 输入 → `POST .../orchestrate/ship` → 显示 PR URL |
| Prune | `ops/OpsView.tsx` | 每 agent「Prune sessions」按钮 → 已有 `POST /api/agents/{kind}/sessions/prune` |

`api.ts` 加封装：`applyPlan/runOrchestrate/resumeOrchestrate/shipFeature/getDiff/pruneSessions`（复用 `writeJSON`）。

### 前端测试点（vitest + 必要 e2e）
- Apply 按钮 POST 正确 URL + body，成功 toast、失败显错误。
- Run 按钮在 roster 不全时禁用；点击 POST run。
- escalated 态显 Resume + View diff；Resume POST。Ship 输入 PR 信息 POST。
- 双绿门：vitest + Playwright e2e（画布工艺规约不破）。

## 4. 执行映射
| 任务 | 座席 | verify |
|---|---|---|
| t-sp2-backend（4 endpoint + 测试） | opencode-worker | `go test ./internal/serve/` |
| t-sp2-frontend（6 处 UI + api 封装 + 测试） | kimi-worker（k2.7） | `cd web && npx vitest run && npx playwright test` |

reviewer=claude（独立重跑 verify）。前端依赖后端 endpoint → t-frontend deps t-backend。

## 5. 验收
1. 后端 4 endpoint 完成、`go test ./internal/serve/` 绿；run 真起子进程（exec 注入测试）。
2. 前端 6 处 UI 完成、vitest + e2e 双绿、tsc clean。
3. 端到端：dashboard 里 Apply→Run→（暂停看 diff→Resume）→Ship 走通（pactify 自身验证）。

## 6. 非目标
自然语言入口（v1.x）、接手按钮（escalation 即交接点，人工处理）、运行历史审计 UI。
