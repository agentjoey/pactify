# Sprint 001

Goal:      Repo 初始化 + 决策 CLI 语言/第一交付物 + 定义 .pact/ 最小文件契约 + CLI 骨架
Period:    2026-06-09 ~ 2026-06-15
Version:   v0.1.0
Assignee:  claude

## Tasks

### T1: 决策：CLI 语言 + 第一交付物 [HIGH] [claude]
**Status:** 🔲 Todo
**Epic:** EP-001
**Context:** 两个开放问题需拍板（§10 agentpact-design-2026-06-09.md）：
1. 第一个交付：先抽 CLI（可移植）还是先 skill（快验证）？建议 CLI first（跨厂商是核心价值主张）
2. CLI 语言：Go（单静态二进制，agent shell 调用零依赖）vs Node（npm 生态，更易分发）
**Acceptance:**
- [ ] Joey 确认语言选型
- [ ] Joey 确认先 CLI 还是先 skill
- [ ] 更新 CLAUDE.md Tech Stack + Dev Commands

### T2: 定义 .pact/ 最小文件契约 [HIGH] [claude]
**Status:** 🔲 Todo
**Epic:** EP-001
**Acceptance:**
- [ ] PROJECT.md schema（章程字段）完整定义
- [ ] STATE.yml schema（含状态机枚举值）完整定义，带注释说明
- [ ] log.jsonl event schema（event_type 枚举 + 必填字段）完整定义
- [ ] tasks/<id>.md 模板定义
- [ ] 文档落地：`docs/specs/pact-file-contract.md`

### T3: CLI 骨架（基于 T1 决策的语言） [HIGH] [claude]
**Status:** 🔲 Todo（依赖 T1）
**Epic:** EP-001
**Commands:** init / join / status / checkpoint / accept
**Acceptance:**
- [ ] `pactify init` → 在目标 repo 生成 .pact/ 骨架
- [ ] `pactify status` → 读 STATE.yml 打印当前状态
- [ ] `pactify checkpoint <task-id>` → task 状态 → awaiting_review + 追加 log.jsonl
- [ ] `pactify accept <task-id>` → reviewer 用；task → accepted + 追加 log.jsonl
- [ ] `pactify log` → 从 log.jsonl 重建并打印 STATE（投影验证）
- [ ] 单元测试覆盖核心状态机转换

### T4: Dogfood（可选，Sprint 末） [MED] [claude]
**Status:** 🔲 Todo（依赖 T3）
**Epic:** EP-003
**Acceptance:**
- [ ] Pactify 自身的 .pact/ 初始化（`pactify init`）
- [ ] Sprint 002 首个 task 通过 .pact/ 协议走一遍完整6段

## Superpowers Checkpoints
| Skill | 触发条件 | 本 Sprint |
|-------|---------|---------|
| brainstorming | T2 文件契约设计前 | ❌ 待触发 |
| verification-before-completion | Task Done 前 | ❌ 待触发 |
| systematic-debugging | 发现 Bug 时 | N/A |

## Sprint 回顾
**Done:** T0（Repo 初始化）｜ **Deferred:** —
