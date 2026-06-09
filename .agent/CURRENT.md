# Current Status — Pactify

Version:        v0.1.0
Sprint:         002
Sprint Status:  🔄 In Progress
Last Updated:   2026-06-09 by claude-opus-4-8
Sprint File:    .agent/sprints/sprint-002.md

## Open Bugs（P0/P1 必须本 Sprint 修复）
🟢 无已知 P0/P1 bug。

## Current Sprint Summary
Sprint 001：Repo 初始化 + roadmap/地基锁定完成。**Phase 0 实现完成**（分支 feat/phase0-pact-skill）：
`.pact/bin/pact.sh` 协议参考实现（11 动词 + 两条铁律 + 座位身份 + replay 恢复），**46/46 bats 测试绿**，
经 final review 修复 4 处不变式漏洞（C1 accept 前置态、C2 task-id 唯一、I3 merge 校验 feature、I4 seat 校验）。
**Phase 0 Exit Gate ✅ PASS + dogfood findings 全部处理**（2026-06-09）。dogfood：Claude 编排 +
opencode worker 真实 Go 任务跑通 6 段，人只说"开始"、rule1 实战拦住自接受。4 个 findings：
**F1（worker 分支/提交纪律）、F2（多行 evidence）、F3（pact_init 覆盖入口文件）已全部修复**，
F4（shell 持久化）入口文件已声明。F1 选最小自动化（join 自动切分支、checkpoint 自动提交、
merge 自动回 base），全流程 hands-off 端到端验证通过。测试 **55/55 绿**。
**Phase 0 已合入 main。**

**Phase 1 / M1.1 协议冻结 ✅ 完成**（2026-06-09，PR #1 merged）：协议冻结为 **v1**——
log 事件加 `event_id` + `init.protocol_version`，validate 版本门禁(高 major fail-closed)+ event_id 必查，
前向兼容(忽略未知)，PROJECT.md 受管区块，JSON Schema 工件(event/seat/task，含 payload-per-type 强校验)，
规范正文 `docs/specs/pact-protocol.md`。final review 修了 spec/code 一致性(join 不扩 roster)。**73/73 绿**。
**GitHub 已上线**：https://github.com/agentjoey/pactify (PUBLIC, default main)。git 流程见 CONTRIBUTING.md
(GitHub flow：feat/* 分支 → PR → merge-commit → 删支；维护者 docs/chore 可直推 main；CI+分支保护待 Phase 2)。
**下一步：M1.2 Go CLI v1**（`pactify` 顶替 pact.sh，命令契约 = docs/specs/pact-protocol.md §7；行为与 bash 参考一致）。
backlog：M6（join roles inert）、I5（task_status grep 脆弱）、F1 worktree 并发隔离、**仓库 LICENSE 未定（待 Joey）**。

## Next Sprint Candidates
- [ ] [EP-001] [HIGH] Phase 0：Claude skill 实现 pact 协议 + dogfood 验证消灭人肉中继
- [ ] [EP-101] [HIGH] M1.1 通讯约定冻结（log.jsonl 事件 schema + .pact/ JSON Schema）
- [ ] [EP-102] [HIGH] M1.2 CLI v1（Go）

## Version History（最近 5 版）
| Version | Date | Summary |
|---------|------|---------|
| v0.1.0 | 2026-06-09 | Repo 初始化 + roadmap 锁定（三产品/守 Team）+ 技术地基决策（Go/MCP/React）|
