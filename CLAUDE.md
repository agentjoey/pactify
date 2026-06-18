# Pactify — Claude Code Context

## ⭐ Session 启动（每次必执行）
```bash
git pull
cat .agent/CURRENT.md
```

## Project Overview
多 agent 协同协议 + 薄 CLI。任何能读文件的 agent 均可参与；协议基于 git + repo，零外部依赖。  
协议核心：`.pact/` 文件契约；CLI 是协议的标准实现；各厂商入口是薄封装。
**Location:** ~/AgentWorks/Code_Claude/pactify
**GitHub:**   agentjoey/pactify
**Version:**  v0.7.0（Sprint2 端到端下发 + worktree-aware 项目）— **Dispatch Panel**：dashboard 从「只能看」→「一句话下发」，右侧滑出面板 goal→后台拉起 planner agent 生成任务图→审阅→确认 = apply+orchestrate run（后端 `POST/GET .../plan/generate[/status]` claude 写；前端 DispatchPanel kimi 经 pact orchestrate dogfood 写，A/B worktree 隔离并行）。**worktree-aware 项目**：`GET .../worktrees` + `state?wt=`，ProjectMenu 项目下缩进列出 git worktree、可切换查看各 worktree 的 board（非主轮询、主 SSE）。承接 v0.6.1（IA v2 收尾 + UI 修正）— RosterDock 重设计为精致身份列表（按角色分组的 logo+名+角色标签，两张悬浮卡居中偏上、Board-only 避开 Canvas/Office 工具栏）、项目下拉每行状态灯、Board 左 gutter 防遮挡、accepted 列显示最近 10 张折叠其余；#22 清理（删 Sidebar/PlanReview/TopBar 死代码、View/CableMark 迁出、RosterDock 齿轮定位座席 Settings）。承接 v0.6.0（IA v2 dashboard 信息架构重做）— header 项目下拉（状态灯+改名+projwiz）、悬浮 RosterDock（磨砂卡片，orchestrator 第一）、Settings modal（整合 ops+setup）、视图 7→3（Board/Canvas/Live，默认 Board，快捷键 1/2/3）、PlanDock 只读悬浮窗、Recipes 进 ⌘K、Board accepted 列折叠/分组/限渲染、删 ReplayBar+`?at=` 回放、删 Sidebar；后端 `PUT /api/registry/{name}` 改名 + SeatDTO 透出 kind + planner kebab-slug 命名校验/prompt；承接 v0.5.2：SSE 反代健壮性修复；v0.5.1：orchestrate per-task token 捕获；v0.5.0：custom-agent manifest API；v0.4.0：orchestrate 自主驱动 + planner + 浅色 dashboard + native audit layer + pactify.dev 文档站

**Technical docs:** [Architecture](docs/architecture.md) · [Deployment](docs/deployment.md) · [Operations](docs/operations.md)

## Tech Stack
| Layer | Tech |
|-------|------|
| CLI | TBD (Go 单静态二进制 / Node) |
| Protocol | Plain files (.pact/) |
| CI | GitHub Actions |

## Key Implementation Details
- `.pact/` = 产品协议文件（用户 repo 使用）；`.agent/` = 本 repo 开发工作台（不是产品一部分）
- `log.jsonl` 为源，`STATE.yml` 为投影，CLI 重算；防多 agent 并发写 STATE 冲突
- worker 不能自标 `accepted`，只能置 `awaiting_review`；只有 reviewer 能转 `accepted`
- 拉取式派发：worker 启动时读 STATE.yml，人是"启动按钮"
- 画布工艺规约（spec 2026-06-12 §5）：节点位置只有两个写入者（placeNew 首现一次 + 用户拖拽），禁止渲染时算位置；RF 节点数组只走 merge-by-id；生产代码禁止伪造 RF 几何（measured/handles）；画布 PR 合并门 = vitest + Playwright e2e 双绿

## Dev Commands
```bash
# CLI 语言未定，Sprint 001 T1 决策后补充
```

## Release 后必做（任何 agent 通用）
1. .agent/CURRENT.md：补充 Version History 描述
2. 更新 Current Sprint Summary
3. 如有架构变更：更新 docs/architecture.md

<!-- pact:begin (managed by pactify — edit outside this block) -->
# pact protocol — seat `claude`

This repo uses the **pact protocol** (v1). You are seat `claude`, roles: orchestrator,reviewer.

**Primary — MCP:** the `pact` MCP server is wired into your config. Use its tools
(status / join / assign / checkpoint / accept / changes / merge / list) and resources
(`pact://state`, `pact://log`). Cold start: call `status`, then `join`
(registers your seat and checks out your feature branch).

**Fallback — shell** (if MCP is unavailable):
```bash
export PACT_AGENT_ID=claude
pactify join claude --roles orchestrator,reviewer
```
then `pactify help` for the verbs.

**The two rules:** a worker cannot self-accept (only the task's reviewer accepts); a
feature cannot merge until all its tasks are accepted.
<!-- pact:end -->
