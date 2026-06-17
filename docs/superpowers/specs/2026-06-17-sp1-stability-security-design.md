# SP1：稳定性内核 + 安全横切 — 设计 Spec

> **隶属**：[Pactify v1 总纲](2026-06-17-pactify-v1-definition-design.md) 的子项目 SP1（地基，SP2 依赖它）。
> **日期**：2026-06-17 · **起草**：claude (opus-4.8) · 现状锚点经三路探索 + 代码核对。
> **执行**：claude（①③ 引擎/编排核心）+ opencode（②④ Go 后端），无前端（kimi 从 SP2 起）。

## 1. 目标

把 v1 的稳定性与安全地基补齐，四个互相独立的工作块：① 错误处理智能分类恢复、
② plan apply 事务化、③ post-merge STATE 滞后 bug 修复、④ acting-seat 授权基线。
不做前端确认弹窗——公网入口已由 Cloudflare Access OTP 认证本人，acting-seat 在应用层授权即足够。

四块互不依赖，可并行三 agent 跑。每块自带 TDD 测试，块级双绿（`go test ./...`）。

---

## 2. 块 ① 错误处理：智能分类恢复（claude）

### 现状锚点
- 主循环 `internal/orchestrate/loop.go:95-191`（`Run`）：软失败后 `h.Fails[task]++`，下一迭代 `tripped()` 达 `MaxFails` → escalate。
- worker 软失败处理在 `loop.go:196-230`（`runOwner`），错误分支 line 209-217。
- 重试 briefing：`brief.go:18-55`（`workerBrief`），`retrying = h.Fails[task] > 0`（loop.go:206）已让 worker「读半成品续/重做」。
- verify 提取：`gate.go:27-42`（`extractVerify`，约定 spec 内 `verify: <cmd>` 行）；执行：`gate.go:71-80`（`runGate` → `(ok, detail)`）；无 verify 行回退 `fallbackGate`（loop.go:25 = `go build ./... && go test ./...`）。
- checkpoint 引擎：`pact.At(dir).As(seat).Checkpoint(taskID, evidence)`（engine.go:337，含 `gitx.CommitAll`）。
- 阈值：`decide.go:40` `Thresholds{MaxRework, MaxFails, MaxIters}`。

### 改动设计
在 `runOwner` 的软失败分支（loop.go:209-217）**计入 `h.Fails++` 之前**，插入分类恢复：

新增 `func (opts Options) classifyAndCheckpoint(ctx, act Action, ownerSeat string) bool`：
1. `readSpec`（loop.go:532）读 task spec → `extractVerify` 取 verify 命令（无则 `fallbackGate`）。
2. `runGate(ctx, opts.Exec, opts.Dir, cmd)` 跑验收。
3. **过** → `pact.At(opts.Dir).As(ownerSeat).Checkpoint(act.Task, evidence)`（evidence = gate 输出摘要 `summarize`），返回 `true`。
4. **不过/出错** → 返回 `false`。

`runOwner` 改：软失败后先调 `classifyAndCheckpoint`；返回 true 则重投影确认 task 已 `awaiting_review` → `h.Fails[task]=0` → return nil（不重烧 worker，下一迭代 nextAction 自然走 RunReviewer）；返回 false 走原 `h.Fails++` 重试路径。

**语义**：「活干完只是 checkpoint 没打」→ 系统补 checkpoint 进评审；「活没干完」→ 重试 worker 续接（现状不变）。

### 测试点（TDD）
- soft-fail 后 verify 过 → 自动 checkpoint，task 转 awaiting_review，`Fails` 不增、worker 不重启（用 fake runner + fake exec 让 gate 返回 ok）。
- soft-fail 后 verify 不过 → `Fails++`，进重试（gate 返回非零）。
- 连续 verify 不过到 `MaxFails` → escalate（沿用现有 escalate 测试骨架）。

---

## 3. 块 ② plan apply 事务化（opencode）

