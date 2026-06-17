# 项目管理向导（添加/删除项目 + 文件夹选择 + 分组）— 设计 Spec

> **日期**：2026-06-17 · 现状锚点经探索核对。属「端到端 UX 重做」方向的第一块。
> **执行**：后端 → **opencode**；前端 → **kimi**；claude orchestrator+reviewer。

## 1. 背景与目标
dashboard 当前无法在 UI 里添加/删除项目（只能 CLI `pactify init`/手工注册）。本功能在左侧 sidebar 加：
① 「添加新项目」向导（**选文件夹不手输** + 自动分辨新建 vs 已有 + 第二步配 agent/seat）；
② 已有项目「删除」；③ 多文件夹聚合为一个 dashboard **项目组**。

**关键约束（已与用户对齐）**：
- 浏览器拿不到本地文件夹绝对路径 → 靠**后端目录浏览** endpoint 实现"选择不手输"。
- 「一个项目聚合多文件夹」= **dashboard 项目组分组**（A 方案，零协议改动）：每个文件夹仍是独立
  pactify 项目（各自 `.pact` + orchestrate），`Group` 只是分组标签。
- 删除 = 仅取消 dashboard 注册，**不动** `.pact`/代码。

## 2. 数据模型
- `internal/registry/registry.go`：`Project` 加 `Group string` (`json:"group,omitempty"`，空 = 顶层未分组，兼容现有)。
- `Register` 接收 group；持久化到 `~/.pactify/projects.json`；`GET /api/registry` DTO 带 `group`。

## 3. 后端（opencode）

### 3.1 目录浏览 endpoint（新）
`GET /api/fs/browse?path=<abs>`：
- 过 `requireSeat`(422 if 无 seat)。
- `path` 为空 → 用 serve 进程的 home 目录（`os.UserHomeDir`）。
- 返回 `{path, parent, entries:[{name, path, isGit, hasPact}]}`：**只列子目录**（跳过文件 + 隐藏目录如 `.git`/`node_modules`），`isGit`=含 `.git`，`hasPact`=含 `.pact`。
- `path` 非法/不可读 → 400。仅读不写。

### 3.2 group 入参
- `registerReq` 加 `Group string`；`handleRegistryAdd` 透传给 `registry.Register`。
- `setupApplyReq` 加 `Group string`；`handleSetupApply` init+wire 后，**自身把项目注册进 registry 带 group**（让向导一次调用即注册，不用前端再调 register）。

### 3.3 删除：复用 `DELETE /api/registry/{name}`（已有，仅取消注册）。

### 后端测试点
- fs browse：列子目录、标 isGit/hasPact、跳过文件/隐藏目录、无 seat→422、非法 path→400、空 path→home。
- register/setup-apply 带 group 落库；GET registry 返回 group。

## 4. 前端（kimi）

### 4.1 Sidebar 分组渲染（`shell/Sidebar.tsx`）
- 项目按 `group` 分组：有 group 的归到「组名」可展开节点下（参考截图 `Projects → 组名 → 文件夹`），无 group 的平铺。
- sidebar header 加「**+ 添加新项目**」按钮；每个项目/组行 hover 出「删除」按钮。

### 4.2 FolderPicker 组件（新）
- 调 `GET /api/fs/browse`：面包屑路径 + 「上一级」 + 子目录列表（每项 checkbox 多选 + 标注 `已是 pactify 项目`/`git repo`）。
- 逐级进入目录、跨目录多选。返回选中的绝对路径数组。

### 4.3 添加向导 modal（新组件，两步）
- **Step 1**：项目组名（可空=不分组）+ FolderPicker 多选文件夹。
- **Step 2**：对每个 `hasPact=false` 的文件夹 → 用 `GET /api/setup/suggest` 拉建议 roster，让用户编辑 agent+seat+角色；`hasPact=true` 的直接注册、Step2 跳过。提供「套用上一个文件夹的 roster」快捷。
- **提交**：逐文件夹分流——`hasPact` → `postRegister(path, {group})`；新 → `setupApply({path, project, seats, group})`（init+wire+注册一气呵成）。汇总成功/失败 + TOML docOnly 提示。

### 4.4 api.ts
加 `browseFs(path?)`、`postRegister(path,{name?,group?})`、`setupApply({...,group})`、复用 `deleteRegistry`。

### 前端测试点（vitest + e2e）
- FolderPicker 导航 + 多选 + hasPact 标注。
- 向导分流：已有项目走 register、新项目走 setup-apply。
- Sidebar 按 group 分组渲染 + 删除确认弹窗。
- 双绿门 vitest + e2e（画布工艺规约不破）。

## 5. 执行映射
| 任务 | 座席 | verify |
|---|---|---|
| t-projwiz-backend（fs browse + group 字段） | opencode-worker | `go test ./internal/serve/ ./internal/registry/` |
| t-projwiz-frontend（Sidebar 分组 + 向导 + FolderPicker + 删除） | kimi-worker | `cd web && npx tsc --noEmit && npx vitest run && npx playwright test` |

reviewer=claude。t-frontend deps t-backend。

## 6. 验收
1. 后端 fs browse + group 完成，`go test` 绿。
2. dashboard 里：点「添加新项目」→ 浏览选多文件夹（不手输）→ 新文件夹配 roster → 提交 → 项目出现在 sidebar（按组）。删除按钮取消注册。
3. 前端 vitest + e2e 双绿，tsc clean。
4. 全程不碰用户 `.pact`/代码（删除只取消注册）。

## 7. 非目标
协议层多文件夹（B 方案）；FolderPicker 的整机根浏览（从 home 起够用）；项目组的嵌套层级（单层分组）。
