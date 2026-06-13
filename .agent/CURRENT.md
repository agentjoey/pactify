# Current Status — Pactify

Version:        v0.3.0（✅ 已发布，首个 GitHub Release，2026-06-10）
Sprint:         003
Sprint Status:  🚧 进行中（… + Canvas P0 ✅（PR #20-#22）+ M3.4 relay ✅（pact self-host dogfood，feature shipped）；剩 M2.3、relay/orchestrate 推 origin）
Last Updated:   2026-06-12 by claude (fable-5 控制端)
Sprint File:    .agent/sprints/sprint-003.md

## Open Bugs（P0/P1 必须本 Sprint 修复）
🟢 无已知 P0/P1 bug。
ℹ️ M2.1 F1 已闭环：M2.2 拍板裸 `pactify` + 分发上 PATH（install.sh/go install），agent 码零改动。
ℹ️ M2.2 终审 deferred minors（M1–M6，见 sprint-003 T2）：plugin 版本同步、go-install version fallback、release 测试门、checkPath LookPath 化等。

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
**M1.2 Go CLI ✅ 完成**（2026-06-10，PR #2 merged）：`pactify` Go CLI = 协议 v1 第二实现，与 bash 可互换。
6 个包（event/projection/gitx/pact/paths + cmd/pactify，cobra，单 4.4M 静态二进制）+ 10 动词 + 两条铁律。
**STATE.yml 字节对齐 bash** → 跨工具 validate 直通；互操作测试证实 bash↔Go 同一 .pact/ 接力 + STATE 字节一致。
go test 6 包 + bats 76 全绿。final review 修了 validate 缺 STATE 报 drift；记录悬挂事件投影分歧（仅畸形 log）。
LICENSE = MIT。

**M1.3 进行中（拆 3 计划，brainstorm spec 一份：docs/superpowers/specs/2026-06-10-m1.3-serve-mcp-dashboard-design.md）：**
- **M1.3a serve 后端 ✅**（2026-06-10，PR #3 merged）：`internal/registry`（~/.pactify/projects.json + register/unregister/list）+ `internal/serve`（State→JSON DTO、HTTP `/api/projects` `/state`、SSE `/events`、fsnotify 多项目 watcher、`pactify serve`）。observe-only 只读 API，无前端。8 Go 包 + 77 bats。final review 修了 watcher offset 两 bug（截断/切分支 reset、partial line）。
- **M1.3b React dashboard ✅**（2026-06-10，PR #4 merged）：`web/` Vite8+React19+TS+Tailwind4（项目切换/座位 chips/kanban/evidence preview/SSE timeline，observe-only），构建产物入库 `internal/serve/dist`，`internal/serve/embed.go` go:embed 在 `/` 服务 SPA（index fallback，/api 优先）。11 vitest + 78 bats。final review 修了 2 个 SSE 生命周期问题（切项目 stale fetchState 竞态；live 徽章接 onopen/onerror）。
- **M1.3c pactify mcp ✅**（2026-06-10，PR #5 merged）：`internal/mcp`（官方 go-sdk v1.6.1）——8 动词 tools（引擎铁律以 IsError 呈现）、`pact://state`/`pact://log` resources、fsnotify→ResourceUpdated 通知（SDK 按 session 订阅范围投递）、`pactify mcp` stdio 命令 + e2e。final review 修了 join seat 必须取自 PACT_AGENT_ID（防记录与行为分座）。

## 🎉 Phase 1 / Pact-Base 完成（2026-06-10）
协议 v1（冻结）+ bash 参考实现 + Go CLI（字节级互操作）+ 多项目 SSE dashboard + stdio MCP server。
PR #1-#5 全 merged。基线：**79 bats + 11 vitest + go test（10 包）全绿**。
**下一步 = Phase 2（Pact-Base 分发）：M2.1 跨 agent 薄封装（opencode/Gemini/Codex + 真实端到端）→
M2.2 分发（GitHub releases/brew/curl|sh + CI/分支保护 EP-009）→ M2.3 Claude marketplace → M2.4 pactify.dev 官网。
开 Sprint 003 + 从 M2.1 brainstorm 起步。**
backlog：M6（join roles inert）、I5（task_status grep 脆弱）、F1 worktree 并发隔离、悬挂事件投影统一、serve minors（log fsnotify errors / failed watch-add / symlink dedup）。

## Next Sprint Candidates
- [ ] [EP-001] [HIGH] Phase 0：Claude skill 实现 pact 协议 + dogfood 验证消灭人肉中继
- [ ] [EP-101] [HIGH] M1.1 通讯约定冻结（log.jsonl 事件 schema + .pact/ JSON Schema）
- [ ] [EP-102] [HIGH] M1.2 CLI v1（Go）

## Version History（最近 5 版）
| Version | Date | Summary |
|---------|------|---------|
| v0.1.0 | 2026-06-09 | Repo 初始化 + roadmap 锁定（三产品/守 Team）+ 技术地基决策（Go/MCP/React）|