### 现状锚点
- plan manifest 结构：`internal/planner/manifest.go:19-23`（`Plan{Feature,Branch,Tasks[]PlanTask}`，`PlanTask{ID,Owner,Reviewer,Spec,Verify,Deps}`），文件 `.pact/plan-<feature>.json`。
- 校验：`planner.Plan.Validate(roster)`（manifest.go:36-112，空字段/roster/owner≠reviewer/无环）。
- 现有 apply（**非事务**，CLI 用）：`internal/planner/apply.go:11-30`（逐个 `pact.At(dir).As("claude").Assign(...)`，中途失败留半成品）。
- serve 写范式：`author.go:138-163`（`handleAuthorAssign` → `actingProject` 校验 → `s.projectMu(id)` 串行 → `proj.Assign`）。
- 引擎 assign：`engine.go:191-228`（`checkAssign`+`checkDeps`+`appendAndRender`）；事件 append：`event/log.go:10-31`（`O_APPEND`）；STATE 重算：`engine.go:49-58`（`appendAndRender` 读全 log → 投影 → `WriteState`）。
- GET plan endpoint：`plan.go:33-82`（`handlePlanReview`）。

### 改动设计
新建 `POST /api/projects/{id}/plan/{feature}/apply`（注册在 `plan.go` 的 `registerPlanRoutes`）：
1. `s.project(id)` 解析 + `s.actingProject(dir)` 校验 seat（422 if 无/非 roster）。
2. 读 `.pact/plan-<feature>.json` → `planner.Parse` → `Plan.Validate(rosterOf(dir))`（400 if 非法）。
3. 持 `s.projectMu(id)`（与单 assign 共用，防交错）。
4. **事务化**（新增 `planner.ApplyTx(dir, plan, roster, seat)`）：
   - 预检每个 task：`checkAssign`+`checkDeps`（基于当前 + 累积投影），任一失败 → 整体 422，零写入。
   - 全部预检过 → 记录 `log.jsonl` 当前字节大小 → 逐个 `Assign` append。
   - 任一 append 失败 → `os.Truncate(log, origSize)` 回滚 + 重算 STATE → 返回错误。
   - 全部成功 → 返回 `{assigned: N}`。

> 关于回滚原子性：append-only 日志无 retract 事件；用「记录原始大小 → 失败截断」实现事务回滚。截断后必须重投影 `WriteState` 让 STATE 与 log 一致。

### 测试点（TDD）
- 合法 plan → 全 task assigned，STATE 含全部，`{assigned:N}`。
- 第 k 个 task 非法（如 dep 不存在）→ 预检阶段整体拒绝，log.jsonl **零新增**，STATE 不变，422。
- 中途 append 失败（注入写错误）→ 截断回滚，log 字节数回到原值，STATE 一致。
- 无 acting-seat → 422；plan 文件缺失 → 400。

---

## 4. 块 ③ post-merge STATE 滞后 bug 修复（claude）

### 根因（已定位）
`internal/pact/engine.go:231-270`（`Merge`）：
1. line 250-265：feature 有独立分支时 `CommitAll(ledger)` → `Checkout(base)` → `MergeNoFF(branch)`。
   **merge commit 带的是 feature 分支 HEAD 的 STATE.yml**（此时 merge 事件尚未 append，STATE 仍为 in_progress/accepted）。
2. line 266-269：`appendAndRender(merge event)` append 事件 + 重算 STATE.yml=shipped 写**工作树**，**但不 git commit**。

结果：工作树 STATE.yml=shipped、HEAD（merge commit）STATE.yml=滞后。in-place 串行路径（无独立分支）不 commit 同样留工作树脏。

### 改动设计
`Merge` 在 `appendAndRender`（line 266）**之后**追加一次提交，把 `.pact/`（log.jsonl + STATE.yml）变更落进 HEAD：
- `gitx.CommitAll(p.dir, "pact "+feature+": merge (state shipped)")`（仅当有变更）。
- 独立分支路径：该 commit 落在 base 上，HEAD STATE.yml=shipped，与工作树一致。
- in-place 路径：同样 commit，HEAD=shipped。

> 复用 `gitx.CommitAll`/`gitx.HasChanges`（已用于 ledger）。确保 merge 事件提交是 Merge 的**最后一步**。

### 测试点（TDD）
- 独立分支 feature merge 后：`git show HEAD:.pact/STATE.yml` 的 feature 状态 = shipped（= 工作树）。
- in-place 串行 feature merge 后：工作树 clean（无未提交 .pact 变更），HEAD STATE=shipped。
- 回归：现有 merge 测试（serial in-place no-op merge、并行 worktree merge）仍绿。

