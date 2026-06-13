# Pactify UI 设计 spec（2026-06-14）

把 `roadmap-next.md` B 部分的高层 UI 规划细化成可实施 spec：每个面给布局、组件、状态、数据流、所需端点 + ASCII mockup。复用已锁定的 design token 体系（`tokens.css`）+ 已建原语（Spinner/Button-loading/Alert/EmptyState/Badge + 微交互 hover-lift/fade-rise）。

## 设计原则
1. **一条可点的链**：dashboard 顶栏视图 = 用户旅程的阶段。现有 5 视图（Kanban/Canvas/Ops/Live/Plan key 1-5），新增 **Setup(0/6)** 与 **Recipes**，把 `注册→配座席→配方/规划→评审→编排→交付` 串成可点流程。
2. **后端已就绪，UI 是消费层**：每个面对应已建的 serve endpoint/CLI；新面优先复用现有原语，少造轮子。
3. **状态四态一致**：loading（Spinner/Skeleton）、empty（EmptyState）、error（Alert + retry）、success——每个数据面都覆盖（#12 polish 的延续）。
4. **危险操作显式确认**：init/wire/apply/merge/push 这类有副作用的操作走二次确认 + loading 态。

## 顶栏视图编排（建议最终态）
```
[logo] [project ▾]   Setup  Kanban  Canvas  Ops  Recipes  Plan  Live   [⌘K] [● live] [seat]
                       0      1       2      3     —        5     4
```
（Setup 仅在"项目未配座席"时高亮提示；Recipes/Plan/Live 是编排链）

---

## 1. Setup 视图（#1 向导）—— 体验起点

**数据**：`GET /api/setup/suggest`（已建）→ `{bindings:[{seat,kind,roles,drivable}], warnings:[]}`。新增 `POST /api/setup/apply`（跑 init + agent wiring）。

**布局/状态**：
```
┌─ Setup：把注册的 agent 配进这个项目 ───────────────────┐
│ 检测到 3 个已注册 agent，建议座席分工：                 │
│                                                         │
│  座席      agent          角色            可驱动         │
│  ┌─────────────────────────────────────────────────┐   │
│  │ claude   claude-code   [orch,reviewer ▾]  drivable│  │
│  │ opencode opencode      [worker ▾]         drivable│  │
│  │ gemini   gemini-cli    [worker ▾]         drivable│  │
│  └─────────────────────────────────────────────────┘   │
│  ⚠ (Alert warn) 若缺 worker：…                          │
│                                                         │
│  [ Apply（init + wire） ]  ← loading 态 + 二次确认       │
└─────────────────────────────────────────────────────────┘
```
- 空注册表 → EmptyState"先去 Ops 注册 agent"+ 跳 Ops 按钮。
- 角色可改（下拉）；改后本地重算 warnings（复用 wizard.Validate 逻辑，前端镜像或调 endpoint）。
- Apply 成功 → toast + 跳 Ops/Kanban。

**组件**：复用 Alert（warnings）、Button（loading）、Select、EmptyState。

---

## 2. Recipes 视图（#11 配方）—— 降门槛入口

**数据**：新增 `GET /api/recipes`（list）+ `POST /api/recipes/{name}/expand {goal}`（预览展开）+ `POST .../generate {goal,feature}`（写 plan manifest）。后端 `internal/recipe` 已有 Get/Names/Expand。

