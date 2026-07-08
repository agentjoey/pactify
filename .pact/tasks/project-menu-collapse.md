# Task project-menu-collapse (P2#16) — 项目下拉的 worktree 列表默认折叠

## 问题
ProjectMenu(web/src/components/shell/ProjectMenu.tsx)把每个项目的 git worktree 分支**内联全展开**
(实测 linx 项目 11 条分支把项目列表淹没)。

## 修法
- 每个有 >1 worktree 的项目行,右侧加一个小 chevron+计数(如 `▸ 11`,data-testid=`worktree-toggle-<name>`),
  **默认折叠**;点 chevron 切换展开(仅该项目);展开状态本地 state(不持久化)。
- **当前选中的 worktree 非主树时**,该项目自动展开(用户能看到自己在哪个树)。
- 点项目名行为不变(选主树);展开后的分支行为不变。
- 键盘可达:chevron 是 button 带 aria-label(`show worktrees for <name>`)+ aria-expanded。

## 改文件
web/src/components/shell/ProjectMenu.tsx + ProjectMenu.test.tsx(若无则新建)

## 测试
- >1 worktree 默认不渲染分支行;点 chevron 后渲染;再点收起。
- currentWorktree 命中某分支时该项目初始展开。
- 单 worktree 项目无 chevron。

## 验收 / Acceptance(视角: ux — 列表不再被分支淹没、当前树可见、键盘可达)
verify: cd web && npx tsc --noEmit && npx vitest run src/components/shell/ProjectMenu.test.tsx
