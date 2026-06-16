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
**Version:**  v0.4.0（已发布）— orchestrate 自主驱动 + planner + 成本/可观测 + session 清理 + GLM 端点可配 + Settings agent 管理 + 浅色 dashboard + native audit layer + pactify.dev 文档站

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
