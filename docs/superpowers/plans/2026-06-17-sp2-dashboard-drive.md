# SP2 dashboard 驱动闭环 Implementation Plan

> **执行**：orchestrate（pact 任务图）。t-sp2-backend（opencode）→ t-sp2-frontend（kimi，deps backend）。reviewer=claude。

**Goal:** dashboard 里跑完整闭环：Plan apply → Run orchestrate（exec 子进程）→ 暂停态看 diff/续跑 → Ship。

**Architecture:** 后端 4 个 HTTP endpoint（run/resume/ship/diff，run/resume 经 exec 子进程与 serve 隔离）+ 前端 6 处 UI。plan apply endpoint SP1 已建，仅缺前端按钮。

**Tech Stack:** Go net/http ServeMux（internal/serve）、os/exec；React19+TS+vitest+Playwright（web/）。

**Spec:** `docs/superpowers/specs/2026-06-17-sp2-dashboard-drive-design.md`

---

## Task t-sp2-backend：orchestrate run/resume/ship/diff endpoints（opencode）

**Files:**
- Modify: `internal/serve/orchestrate.go`（加 4 路由 + handler）
- Create: `internal/serve/orchestrate_drive_test.go`
- 复用：`internal/serve/author.go`（actingProject）、`internal/finish`（ship）、`cmd_orchestrate.go`（命令形态参考）

**实现要点（TDD：先写失败测试 → 红 → 实现 → 绿）：**

- [ ] **run** `POST /api/projects/{id}/orchestrate/run`，body `{feature?:string, max_concurrency?:int}`：
  - `s.project(id)`(404) → `s.actingProject(dir)`(422)。
  - 防重入：读 status.json，若运行中（非 done/escalated）→ 409。
  - 从 roster 推断 `--seat-kind`：遍历 STATE seats，每个有 kind 的 seat 生成 `seat=kind`（projection.Seat 的 kind 字段；init seats 第 4 段）。
  - 经**注入的 runner**（seam，便于测试）后台启动：生产 = `exec.Command(pactifyBin, "orchestrate", "--feature", f, "--as", s.seat, "--seat-kind", ...)`，设 `PACT_AGENT_ID=s.seat`，`cmd.Start()`（不 Wait），返回 **202** `{status_url}`。
  - 测试用 fake runner 断言命令行 + 不阻塞。
- [ ] **resume** `POST .../orchestrate/resume`：同 run，命令加 `--resume`。
- [ ] **ship** `POST .../orchestrate/ship`，body `{remote?,branch?,pr?,title?,body?}`：actingProject → 调 `finish.Push`/`finish.OpenPR`（参考 cmd_finish.go），**同步**返回 `{pushed:true, pr_url?}`。
- [ ] **diff** `GET .../orchestrate/diff`：`exec git -C dir diff`（`?staged` → `--staged`），返回 `{diff:string}`。
- [ ] verify 全绿 → checkpoint。

**测试骨架（orchestrate_drive_test.go）：**
```go
// run 无 seat → 422；运行中 → 409；合法 → 202 + runner 收到正确 argv（fake runner）。
// ship → finish 被调（fake）返回 pushed。 diff → 返回 git diff 文本。
```

**verify: go test ./internal/serve/**

---

## Task t-sp2-frontend：Apply/Run/Resume/diff/Ship/Prune UI（kimi，deps t-sp2-backend）

**Files:**
- Modify: `web/src/lib/api.ts`（加封装）
- Modify: `web/src/components/PlanReview.tsx`（Apply）
- Modify: `web/src/components/LiveOrchestrate.tsx`（Run/Resume/diff/Ship）
- Modify: `web/src/components/ops/OpsView.tsx`（Prune）
- Create/Modify: 对应 `*.test.tsx`

**实现要点（TDD：先写失败 vitest → 红 → 实现 → 绿）：**

- [ ] `api.ts` 加：`applyPlan(project,feature)`、`runOrchestrate(project,body)`、`resumeOrchestrate(project)`、`shipFeature(project,body)`、`getDiff(project)`、`pruneSessions(kind)`（复用 `writeJSON`/fetch 模式）。
- [ ] `PlanReview.tsx`：加「Apply」按钮 → `applyPlan` → 成功 toast/刷新、失败显错误。
- [ ] `LiveOrchestrate.tsx`：
  - 非运行态 + roster 完整 → 「Run」按钮 → `runOrchestrate`。
  - escalated 态 → escalation 详情 + 「View diff」（`getDiff` 渲染 `<pre>`）+「Resume」→ `resumeOrchestrate`。
  - shipped 态 → 「Ship」按钮 → PR title/body 输入 → `shipFeature` → 显示 PR URL。
- [ ] `OpsView.tsx`：每 agent「Prune sessions」按钮 → `pruneSessions`。
- [ ] vitest 覆盖每个按钮的 POST URL/body + 成功/失败态；必要 e2e（Apply→Run 流）。画布工艺规约不破。
- [ ] 双绿 → checkpoint。

**verify: cd web && npx tsc --noEmit && npx vitest run && npx playwright test**

---

## Self-Review
- Spec 覆盖：run/resume/ship/diff（backend）+ Apply/Run/Resume/diff/Ship/Prune（frontend）全覆盖。
- 无 placeholder：每 task 有 files + 测试/实现要点 + verify。
- 一致：endpoint 路径前后端一致（`/orchestrate/{run,resume,ship,diff}`、`/plan/{feature}/apply`）。
