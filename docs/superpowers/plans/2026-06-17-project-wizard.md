# 项目管理向导 Implementation Plan

> **执行**：orchestrate（pact 任务图）。t-projwiz-backend（opencode）→ t-projwiz-frontend（kimi，deps backend）。reviewer=claude。一轮跑（dep 保证后端先）。

**Goal:** dashboard sidebar 加「添加新项目」向导（fs 浏览选文件夹 + 自动分辨新建/已有 + 配 agent/seat + 分组）+ 删除项目。

**Architecture:** A 方案——dashboard 项目组分组（`Project.Group` 标签，每文件夹仍独立 pactify 项目）。后端加 fs-browse endpoint + group 入参；前端 Sidebar 分组 + 两步向导 + FolderPicker。

**Tech Stack:** Go net/http（internal/serve、internal/registry）；React19+TS+vitest+Playwright。

**Spec:** `docs/superpowers/specs/2026-06-17-project-wizard-design.md`

---

## Task t-projwiz-backend：fs browse + group（opencode）

**Files:**
- Create: `internal/serve/fsbrowse.go` + `fsbrowse_test.go`
- Modify: `internal/registry/registry.go`（`Project` 加 `Group`；`Register` 透传）
- Modify: `internal/serve/registry.go`（`registerReq` 加 `Group`，透传）
- Modify: `internal/serve/setup.go`（`setupApplyReq` 加 `Group`；init+wire 后**自注册进 registry** 带 group）

**实现要点（TDD：先写失败测试→红→实现→绿）：**
- [ ] `GET /api/fs/browse?path=<abs>`（注册路由 + `handleFsBrowse`）：`requireSeat`(422)；空 path→`os.UserHomeDir`；`os.ReadDir` 列**子目录**（跳过非目录 + `.git`/`node_modules`/隐藏 `.`-前缀目录）；每项 `{name, path, isGit:含.git, hasPact:含.pact}`；返回 `{path, parent, entries}`；非法/不可读 path→400。
- [ ] `registry.Project` 加 `Group string \`json:"group,omitempty"\``；`Register(name, path, group)` 持久化；GET registry DTO 带 group。
- [ ] `registerReq` + `setupApplyReq` 各加 `Group string`；setup-apply 在 init+wire 成功后调 `registry.Register(project, path, group)`（注册进 dashboard）。
- [ ] verify 绿 → checkpoint。

**测试骨架（fsbrowse_test.go）：**
```go
// 无 seat → 422；合法 path → 列子目录，标 isGit/hasPact，跳过文件 + .git；空 path → home；非法 path → 400。
// 用 t.TempDir() 造 dir 树（含一个 .git 子目录 + 一个 .pact 子目录 + 一个普通文件）。
```

**verify: go test ./internal/serve/ ./internal/registry/**

---

## Task t-projwiz-frontend：Sidebar 分组 + 向导 + FolderPicker + 删除（kimi，deps t-projwiz-backend）

**Files:**
- Modify: `web/src/components/shell/Sidebar.tsx`（按 group 分组渲染 + 「+ 添加新项目」按钮 + 每行删除按钮）
- Create: `web/src/components/shell/AddProjectWizard.tsx` + test
- Create: `web/src/components/shell/FolderPicker.tsx` + test
- Modify: `web/src/lib/api.ts`（`browseFs`、`postRegister(+group)`、`setupApply(+group)`）

**实现要点（TDD：先写失败 vitest→红→实现→绿）：**
- [ ] `api.ts`：`browseFs(path?)→{path,parent,entries}`；`postRegister(path,{name?,group?})`；`setupApply({path,project,seats,group})`（复用 writeJSON）。
- [ ] `FolderPicker.tsx`：调 `browseFs`，面包屑 + 「上一级」+ 子目录列表（每项 checkbox 多选 + 标 `hasPact`→「已是项目」/`isGit`→「git」）；跨目录累积选中绝对路径数组。
- [ ] `AddProjectWizard.tsx`：两步 modal。Step1 = 组名输入 + FolderPicker。Step2 = 对 `hasPact=false` 文件夹用 `getSetupSuggest` 拉 roster 让用户编辑（+「套用上一个 roster」快捷）；`hasPact=true` 跳过。提交逐文件夹分流：`hasPact`→`postRegister(path,{group})`；新→`setupApply({path,project,seats,group})`；汇总结果 + docOnly 提示。
- [ ] `Sidebar.tsx`：按 `group` 分组渲染（有组→可展开节点；无组→平铺）；header「+ 添加新项目」开 wizard；每项 hover 出删除按钮→确认弹窗→`deleteRegistry`。
- [ ] vitest 覆盖 FolderPicker 导航/多选/标注、wizard 分流、Sidebar 分组 + 删除确认；e2e 走「添加→选文件夹→提交→项目出现」。画布工艺规约不破。
- [ ] 双绿 → checkpoint。

**verify: cd web && npx tsc --noEmit && npx vitest run && npx playwright test**

---

## Self-Review
- Spec 覆盖：fs browse + group（backend）+ Sidebar 分组/向导/FolderPicker/删除（frontend）全覆盖。
- 无 placeholder：每 task files + 测试/实现要点 + verify。
- 一致：`browseFs`/`postRegister`/`setupApply` 字段（path/group/project/seats）前后端一致；`Project.Group` 贯穿 registry→DTO→sidebar。
