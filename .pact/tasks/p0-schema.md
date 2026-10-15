# Task p0-schema

**Feature:** p0-codex · **Owner:** codex-worker · **Reviewer:** claude-session · **Linear:** AJ-613

## worker 须知（必读）
- 只改「Files」列出的文件；需要改别的文件时，在 checkpoint evidence 里说明原因。
- 跑任何 Go 测试都带 `PACTIFY_HOME="$(mktemp -d)"`；不要运行会写入真实 `~/.pactify` 或 `~/.kimi-code` 的命令（真机探测步骤只读 `~/.kimi-code`）。
- 不要改 CLAUDE.md / AGENTS.md / GEMINI.md；不要提交临时探测程序或生成物。
- 严格按 TDD：先写测试并看它失败，再实现，再看它通过。
- 完成后用 pact checkpoint 提交，evidence 写：verify 命令的完整输出摘要 + 新增测试名列表 + 真机探测结果（若本任务有该步骤）。
- 设计以本文件的「Interfaces」为准；与仓库现状冲突时以本文件为准，并在 evidence 里写明冲突点。
- 背景：这是 pactify 的 harness 路由器 Phase 0（spec `docs/specs/harness-subagents.md`，完整计划 `docs/specs/harness-subagents-phase0-plan.md`；这两个文件可能还不在你的分支上，本文件已包含你需要的全部内容）。

## Global Constraints

- 只拉起用户本机已装、已登录的**官方** CLI；不读写其凭据，不做模型代理（spec P-1）。
- 请求的模型无法兑现时**拒绝执行并给出可选模型**，绝不静默降级（P-2）。
- 用户只面对 `--read-only` / `--write` 两档；映射：codex `sandbox: read-only` / `workspace-write` 且 `approvalPolicy: never`；kimi `session/set_mode plan` / 默认模式（P-3、§5.4）。
- 回到 Claude 的只有结果摘要；最终回复截断到 **4096 字节**；完整日志落盘给路径（P-4）。
- 转发 subagent 用 `model: haiku`、`tools: Bash`，只调一次 Bash（P-5）。
- 每个 job 记录 callee 的 CLI 版本（P-7）。
- **`AccessDefault`（空值）必须保持 serve 现有行为逐字节不变**：本计划对 `internal/cockpit` 的改动只在调用方显式传 `Access`、`Model`、`Effort`、`Env`、`Stderr` 时生效。
- 任何测试不得写开发者真实的 `~/.pactify` 或 `~/.kimi-code`：Go 测试一律用 `t.TempDir()` 作 `PACTIFY_HOME` 与 kimi home；验收命令统一以 `PACTIFY_HOME="$(mktemp -d)"` 运行。
- 被调用的 agent 不提交代码；改动留在工作区，由 host 决定（spec §5.3）。
- job 目录：`$PACTIFY_HOME/jobs/<job-id>/`（`PACTIFY_HOME` 未设时为 `~/.pactify`）。job id 格式 `YYYYMMDD-HHMMSS-<6 位小写十六进制>`。

### Task 1: 刷新 codex app-server schema（AJ-613）

**Files:**
- Modify: `internal/cockpit/codexschema/codex_app_server_protocol.schemas.json`（整文件重新生成）
- Modify: `internal/cockpit/codexschema/CODEX_VERSION`
- Test: `internal/cockpit/codexschema/schema_drift_test.go`、`schema_contract_test.go`（不改，只跑）

**Interfaces:**
- Consumes: 无
- Produces: 与本机 codex 版本一致的 schema；Task 2 依据它读 `ThreadStartResponse.model / modelProvider / reasoningEffort / sandbox / approvalPolicy` 与 `ItemCompletedNotification` 中 agentMessage 的 `text`。

- [ ] **Step 1: 确认当前漂移测试失败**

Run: `PACTIFY_HOME="$(mktemp -d)" go test ./internal/cockpit/codexschema/ -run TestSchemaDrift -v`
Expected: FAIL，输出含「definitions.v2 added」「To resolve」。

- [ ] **Step 2: 重新生成 schema**

```bash
tmp="$(mktemp -d)"
codex app-server generate-json-schema --out "$tmp"
cp "$tmp/codex_app_server_protocol.schemas.json" internal/cockpit/codexschema/
codex --version | awk '{print $NF}' > internal/cockpit/codexschema/CODEX_VERSION
```

- [ ] **Step 3: 复核 `internal/cockpit/codex.go` 与新 schema 的契合**

逐项核对并把结果写进 checkpoint evidence（表格：名称 / schema 定义 / 结论）：
- 客户端发出的请求：`initialize`、`initialized`、`thread/start`、`thread/resume`、`turn/start`、`turn/interrupt`。
- 监听的通知：`item/agentMessage/delta`、`item/completed`、`item/commandExecution/outputDelta`、`item/started`、`thread/tokenUsage/updated`、`turn/started`、`turn/completed`、`error`、`turn/diff/updated`、`turn/plan/updated`。
- 读取的 JSON 路径：`jsonStringAt` 与 `parseUsageFromParams` 的全部调用点。
- 审批回复结构：`item/commandExecution/requestApproval`、`item/fileChange/requestApproval`、`item/permissions/requestApproval`。
本任务**不改** `codex.go` 的行为；发现与 schema 不一致之处只记录，交给 Task 2。

- [ ] **Step 4: 跑通两个 schema 测试**

Run: `PACTIFY_HOME="$(mktemp -d)" go test ./internal/cockpit/codexschema/ -v`
Expected: `TestSchemaDrift`、`TestSchemaContract` PASS。

- [ ] **Step 5: checkpoint**

## 验收命令

verify: PACTIFY_HOME="$(mktemp -d)" go test ./internal/cockpit/codexschema/ && test "$(cat internal/cockpit/codexschema/CODEX_VERSION)" = "$(codex --version | awk '{print $NF}')"
