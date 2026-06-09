# Sprint 002

Goal:      Phase 1 (Pact-Base) — 协议冻结 (M1.1) → Go CLI (M1.2) → pactify serve / MCP + dashboard (M1.3)
Period:    2026-06-09 ~ TBD
Version:   v0.2.0 (target)
Assignee:  claude (+ opencode via pact protocol where useful)

## Context
Phase 0 已合入 main：`.pact/bin/pact.sh` bash 参考实现 + 11 动词 + 两条铁律，55/55 测试，
真实跨 agent dogfood 通过。Phase 1 把经验 schema 形式化冻结，再用 Go CLI 顶替 bash，
最后加 `pactify serve`（MCP server + 本地 dashboard）。

## Tasks

### T1: M1.1 通讯约定 / 协议冻结 [HIGH] [claude]
**Status:** ✅ Done (PR #1 merged，73/73 测试，协议 v1 冻结)
**Milestone:** M1.1
**Acceptance:**
- [ ] log.jsonl 事件 schema 正式定义（含 event_type 枚举 + 版本字段 + 与 MCP 工具语义对齐）
- [ ] .pact/ 文件契约 JSON Schema（PROJECT.md 座位表 / STATE.yml / tasks）
- [ ] docs/specs/pact-protocol.md（他人可独立实现；含状态机、两条铁律、座位身份、恢复语义）
- [ ] 吸收 Phase 0 dogfood 经验（F1 分支模型、座位握手、replay 恢复）

### T2: M1.2 Go CLI v1 [HIGH] [claude/opencode]
**Status:** ✅ Done (PR #2 merged；6 Go 包 + 76 bats；bash↔Go 互操作 + STATE 字节一致)
**Milestone:** M1.2
**Acceptance:**
- [ ] `pactify init/join/status/checkpoint/accept/changes/merge/log/validate/help`（命令契约同 pact.sh）
- [ ] 单静态二进制；Go YAML/JSON 库；行为与 bash 参考实现一致（同测试场景）
- [ ] Claude skill 退化为薄封装（调 pactify CLI）

### T3: M1.3 pactify serve（MCP server + 本地 dashboard）[HIGH] [claude]
**Status:** 🔄 In Progress（拆 3 计划：**A serve 后端 ✅ PR #3 merged**；B React dashboard 待做；C pactify mcp 待做）
**Milestone:** M1.3
**Acceptance:**
- [ ] MCP server：事件订阅 + 工具暴露
- [ ] fsnotify watch log.jsonl → SSE/WS → Vite/React 本地 dashboard（go:embed）
- [ ] 展示 active agents / task 看板 / 事件时间线 / blocked

## Superpowers Checkpoints
| Skill | 触发条件 | 本 Sprint |
|-------|---------|---------|
| brainstorming | T1 协议冻结设计前 | 🔄 进行中 |
| writing-plans | 每个 milestone 实现前 | ❌ 待触发 |
| verification-before-completion | Task Done 前 | ❌ 待触发 |

## Sprint 回顾
（进行中）
