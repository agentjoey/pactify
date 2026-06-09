# Product Backlog — Pactify
> 排入 Sprint 后从此处移除。Epic 按三产品分层（见 docs/ROADMAP.md）。

## 🔴 HIGH — Phase 0（Skill 流程验证，当前）
- [ ] [EP-001] [HIGH] Claude skill 实现 pact 协议（手写文件 + 状态转换）
- [ ] [EP-001] [HIGH] Dogfood：Pactify 自身用 skill 走完 6 段，验证消灭人肉中继

## 🔴 HIGH — Phase 1（Pact-Base：地基 + CLI + 监控）
- [ ] [EP-101] [HIGH] M1.1 通讯约定冻结：log.jsonl 事件 schema（MCP 语义对齐）
- [ ] [EP-101] [HIGH] M1.1 .pact/ 文件契约 JSON Schema + docs/specs/pact-protocol.md
- [ ] [EP-102] [HIGH] M1.2 CLI v1（Go）：init/join/status/checkpoint/accept/merge/log/validate
- [ ] [EP-102] [HIGH] M1.2 Claude skill 退化为薄封装（调 CLI）
- [ ] [EP-103] [HIGH] M1.3 pactify serve：MCP server + fsnotify→SSE→React 本地 dashboard

## 🟡 MED — Phase 2（Pact-Base：覆盖 + 分发）
- [ ] [EP-201] [MED] M2.1 跨 agent 薄封装（opencode/Gemini/Codex）+ 端到端测试
- [ ] [EP-202] [MED] M2.2 分发：GitHub releases + brew tap + curl|sh + go install
- [ ] [EP-203] [MED] M2.3 Claude marketplace 上架
- [ ] [EP-204] [MED] M2.4 pactify.dev 官网 v1（协议文档 + 快速开始）

## 🟢 LOW — Phase 3（Pact-Squad：可视化编排）
- [ ] [EP-301] [LOW] M3.1 任务编排画布（React Flow）
- [ ] [EP-302] [LOW] M3.2 可视化派发 + 实时追踪
- [ ] [EP-303] [LOW] M3.3 agent 通讯可视化（事件流实时图）
- [ ] [EP-304] [LOW] M3.4 Team 云同步 relay 接口预留 + Squad 付费 feature gate 设计

## 📋 研究向 / 远期（Phase 4-6，未决策细节）
- [ ] [EP-401] Pact-Team：云端事件 relay + 托管 Mission-control + RBAC（自建，不绑 Vercel）
- [ ] [EP-402] Pact-Team：推送式 daemon（建在 MCP/A2A 上）
- [ ] [EP-501] 增长：多市场入驻 + 企业版 + 生态 adapter
- [ ] [EP-601] 中国 Go-to-Market（云端迁出 Vercel + 本地化分发，独立 Phase）

## ✅ 已完成（按 Sprint 归档）
- ✅ Sprint 001: Repo 初始化 + roadmap 锁定（三产品 + 守 Team）+ 技术地基决策（Go/MCP/React）
