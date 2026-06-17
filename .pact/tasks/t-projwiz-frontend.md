# t-projwiz-frontend：Sidebar 分组 + 添加向导 + FolderPicker + 删除（项目向导前端）

> 完整 TDD 要点见 `docs/superpowers/plans/2026-06-17-project-wizard.md` 的 **Task t-projwiz-frontend**；
> 设计见 `docs/superpowers/specs/2026-06-17-project-wizard-design.md`。先读它们 + 现有 `shell/Sidebar.tsx`。
> 依赖后端（fs browse + group + setup-apply 自注册）已 merged。

## 目标
左侧 sidebar 加「添加新项目」向导 + 删除项目 + 项目组分组：

1. **`api.ts`**：`browseFs(path?)→{path,parent,entries}`、`postRegister(path,{name?,group?})`、`setupApply({path,project,seats,group})`（复用 writeJSON）。
2. **`FolderPicker.tsx`**（新）：调 `browseFs`，面包屑 + 「上一级」+ 子目录列表（每项 checkbox 多选 + 标 `hasPact`→「已是项目」/`isGit`→「git」）；跨目录累积选中绝对路径数组。
3. **`AddProjectWizard.tsx`**（新，两步 modal）：Step1 = 组名 + FolderPicker；Step2 = 对 `hasPact=false` 文件夹用 `getSetupSuggest` 拉 roster 编辑（+「套用上一个 roster」快捷），`hasPact=true` 跳过；提交逐文件夹分流（已有→`postRegister`、新→`setupApply`）+ 汇总结果/docOnly 提示。
4. **`Sidebar.tsx`**：按 `group` 分组渲染（有组→可展开节点、无组→平铺）；header「+ 添加新项目」开 wizard；每项 hover 出删除按钮→确认弹窗→`deleteRegistry`。

## 文件
- 改 `web/src/components/shell/Sidebar.tsx`、`web/src/lib/api.ts`
- 建 `web/src/components/shell/AddProjectWizard.tsx` + `FolderPicker.tsx` + 各自 test

## 纪律
- **TDD**：先写失败 vitest→红→实现→绿。覆盖 FolderPicker 导航/多选/标注、wizard 分流、Sidebar 分组 + 删除确认；e2e 走「添加→选文件夹→提交→项目出现」。
- **画布工艺规约**（spec 2026-06-12 §5）不破。复用现有组件/样式模式。完成跑 verify 双绿再 checkpoint。

verify: cd web && npx tsc --noEmit && npx vitest run && npx playwright test
