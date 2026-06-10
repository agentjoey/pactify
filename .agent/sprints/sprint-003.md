# Sprint 003

Goal:      Phase 2 (Pact-Base 覆盖+分发) — M2.1 跨 agent 薄封装（MCP 为主）→ M2.2 分发 → M2.3 marketplace → M2.4 官网
Period:    2026-06-10 ~ TBD
Version:   v0.3.0 (target)
Assignee:  claude (+ opencode via pact protocol)

## Context
Phase 1 / Pact-Base 完成（Sprint 002，PR #1–#5）：协议 v1 + bash 参考 + Go CLI（互操作）+
多项目 SSE dashboard + stdio MCP server。Phase 2 把"薄封装"为各 agent 做 drop-in 接入，
MCP 为主、shell 兜底，opencode 深做到端到端 dogfood。

设计/计划：`docs/superpowers/{specs,plans}/2026-06-10-m2.1-cross-agent-onboarding.{md}`

## Tasks

### T1: M2.1 跨 agent 薄封装（MCP 为主，app+CLI 双形态）[HIGH] [claude+opencode]
**Status:** ✅ Done（本地 merge 到 main，commit 990006b；exit gate ✅ PASS）
**Milestone:** M2.1
**实现**（8 个 TDD 任务，subagent 驱动 + 两阶段审查，全程绿）：
- 新包 `internal/agent`：`Adapter` 注册表，7 kind = vendor×surface，三正交轴
  （format=JSON 方言/TOML；scope=project/global；rooting=cwd/`--project`）。
- JSON merger 自动布线全部 JSON kind（含两个桌面 app：claude-desktop、antigravity）；
  codex（两面）TOML **doc-only**（不引 TOML 依赖）。幂等、非法 JSON 不覆盖、symlink 安全。
- 共享 "pact briefing"（MCP-first + shell 兜底）+ 导出 `pact.BakeManagedBlock`（拒 marker-in-body）。
- `pactify agent add <kind>`（--id/--roles/--project/--print；必填校验；写入确认，全局标 machine-global）。
- `pactify init` 第 4 段 seat 字段（kind）；前置校验 entry==kind 默认、拒桌面 kind（fail-closed）。
- `pactify mcp --project` chdir-root（桌面 app 锚定 repo；`filepath.Abs`）。
- 生成 `docs/agent-onboarding.md`，`TestDocFileInSync` 保证不漂移。
**验收门**：确定性 interop（已有 `TestFullLifecycleViaMCP`）+ stdio smoke（`tests/mcp.bats`）
+ `--project` smoke（`tests/mcp_project.bats`，从异 cwd 验锚定）。
**基线**：go 10 包 + bats 92 + vitest 11 全绿。go vet 干净。
**最终整体审查**修了 2 个 merge 前阻塞：I1 `--project` 相对路径未 abs（烤进全局配置）、
I2 桌面 kind 静默写全局文件无输出。
**CI workflow 整体留 M2.2**（本程测试只本地/dogfood 跑）。

#### Exit Gate — opencode 端到端 dogfood ✅ PASS（2026-06-10）
独立 scratch 仓 `~/AgentWorks/Code_Claude/pact-dogfood-m2`：`pactify init` 一步把 opencode 接进来
（`opencode.json` 挂 pact MCP server + `PACT_AGENT_ID=opencode`，`AGENTS.md` 烤 briefing）。
orchestrator(claude) 派 T1（写 `hello.sh`）→ 人只说一次"开始"→ opencode worker 经 pact MCP
工具自驱 join→实现→checkpoint → reviewer(claude) 独立验收 accept → merge，F1 **shipped**。
**消灭人肉中继成立**（除"开始"外无手动转述）。

**Findings：**
- **F1（PATH/分发，已临时修，待产品决策）**：agent MCP config 写的是裸 `pactify` 命令，靠 PATH 解析。
  pactify 只装在 `~/bin`（不在登录 PATH）时，opencode 起不了 MCP server 也用不了 CLI 兜底 → worker
  无法自 join，人肉中继被迫回归。**dogfood 临时修**：装到 `/opt/homebrew/bin`（上 PATH）后重启 opencode 即通。
  → **决策点**：M2.2 分发（brew/curl→PATH）本就解决此问题；或让 `pactify agent add` 把
  `os.Executable()` **绝对路径**写进 config 以摆脱 PATH 依赖（更稳，但绑定二进制位置）。带进 M2.2。
- **F2（init 工件随首个 feature 漂移，Minor）**：init 生成的入口/config 文件（AGENTS.md/CLAUDE.md/
  opencode.json）若在派活前未 commit，会被 worker 首个 checkpoint 的 `git add -A` 扫进 feature 提交，
  污染 feature diff。建议 `pactify init` 或工作流在开首个 feature 分支前先提交 scaffold 工件。低优。

### T2: M2.2 分发（GitHub releases + brew tap + curl|sh + go install）+ CI/分支保护 [HIGH]
**Status:** 🔲 Todo
**Milestone:** M2.2
**Context:** 吸收 M2.1 F1——分发的核心交付就是把 `pactify` 放上 agent 继承的 PATH。
**Acceptance:**
- [ ] GitHub Actions CI（go test + bats + vitest + build）+ 分支保护（EP-009）
- [ ] release 流水线（多平台静态二进制）
- [ ] brew tap + `curl|sh` 安装脚本 + `go install` 路径
- [ ] 决策 F1：config 用 PATH 裸命令 vs os.Executable() 绝对路径

### T3: M2.3 Claude marketplace 上架（skill 打包）[MED]
**Status:** 🔲 Todo
**Milestone:** M2.3

### T4: M2.4 pactify.dev 官网 v1（协议文档 + 5 分钟快速开始）[MED]
**Status:** 🔲 Todo
**Milestone:** M2.4