---

## 5. 块 ④ acting-seat 授权基线（opencode）

### 现状锚点（副作用端点 + seat 覆盖）
| 端点 | handler | 类别 | 现状 seat 闸 |
|---|---|---|---|
| POST `/projects/{id}/tasks` | handleAuthorTask | project | ✅ actingProject |
| POST `/projects/{id}/verbs/{assign,accept,changes,merge}` | author.go | project | ✅ actingProject |
| PUT `/projects/{id}/squad/layout` | handlePutLayout | project(sidecar) | ⚠️ 无（布局元数据，低敏） |
| POST/DELETE `/agents/{kind}/register` | handleAgentRegister/Unregister | machine | ❌ 无 |
| POST `/agents/{kind}/config` | handleAgentConfigSet | machine | ❌ 无 |
| POST/DELETE `/manifests`、`/manifests/{kind}` | handleManifestCreate/Delete | machine | ❌ 无 |
| POST/DELETE `/registry`、`/registry/{name}` | handleRegistryAdd/Delete | machine | ❌ 无 |
| POST `/agents/{kind}/sessions/prune` | handleSessionsPrune | machine | ❌ 无 |
| POST `/recipes/{name}/expand` | handleRecipeExpand | machine | ❌ 无 |
| POST `/projects/{id}/wiring/{kind}` | handleWire | project(.pact 写) | ❌ 无 |

acting seat 校验：`author.go:48-67`（`actingProject`：`s.seat==""`→拒；非 roster→拒）。

### 改动设计（分层基线）
- **project-scoped 协议写**（已含 + 新覆盖 `handleWire`）：要求 `actingProject` 成功（seat 配置 + ∈ roster）。`handleWire` 写 `.pact` 配置，补上 `actingProject` 校验。
- **machine-scoped 操作**（register/config/manifest/registry/recipe/prune）：不要求 roster 成员（机器级），但加最低闸 `requireSeat()`——`s.seat==""` → **422** fail-closed（「需配置 acting seat」，与现有 `actingProject` 的 422 一致）。新增 helper `func (s *Server) requireSeat(w http.ResponseWriter) bool`。
- **layout sidecar**：维持现状（低敏元数据），不加闸，spec 注明。

> 语义：Access OTP 认证「是本人」，seat 闸保证「有一个授权操作者」。machine-scoped 不绑 roster（roster 是 project 概念）。

### 测试点（TDD）
- machine-scoped 端点在 `s.seat==""` → 422，不执行副作用。
- machine-scoped 端点在 seat 已配置（无需 roster）→ 正常执行。
- `handleWire` 无 seat / 非 roster → 拒；合法 → 执行。
- 回归：现有 author project 写端点测试仍绿。

---

## 6. 验收（SP1 块级）

1. 四块 deliverable 完成，`go test ./...` 全绿（每块自带新测试）。
2. ③ 修复后：merge 一个 feature，`git show HEAD:.pact/STATE.yml` 与工作树一致（shipped）。
3. ④ 后：无 acting-seat 时所有副作用端点 fail-closed；有 seat 时 project 写需 roster、machine 操作放行。
4. ① 后：构造 soft-fail + verify-pass 场景，系统自动 checkpoint 而非重烧 worker。
5. ② 后：非法 plan apply 零写入、合法 plan 全 assign。

## 7. 执行映射（self-dogfood orchestrate）

| 块 | 座席 | 类型 |
|---|---|---|
| ① 智能分类恢复 | claude | 编排器核心逻辑（reviewer=claude） |
| ③ Merge STATE 修复 | claude | pact 引擎核心 |
| ② plan apply 事务化 | opencode | Go 后端 endpoint + planner.ApplyTx |
| ④ acting-seat 基线 | opencode | Go 后端中间件 |

claude 兼 orchestrator + reviewer；两条铁律（worker 不自接受、全 task accepted 才 merge）。块间无依赖，任务图可并行。

## 8. 非目标（SP1 不做）
前端任何改动、确认弹窗、沙箱分级（沿用现有 per-run 权限姿态）、orchestrate/finish 新端点（属 SP2）、并行 token 汇总。