**布局**：左配方列表 + 右目标输入 + 实时预览：
```
┌─ Recipes ───────────────────────────────────────────────┐
│ 配方                    │ 目标                            │
│ ○ add-tests            │ ┌─────────────────────────────┐ │
│ ● review-harden  ←选中  │ │ 给缓存层加并发安全           │ │
│ ○ spec-to-plan         │ └─────────────────────────────┘ │
│                        │ 预览（实时展开）：               │
│ "实现+独立评审加固"     │  t-impl   (no deps)             │
│                        │  t-harden (deps: t-impl)        │
│                        │  [ Generate plan → 去 Plan 复审 ]│
└─────────────────────────────────────────────────────────┘
```
- 选配方 + 填目标 → 调 expand 实时预览任务卡（owner/reviewer 占位 + deps）。
- "Generate plan" → 写 `.pact/plan-<feature>.json` → 自动切到 **Plan 视图(#7)**。
- **价值**：Recipes → Plan review → Run 一条链点通。

---

## 3. Plan 视图增强（#7，已建只读）

**已建**：PlanReview 面板（只读，Plan 视图 key 5）。
**增量**：
- 任务行内编辑 owner/reviewer/deps（下拉，座席来自 state.agents）。
- "Apply"（→ 新增 `POST .../plan/{feature}/apply`，包 `plan apply` + 事务化回滚）。
- "Run orchestrate"（→ 见面 5）。
- 人审默认开关（设置项）。
```
┌─ Plan review · review-harden → feat-rh    [valid ✓] ─────┐
│  t-impl    [opencode ▾]→[claude ▾]   verify: go test     │
│  t-harden  [opencode ▾]→[claude ▾]   deps:[t-impl ▾]     │
│                                                          │
│  [ Apply 任务图 ]   [ Apply + Run orchestrate ]          │
└──────────────────────────────────────────────────────────┘
```

---

## 4. Agent Config 面板（#10/#9/#4，Ops 内）

**数据**：新增 `GET/POST /api/agents/{kind}/config`（后端 agentreg.Config/SetConfig 已有）。
```
┌─ Ops · Agents ──────────────────────────────────────────┐
│ opencode    drivable   model:[deepseek-v4-pro ▾]         │
│                        权限:( ●blanket  ○scoped )        │
│ claude-code drivable   model:[claude-opus-4-8 ▾]         │
│                        权限:( ○blanket  ●scoped )        │
│                        允许工具:[Read,Edit,Bash …]       │
│ antigravity manual     —（不可驱动，仅人工交接）          │
└──────────────────────────────────────────────────────────┘
```
- model 下拉、权限姿态 radio（blanket↔scoped）、scoped 时显工具多选。
- drivable/manual 徽章（scan 信号）。

---

## 5. Run 控制 + 命令面板（#3 + 自然语言面板）

**数据**：新增 `POST /api/projects/{id}/orchestrate {feature?,maxConcurrency}`（启动，**需安全设计**：仅本地、acting-seat 校验、可能要确认弹窗）。并行进度读已建 `GET .../orchestrate/parallel`（本次已实现聚合）。

**命令面板**（⌘K 之外的对话入口）：
```
┌─ Run ───────────────────────────────────────────────────┐
│ 💬 说一句话，我来拆/选配方/跑：                          │
│ ┌──────────────────────────────────────────────────┐   │
│ │ 给 relay 加重试上限可配                            │   │
│ └──────────────────────────────────────────────────┘   │
│ → 建议：配方 review-harden / planner 拆解  [预览] [跑]   │
│                                                          │
│ 并发:[2 ▾]   features:[全部 ▾]   [ Start orchestrate ]   │
│                                                          │
│ ⏳ 等待你确认（人在环）：t-harden 改了 X，要继续吗？     │
└──────────────────────────────────────────────────────────┘
```
- 并行进度 → Live 视图聚合卡片（本次已建 parallel-panel）。

---

## 6. Review Gate / 升级 UX（#4 人审门）

**数据**：escalation 文件 + 新增 `POST .../orchestrate/resume`（批准续跑/打回）+ review-gate 开关。
```
┌─ Live · feat-rh 已暂停 ──────────────────────────────────┐
│ ⊘ 硬测试门失败：go test ./... → FAIL TestRetry          │
│ [ 看 diff ]                                              │
│ [ 批准合并 ]  [ 打回(reason…) ]  [ 我来接手 ]  [ 续跑 ]  │
└──────────────────────────────────────────────────────────┘
```
- review-gate 开关（全自动↔每棒/合并前停）放设置或 Run 面板。

---

## 7. Ship 按钮（#5 收尾交付步）

feature shipped 到本地 main 后，dashboard 显 Ship：
```
✅ feat-rh 已合入本地 main   [ Push origin ]  [ 开 PR ]
```
**数据**：新增 `POST /api/projects/{id}/finish {pr?,head,title}`（后端 internal/finish 已有）。

---

## 8. Sessions / #12 polish 收尾
- **Sessions**（Ops 内）：每 agent "prune sessions" 按钮（接 sessions endpoint），unsupported 显灰禁用。
- **#12 收尾**：Ops 三面板空态 + loading 骨架；fetch 失败用 Alert（现静默）；Board 空态；focus ring 审计；按钮跨视图一致性。

---

## 实施顺序（建议）
1. **#12 收尾 + Setup(#1 UI)**：门面 + 体验起点。
2. **Recipes(#11) → Plan apply(#7) → Run(#5 NL/启动)**：把"说人话就能跑"链点通。
3. **Review Gate(#4) + Ship(#5收尾)**：可信 + 闭环终点。
4. **Agent Config(#10/#9/#4) + Sessions(#6)**：配置面。

**所需新增 serve endpoint 汇总**：`POST /api/setup/apply`、`GET /api/recipes`、`POST /api/recipes/{name}/expand|generate`、`POST .../plan/{feature}/apply`、`GET/POST /api/agents/{kind}/config`、`POST /api/projects/{id}/orchestrate`、`POST .../orchestrate/resume`、`POST /api/projects/{id}/finish`。

> 所有 UI 改动走 CLAUDE.md 画布合并门（vitest + Playwright e2e 双绿）。office-zoom 既有 flake 需先单独修/隔离。
