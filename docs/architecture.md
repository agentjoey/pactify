# Pactify — Architecture

> Last updated: 2026-06-09 | Status: Draft（Sprint 001，CLI 语言待定）

## Overview

Pactify 是一个**多 agent 协同协议 + 薄 CLI**。

```
用户 repo
  .pact/
    PROJECT.md      ← 章程（目标/技术栈/角色/约定）
    STATE.yml       ← 结构化活状态（log.jsonl 的投影）
    tasks/<id>.md   ← 单任务 spec+plan+验收项+交接日志
    log.jsonl       ← append-only 事件流（事实源）
  CLAUDE.md / AGENTS.md / GEMINI.md  ← 各厂商入口，均 → .pact/
  docs/specs|plans|decisions/        ← 知识库
```

## 核心设计决策

### log.jsonl 为源，STATE.yml 为投影

多 agent 并发写 STATE.yml → git 合并冲突；append-only 的 log.jsonl 天然 merge 友好。
- **写**：任何状态变更 → 追加 log event
- **读**：直接读 STATE.yml（快、结构化）
- **重算**：`pactify log` 从 log.jsonl replay → 重建 STATE.yml

### 拉取式派发

异构 agent 跨进程/设备/厂商，无法互相 ping → 派发 = worker 启动时读 STATE（pull），不是 orchestrator 主动 push。人是"启动按钮"。

### 角色与职责分离

| 角色 | 职责 |
|---|---|
| orchestrator | 拆 spec→tasks；派发；合并；维护章程 |
| worker | 实现；checkpoint → awaiting_review + 写证据 |
| reviewer | 验证 diff+证据 → accepted / changes_requested |
| human | 启动按钮 + 最终权威 |

worker 不能自标 `accepted`（职责分离）。

## CLI 命令集（规划）

```
pactify init           # 在目标 repo 生成 .pact/ 骨架
pactify join           # worker 冷启动；追加 join 事件
pactify status         # 打印 STATE.yml 现状
pactify checkpoint <id># task → awaiting_review + 追加 log
pactify accept <id>    # reviewer 用；task → accepted + 追加 log
pactify merge <feat>   # 全任务 accepted → --no-ff 合并 + feature → shipped
pactify log            # replay log.jsonl → 重建 STATE（投影验证）
```

## Open-core 分层

| 层 | 内容 | 状态 |
|---|---|---|
| 开源核心 | 协议文件契约 + CLI + 各厂商薄封装 | Sprint 001-002 |
| 产品/平台 | 持久状态服务 + 推送式 daemon + Mission-control UI | 产品阶段 |

## 技术选型（待定）

CLI 语言：Go（单静态二进制，agent shell 调用零依赖）vs Node（npm 生态）→ Sprint 001 T1 决策。
