# Product Backlog — Pactify
> 排入 Sprint 后从此处移除。

## 🔴 HIGH
- [ ] [EP-002] [HIGH] CLI 完整命令集（init/join/status/checkpoint/accept/merge/log）
- [ ] [EP-003] [HIGH] Dogfood：Pactify 自身用 .pact/ 管理开发（满血后再推给用户）
- [ ] [EP-004] [HIGH] STATE 校验器（`pactify validate`，防 schema 漂移）

## 🟡 MED
- [ ] [EP-005] [MED] Claude skill 薄封装（调同一 CLI）
- [ ] [EP-006] [MED] opencode / Gemini 入口薄封装
- [ ] [EP-007] [MED] git worktree 隔离方案（各 agent 独立工作树，替代锁机制）
- [ ] [EP-008] [MED] `pactify log --replay` 从 log.jsonl 重建 STATE.yml

## 🟢 LOW
- [ ] [EP-009] [LOW] GitHub Actions CI（validate .pact/ + 跑测试）
- [ ] [EP-010] [LOW] pactify.dev 官网（协议文档 + 快速开始）

## 📋 研究向（未决策）
- [ ] CLI 语言最终确认：Go（单静态二进制）vs Node（npm 分发）→ Sprint 001 T1 决策
- [ ] 推送式 daemon / watcher（产品阶段，Open-core 付费层）
- [ ] 持久状态服务 + Mission-control UI（Open-core 平台层）

## ✅ 已完成（按 Sprint 归档）
- ✅ Sprint 001: Repo 初始化 + 文件契约骨架
