# Phase 0（harness subagents）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **本计划的实际执行方式（Owner 2026-10-15 指定）**：开发由 **codex 经 pactify 编排器**完成（座席 `codex-worker`），Claude 会话担任 orchestrator 与 reviewer（座席 `claude-session`）。下文「执行协议」一节定义这套流程；Task 1–7 的正文会被逐字拷进 `.pact/tasks/*.md` 交给 codex，Task 8 由 Claude 会话亲自执行。

**Goal:** 做出「在 Claude Code 里用 `/codex`、`/kimi` 把大任务委派给 codex / kimi，并拿回核实过模型与成本的结果摘要」的最小可用链路，再用 10 个真实大任务验证它值不值得做下去。

**Architecture:** 底层复用 `internal/cockpit` 的会话接口（codex 走 `codex app-server`，kimi 走 `kimi acp`），在其上新增 `internal/sub` 包与 `pactify sub` 命令：一个独立进程组的后台 worker 持有会话、写 job 状态，前台命令只等状态文件。Claude Code 插件只是薄壳：skill 转写任务、haiku 转发 subagent 调一次 Bash、主线程后台等待完成通知。

**Tech Stack:** Go 1.26（cobra、标准库 os/exec、encoding/json），JSON-RPC over stdio（codex app-server 协议、ACP），Claude Code 插件（skills / agents / hooks 的 Markdown 与 JSON）。

**Spec:** `docs/specs/harness-subagents.md`（D1–D7、§4 架构、§5 关键设计、§6 Phase 0、§10 实测结果、§11 兼容适配层）。Linear：项目 P-AJ-18，里程碑 M0，issue AJ-612 … AJ-618。

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

## Review Focus

以下五类输入 spec 隐含但单个任务的常规测试不会自然覆盖；每条都已作为测试加进对应任务。

1. **事件缓冲区满时的「回合结束」事件**：现有 `dispatchEvent` 在通道满时直接丢弃事件；若丢的是 `turn_completed`，`pactify sub` 会永远等下去。期望：回合结束事件永不丢弃。→ Task 2、Task 4 各一条测试。
2. **用户指定了 harness 不认识的模型名**（例如把 agy 的旧名字传给 kimi）：期望立即失败、列出可选模型、不启动任务。→ Task 4（kimi）、Task 2（codex 由服务端拒绝时如实报错）、Task 6（CLI 退出码非 0）。
3. **工作区原本就有未提交改动**：期望 diffstat 只算 callee 本次带来的改动，不把用户原有改动算进去。→ Task 5 测试。
4. **kimi 在 plan 模式下反复申请退出、反复提问**（实测 32 次工具调用）：期望超过上限即中断，结果摘要首行标明「工具调用超限」，且无文件改动。→ Task 6 测试（假后端模拟）。
5. **前台 `run` 进程被杀或超时返回后**：期望后台 worker 仍跑完，`wait` / `result` 能拿到完整结果。→ Task 6 测试。

---

## 执行协议（Claude 会话负责，codex 不需要读）

### 一、为什么这样搭

- **独立 clone**：主副本里有 Owner 未提交的改动；`cmd/pactify` 的测试与 pact 账本自动提交都可能写进所在仓库。全部工作放在独立 clone `~/AgentWorks/Code_Claude/pactify-p0`，主副本不动。
- **集成分支**：pact 编排器在任务全部验收后会把 feature 合并进「基线分支」。把基线设为 `p0-integration`，每个 feature 合并后由 Claude 开 `p0-integration → main` 的 PR、等 CI 绿再合，保持仓库的 PR + CI 惯例。
- **评审座席不绑 agent**：本机 `~/.pactify/roles.json` 把 `claude` 座席绑在 `codex-focused-reviewer`（codex-cli）上，用它当评审会拉起一个 codex 来评审。所以新建座席 `claude-session`（orchestrator + reviewer，不声明 kind、不绑角色）。编排器遇到「评审座席就是编排器自己且没有 kind」时会暂停并提示人工完成（`internal/orchestrate/loop.go` 约 500 行），这就是 Claude 会话的评审闸口。

### 二、预备步骤（Claude 会话执行，一次性）

- [ ] **P1 · AJ-612**：在主副本把 spec 定稿与本计划单独开 docs PR（只含 `docs/specs/harness-subagents.md`、`docs/specs/harness-subagents-phase0-plan.md`，以及三份 entry 文件「当前状况」里方向那一条的同步修改；**不得**带上 entry 文件末尾 Owner 未提交的 SHARED-ENGINEERING 块）。CI 绿后合入，Linear AJ-612 置 Done。
- [ ] **P2 · 建 clone 与集成分支**：
```bash
cd ~/AgentWorks/Code_Claude
git clone git@github.com:agentjoey/pactify.git pactify-p0
cd pactify-p0
git checkout -b p0-integration origin/main
git push -u origin p0-integration
```
- [ ] **P3 · 构建驱动用的 pactify**（用 clone 里的 main 版本，放到会话 scratchpad 的 bin，不覆盖用户 PATH 上的版本）：
```bash
go build -o "$SCRATCH/bin/pactify" ./cmd/pactify
export PATH="$SCRATCH/bin:$PATH"
pactify version
```
- [ ] **P4 · 座席与基线**（在 clone 根目录）：
```bash
PACT_AGENT_ID=claude pactify config base-branch p0-integration
PACT_AGENT_ID=claude-session pactify join claude-session --roles orchestrator,reviewer
PACT_AGENT_ID=codex-worker  pactify join codex-worker  --roles worker --kind codex-cli
pactify status | sed -n '1,20p'
```
  预期：名册里出现 `claude-session`（无 kind）与 `codex-worker`（kind codex-cli）；`base_branch` 为 `p0-integration`。随后 `git push origin p0-integration`（账本提交随集成分支走）。
- [ ] **P5 · codex 工作模型**（Owner 2026-10-15 指定 **GPT-6.1-Sol High**）：新建角色 `p0-codex-worker`（kind `codex-cli`，model `gpt-6.1-sol`，effort `high`）并把座席 `codex-worker` 绑上去。`pactify role set` 没有 effort 参数，effort 直接写进 `~/.pactify/roles.json` 的该 profile（`internal/roles.Profile.Effort`，显式 effort 优先于任务分级）；改前备份。驱动时**不传** `--seat-kind codex-worker=...`：显式 seat-kind 会压过角色绑定，导致模型与 effort 不生效。
- [ ] **P6 · 门禁命令**：不设项目级 gate，每个任务用自己的 `verify:` 行（见各任务末尾）。

### 三、每个 feature 的循环

本计划共 4 个 feature，按顺序执行：`p0-codex`（Task 1–2）→ `p0-kimi`（Task 3–4）→ `p0-sub`（Task 5–6）→ `p0-plugin`（Task 7）。

1. **写任务 spec**：把对应 Task 的正文（从「Files」到「验收命令」）拷进 `.pact/tasks/p0-<task>.md`，顶部加一段固定的「worker 须知」（见下），末尾保留 `verify:` 行。
2. **分派**（以 `p0-codex` 为例）：
```bash
export PACT_AGENT_ID=claude-session
pactify assign p0-schema --feature p0-codex --branch feat/p0-codex \
  --owner codex-worker --reviewer claude-session --spec .pact/tasks/p0-schema.md
pactify assign p0-codex-backend --feature p0-codex --branch feat/p0-codex \
  --owner codex-worker --reviewer claude-session --spec .pact/tasks/p0-codex-backend.md \
  --deps p0-schema
git push origin p0-integration
```
3. **驱动**：
```bash
PACT_AGENT_ID=claude-session pactify orchestrate --feature p0-codex --as claude-session \
  --run-timeout 60 --idle-timeout 10 --no-register
```
  编排器默认在隔离 worktree 里拉起 codex；codex 完成后自行 checkpoint，任务进入 `awaiting_review`，编排器因评审座席无 kind 而暂停。
4. **评审**（Claude 会话）：读 feature 分支相对集成分支的 diff 与 checkpoint evidence，按下方「评审清单」逐条核对，自己在 clone 里跑一遍 `verify:` 命令。
   - 通过：`pactify accept <task> --evidence "<自己跑出的验收输出摘要>"`
   - 不通过：`pactify changes <task> --reason "<具体到文件和行为的修改要求>"`，然后重跑第 3 步。
5. **合并与 PR**：feature 内全部任务 accepted 后再跑第 3 步，编排器执行 verify 硬门并把 feature 合进 `p0-integration`。随后：
```bash
git push origin p0-integration
gh pr create --base main --head p0-integration --title "phase0: <feature 摘要>" --body "<含 Linear issue 编号与验收证据>"
```
  CI 绿后合入 main，Linear 对应 issue 置 Done；再 `git pull` 让集成分支跟上 main。
6. **升级处理**：编排器因返工超限或硬门失败而暂停时，读 `.pact/orchestrate/` 下的记录；能靠更清楚的修改要求解决的就 `changes`，属于计划本身有误的就先改本计划与任务 spec，再 `--resume`。

**worker 须知**（每个任务 spec 顶部固定附上）：
```
- 只改「Files」列出的文件；需要改别的文件时，在 checkpoint evidence 里说明原因。
- 跑任何 Go 测试都带 PACTIFY_HOME="$(mktemp -d)"；不要运行 `go test ./...` 以外会触碰真实 home 的命令。
- 不要提交 `.pact/` 以外的生成物；不要改 CLAUDE.md / AGENTS.md / GEMINI.md。
- 完成后用 pact checkpoint 提交，evidence 写：verify 命令的完整输出摘要 + 新增测试名列表。
- 不确定的设计点按本 spec 的「Interfaces」执行；与 spec 冲突时以 spec 为准并在 evidence 里写明。
```

**评审清单**（Claude 会话每次评审逐条核对）：
- `verify:` 命令自己重跑一遍为绿；新增测试确实覆盖了「Steps」里列出的每个用例，且测试先失败后通过的顺序在提交历史或 evidence 里可见。
- `AccessDefault` 路径（serve）零行为变化：`internal/serve` 与 `internal/cockpit` 原有测试全绿，`-race` 绿。
- 只动了「Files」里的文件；没有顺手重构、没有新增依赖。
- 错误信息给出用户能照着做的下一步（例如可选模型列表），没有吞错。
- Review Focus 里归属本任务的那一条测试存在且断言正确。

---

## 文件结构

| 文件 | 职责 | 任务 |
|---|---|---|
| `internal/cockpit/codexschema/*` | 随附的 codex app-server 协议 schema 与版本号 | Task 1 |
| `internal/cockpit/backend.go` | `StartOpts` 新字段、`Access`、`SessionInfo`、`InfoSession`、`OptsResumer` | Task 2 |
| `internal/cockpit/codex.go` | codex 会话：设模型 / 权限档 / effort、读实际模型、最终回复、stderr、回合结束事件不丢 | Task 2 |
| `internal/acp/acp.go` | ACP 客户端：`NewSessionDetailed`、`SetConfigOption`、`SetMode`、`Cancel`、`FindConfig` | Task 3 |
| `internal/cockpit/acp.go` | ACP 会话：应用模型 / effort / 只读模式、`Info()`、回合结束事件、`Interrupt` 走 `session/cancel` | Task 4 |
| `internal/cockpit/kimirecord.go` | 读 kimi 本地会话记录，得到实际模型与逐轮 token | Task 4 |
| `internal/orchestrate/env.go` | 导出 `CrossVendorStrip` | Task 5 |
| `internal/sub/job.go` | job id、job 目录、状态读写 | Task 5 |
| `internal/sub/collect.go` | 从事件流收集最终回复、token、工具调用数、回合结束 | Task 5 |
| `internal/sub/diff.go` | 工作区快照与 diffstat（排除用户原有改动） | Task 5 |
| `internal/sub/envelope.go` | 结果摘要的结构、Markdown 渲染、JSON | Task 5 |
| `internal/sub/harness.go` | harness 注册表（codex、kimi）：kind、后端、版本探测、实际模型核实 | Task 6 |
| `internal/sub/worker.go` | 后台 worker：起会话、审批策略、工具上限、空闲看门狗、写结果 | Task 6 |
| `cmd/pactify/cmd_sub.go` | `pactify sub run / wait / status / result / cancel` 与隐藏的 `__worker` | Task 6 |
| `plugins/pactify/**` | Claude Code 插件：skills、agents、hooks、plugin.json | Task 7 |
| `.claude-plugin/marketplace.json` | 登记新插件 | Task 7 |
| `docs/phase0-report.md` | Phase 0 报告 | Task 8 |

---

## Feature `p0-codex`（AJ-613、AJ-614）

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

**验收命令**
```
verify: PACTIFY_HOME="$(mktemp -d)" go test ./internal/cockpit/codexschema/ && test "$(cat internal/cockpit/codexschema/CODEX_VERSION)" = "$(codex --version | awk '{print $NF}')"
```

### Task 2: codex 后端补齐委派能力（AJ-614）

**Files:**
- Modify: `internal/cockpit/backend.go`（新增类型，`StartOpts` 加字段）
- Modify: `internal/cockpit/codex.go`
- Test: `internal/cockpit/codex_test.go`（新增用例，沿用其中的 `fakeCodexServer`）

**Interfaces:**
- Consumes: Task 1 的 schema。
- Produces（后续任务依赖，名称与类型不得改动）：

```go
// backend.go 新增
type Access string

const (
	AccessDefault  Access = ""          // 后端原有行为：serve 用这个，必须零变化
	AccessReadOnly Access = "read-only" // 只读：codex sandbox read-only；kimi plan 模式
	AccessWrite    Access = "write"     // 可写：codex sandbox workspace-write；kimi 默认模式
)

type StartOpts struct {
	RepoDir      string    // agent 的 cwd = 仓库根
	Seat         string    // PACT_AGENT_ID
	Model        string    // 可空；非空时必须兑现，否则 Start 返回错误
	SystemPrompt string    // 可空
	Effort       string    // 可空；codex 每轮 effort，kimi 的 thinking 选项
	Access       Access    // 见上
	Env          []string  // 追加在继承环境之后（同名键以此为准）
	Stderr       io.Writer // nil → os.Stderr
}

// SessionInfo 是会话自报的实际运行参数；字段为空表示后端没有回报。
type SessionInfo struct {
	Model    string
	Provider string
	Effort   string
	Access   string // 后端回报的权限档原文，例如 codex 的 "readOnly" / "workspaceWrite"
}

// InfoSession 由能回报实际运行参数的会话实现。
type InfoSession interface{ Info() SessionInfo }

// OptsResumer 由能带参数续接的后端实现；Backend.Resume 等价于 ResumeWith(ctx, id, StartOpts{})。
type OptsResumer interface {
	ResumeWith(ctx context.Context, threadID string, opts StartOpts) (Session, error)
}

// 回合结束事件：Event{Kind: EventState, State: StateTurnCompleted 或 StateTurnFailed}
const (
	StateTurnCompleted = "turn_completed"
	StateTurnFailed    = "turn_failed"
)
```

  codex 最终回复事件：`Event{Kind: EventMessage, Final: true, Text: <item.text>}`。额度事件：`Event{Kind: EventSystem, State: "rate_limits", Raw: <原始参数>}`。

- [ ] **Step 1: 写失败测试——thread/start 带上模型与权限档，并读出实际模型**

```go
func TestCodexThreadStartCarriesOptsAndReportsActualModel(t *testing.T) {
	s := newFakeCodexServer(t)
	var got map[string]any
	s.handle("thread/start", func(m wireMessage) any {
		_ = json.Unmarshal(m.Params, &got)
		return map[string]any{
			"thread":          map[string]any{"id": "th-1"},
			"model":           "gpt-6-luna",
			"modelProvider":   "openai",
			"reasoningEffort": "high",
			"sandbox":         map[string]any{"type": "readOnly", "networkAccess": false},
			"approvalPolicy":  "never",
		}
	})
	info, id, err := s.client.threadStartOpts(context.Background(), StartOpts{
		RepoDir: "/repo", Model: "gpt-6-luna", Access: AccessReadOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "th-1" {
		t.Fatalf("thread id = %q", id)
	}
	if got["model"] != "gpt-6-luna" || got["sandbox"] != "read-only" || got["approvalPolicy"] != "never" || got["cwd"] != "/repo" {
		t.Fatalf("thread/start params = %v", got)
	}
	want := SessionInfo{Model: "gpt-6-luna", Provider: "openai", Effort: "high", Access: "readOnly"}
	if info != want {
		t.Fatalf("info = %+v, want %+v", info, want)
	}
}

func TestCodexThreadStartDefaultAccessSendsOnlyCwd(t *testing.T) {
	s := newFakeCodexServer(t)
	var got map[string]any
	s.handle("thread/start", func(m wireMessage) any {
		_ = json.Unmarshal(m.Params, &got)
		return map[string]any{"thread": map[string]any{"id": "th-2"}}
	})
	if _, _, err := s.client.threadStartOpts(context.Background(), StartOpts{RepoDir: "/repo"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["cwd"] != "/repo" {
		t.Fatalf("serve path must send only cwd, got %v", got)
	}
}

func TestCodexWriteAccessMapsToWorkspaceWrite(t *testing.T) {
	s := newFakeCodexServer(t)
	var got map[string]any
	s.handle("thread/start", func(m wireMessage) any {
		_ = json.Unmarshal(m.Params, &got)
		return map[string]any{"thread": map[string]any{"id": "th-3"}}
	})
	if _, _, err := s.client.threadStartOpts(context.Background(), StartOpts{RepoDir: "/r", Access: AccessWrite}); err != nil {
		t.Fatal(err)
	}
	if got["sandbox"] != "workspace-write" || got["approvalPolicy"] != "never" {
		t.Fatalf("params = %v", got)
	}
}
```

（若 `wireMessage` 没有 `Params json.RawMessage` 字段，先给它加上；它是测试辅助结构。）

- [ ] **Step 2: 运行，确认失败**

Run: `PACTIFY_HOME="$(mktemp -d)" go test ./internal/cockpit/ -run 'TestCodexThreadStart|TestCodexWriteAccess' -v`
Expected: 编译失败，`threadStartOpts` / `AccessReadOnly` 未定义。

- [ ] **Step 3: 实现**

在 `backend.go` 加入 Interfaces 中的类型。在 `codex.go`：

```go
func sandboxMode(a Access) string {
	switch a {
	case AccessReadOnly:
		return "read-only"
	case AccessWrite:
		return "workspace-write"
	}
	return ""
}

func codexStartParams(opts StartOpts) map[string]any {
	p := map[string]any{"cwd": opts.RepoDir}
	if opts.Model != "" {
		p["model"] = opts.Model
	}
	if m := sandboxMode(opts.Access); m != "" {
		p["sandbox"] = m
		p["approvalPolicy"] = "never"
	}
	return p
}

type codexThreadResult struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	Model           string `json:"model"`
	ModelProvider   string `json:"modelProvider"`
	ReasoningEffort string `json:"reasoningEffort"`
	Sandbox         struct {
		Type string `json:"type"`
	} `json:"sandbox"`
}

func (r codexThreadResult) info() SessionInfo {
	return SessionInfo{Model: r.Model, Provider: r.ModelProvider, Effort: r.ReasoningEffort, Access: r.Sandbox.Type}
}

func (c *codexClient) threadStartOpts(ctx context.Context, opts StartOpts) (SessionInfo, string, error) {
	raw, err := c.rpc.call(ctx, "thread/start", codexStartParams(opts))
	if err != nil {
		return SessionInfo{}, "", err
	}
	var res codexThreadResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return SessionInfo{}, "", fmt.Errorf("parse thread/start result: %w", err)
	}
	return res.info(), res.Thread.ID, nil
}
```

  原 `threadStart(ctx, cwd)` 改为调用 `threadStartOpts(ctx, StartOpts{RepoDir: cwd})` 的薄包装，保证 serve 路径不变。`thread/resume` 同理新增 `threadResumeOpts(ctx, threadID string, opts StartOpts)`：参数为 `codexStartParams(opts)` 再加 `"threadId": threadID`，返回值解析同一个 `codexThreadResult`。

- [ ] **Step 4: 运行，确认通过**

Run: 同 Step 2。Expected: PASS。

- [ ] **Step 5: 写失败测试——effort、最终回复、额度事件、回合结束事件不丢**

```go
func TestCodexTurnStartCarriesEffort(t *testing.T) {
	s := newFakeCodexServer(t)
	var got map[string]any
	s.handle("turn/start", func(m wireMessage) any {
		_ = json.Unmarshal(m.Params, &got)
		return map[string]any{"turn": map[string]any{"id": "tu-1"}}
	})
	if err := s.client.turnStartEffort(context.Background(), "th-1", "do it", "low"); err != nil {
		t.Fatal(err)
	}
	if got["effort"] != "low" {
		t.Fatalf("turn/start params = %v", got)
	}
}

func TestCodexItemCompletedCarriesFinalText(t *testing.T) {
	c, evs := newCodexRecordingClient()
	c.handleNotification("item/completed", json.RawMessage(`{"item":{"type":"agentMessage","id":"m1","text":"WROTE"}}`))
	if len(*evs) != 1 || (*evs)[0].Kind != EventMessage || !(*evs)[0].Final || (*evs)[0].Text != "WROTE" {
		t.Fatalf("events = %s", dumpEvents(*evs))
	}
}

func TestCodexRateLimitsSurfaceAsSystemEvent(t *testing.T) {
	c, evs := newCodexRecordingClient()
	c.handleNotification("account/rateLimits/updated", json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":12}}}`))
	if len(*evs) != 1 || (*evs)[0].Kind != EventSystem || (*evs)[0].State != "rate_limits" {
		t.Fatalf("events = %s", dumpEvents(*evs))
	}
}

// Review Focus 1：缓冲区满时回合结束事件不能丢。
func TestCodexTurnEndNeverDropped(t *testing.T) {
	s := &codexSession{events: make(chan Event, 1), approvals: make(chan ApprovalRequest, 1), done: make(chan struct{})}
	s.events <- Event{Kind: EventMessage, Text: "filler"} // 缓冲区已满
	go s.dispatchEvent(Event{Kind: EventState, State: StateTurnCompleted})
	<-s.events // 消费者取走 filler
	select {
	case e := <-s.events:
		if e.State != StateTurnCompleted {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn_completed was dropped")
	}
}
```

- [ ] **Step 6: 运行，确认失败**

Run: `PACTIFY_HOME="$(mktemp -d)" go test ./internal/cockpit/ -run 'TestCodexTurnStartCarriesEffort|TestCodexItemCompleted|TestCodexRateLimits|TestCodexTurnEndNeverDropped' -v`
Expected: FAIL（`turnStartEffort` 未定义、Final 事件无正文、无 rate_limits 事件、done 字段不存在或事件被丢）。

- [ ] **Step 7: 实现**

- `turnStartEffort(ctx, threadID, text, effort string) error`：在原 `turnStart` 的参数上，`effort != ""` 时加 `"effort": effort`；原 `turnStart` 改为 `turnStartEffort(ctx, id, text, "")`。`codexSession` 新增字段 `effort string`，`Prompt` 用它调用 `turnStartEffort`。
- `item/completed` 的 agentMessage 分支：解析 `item.text`，发 `Event{Kind: EventMessage, Final: true, Text: text, Raw: raw}`（`codexThreadItem` 加 `Text string \`json:"text"\``）。
- `handleNotification` 新增 `case "account/rateLimits/updated":` 发 `Event{Kind: EventSystem, State: "rate_limits", Raw: raw}`。
- `codexSession` 加 `done chan struct{}`（`Close` 时关闭）；`dispatchEvent` 改为：

```go
func (s *codexSession) dispatchEvent(e Event) {
	if e.Kind == EventState && (e.State == StateTurnCompleted || e.State == StateTurnFailed) {
		select { // 回合结束事件：阻塞直到被取走或会话关闭，绝不丢弃
		case s.events <- e:
		case <-s.done:
		}
		return
	}
	select { // 其余事件维持原行为：满了就丢
	case s.events <- e:
	default:
	}
}
```

  `Close` 中先 `close(s.done)` 再关闭 events 通道（与现有 closeOnce 合并），避免向已关闭通道发送。
- `newCodexSession` 改签名为 `newCodexSession(ctx context.Context, opts StartOpts, resumeThreadID string)`：`cmd.Env = append(filteredEnviron(), opts.Env...)`；`cmd.Stderr` 取 `opts.Stderr`，为 nil 时 `os.Stderr`；新会话走 `threadStartOpts`，续接走 `threadResumeOpts`；把返回的 `SessionInfo` 存在会话上；`effort = opts.Effort`。
- `codexSession` 实现 `Info() SessionInfo`。
- `codexBackend` 实现 `ResumeWith(ctx, threadID, opts)`；`Start` 传 `opts`，`Resume` 等价于 `ResumeWith(ctx, id, StartOpts{})`。

- [ ] **Step 8: 运行 cockpit 与 serve 全部测试**

Run: `PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/cockpit/... ./internal/serve/...`
Expected: PASS（含既有测试，证明 serve 路径未变）。

- [ ] **Step 9: 真机探测（evidence 必附）**

在一个临时 git 仓库里执行（`$TMPREPO` 用 `mktemp -d` 后 `git init` 并提交一个文件）：用新增的测试辅助或一个临时 `main` 小程序分别以 `AccessReadOnly` 与 `AccessWrite`、`Model: "gpt-6-luna"` 启动会话，提示「Create a file named probe.txt containing hi. Then reply with one line: WROTE or BLOCKED.」。Expected：只读档最终回复为 `BLOCKED` 且文件不存在；可写档为 `WROTE` 且文件存在；两次 `Info().Model == "gpt-6-luna"`。临时小程序不要提交。

- [ ] **Step 10: checkpoint**

**验收命令**
```
verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/cockpit/... ./internal/serve/... && go build ./...
```

---

## Feature `p0-kimi`（AJ-615）

### Task 3: ACP 客户端补齐会话配置、模式与取消（AJ-615 前半）

**Files:**
- Modify: `internal/acp/acp.go`
- Test: `internal/acp/acp_test.go`（扩展其中的 `fakeServer`）

**Interfaces:**
- Produces：

```go
type ConfigValue struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

type ConfigOption struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Category     string        `json:"category"` // "model" | "thought_level" | "mode" | …
	Type         string        `json:"type"`     // "select"
	CurrentValue string        `json:"currentValue"`
	Options      []ConfigValue `json:"options"`
}

type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type NewSessionResult struct {
	SessionID     SessionID
	ConfigOptions []ConfigOption
	Modes         []SessionMode
	CurrentModeID string
}

func (c *Client) NewSessionDetailed(ctx context.Context, cwd string) (NewSessionResult, error)
func (c *Client) SetConfigOption(ctx context.Context, sid SessionID, configID, value string) ([]ConfigOption, error)
func (c *Client) SetMode(ctx context.Context, sid SessionID, modeID string) error
func (c *Client) Cancel(sid SessionID) error // 发 session/cancel 通知（无 id）

// FindConfig 先按 ID 精确匹配，再按 Category 匹配。
func FindConfig(opts []ConfigOption, idOrCategory string) (ConfigOption, bool)
```

  `NewSession(ctx, cwd)` 保留，改为调用 `NewSessionDetailed` 并只返回 `SessionID`。

- [ ] **Step 1: 扩展测试用的 fakeServer**

给 `fakeServer` 加 `onMethod map[string]func(fs *fakeServer, params json.RawMessage) any`（请求按方法名查表，查到就用返回值作 result）与 `notified []string`（记录收到的通知方法名）。`session/new` 的默认返回保持 `{"sessionId":"sess-1"}` 以免影响现有测试。

- [ ] **Step 2: 写失败测试**

```go
const kimiNewSession = `{"sessionId":"sess-1",
 "configOptions":[
  {"type":"select","id":"model","name":"Model","category":"model","currentValue":"kimi-code/k3-256k",
   "options":[{"value":"kimi-code/kimi-for-coding","name":"K2.8 Preview"},{"value":"kimi-code/k3-256k","name":"K3-256k"}]},
  {"type":"select","id":"thinking","name":"Thinking","category":"thought_level","currentValue":"high",
   "options":[{"value":"low","name":"Thinking Low"},{"value":"high","name":"Thinking High"}]}],
 "modes":{"currentModeId":"default","availableModes":[{"id":"default","name":"Default"},{"id":"plan","name":"Plan"}]}}`

func TestNewSessionDetailedParsesConfigAndModes(t *testing.T) {
	fs := &fakeServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
		"session/new": func(*fakeServer, json.RawMessage) any { return json.RawMessage(kimiNewSession) },
	}}
	c := newTestClient(t, fs)
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	res, err := c.NewSessionDetailed(ctx(t), ".")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := FindConfig(res.ConfigOptions, "model")
	if !ok || m.CurrentValue != "kimi-code/k3-256k" || len(m.Options) != 2 {
		t.Fatalf("model option = %+v", m)
	}
	if th, ok := FindConfig(res.ConfigOptions, "thought_level"); !ok || th.ID != "thinking" {
		t.Fatalf("thought_level lookup by category failed: %+v", th)
	}
	if res.CurrentModeID != "default" || len(res.Modes) != 2 || res.Modes[1].ID != "plan" {
		t.Fatalf("modes = %+v current=%q", res.Modes, res.CurrentModeID)
	}
}

func TestSetConfigOptionSendsIdsAndReturnsOptions(t *testing.T) {
	var got map[string]any
	fs := &fakeServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
		"session/set_config_option": func(_ *fakeServer, p json.RawMessage) any {
			_ = json.Unmarshal(p, &got)
			return map[string]any{"configOptions": []any{map[string]any{"id": "model", "category": "model", "currentValue": "kimi-code/kimi-for-coding"}}}
		},
	}}
	c := newTestClient(t, fs)
	opts, err := c.SetConfigOption(ctx(t), "sess-1", "model", "kimi-code/kimi-for-coding")
	if err != nil {
		t.Fatal(err)
	}
	if got["sessionId"] != "sess-1" || got["configId"] != "model" || got["value"] != "kimi-code/kimi-for-coding" {
		t.Fatalf("params = %v", got)
	}
	if len(opts) != 1 || opts[0].CurrentValue != "kimi-code/kimi-for-coding" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestSetModeAndCancel(t *testing.T) {
	var got map[string]any
	fs := &fakeServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
		"session/set_mode": func(_ *fakeServer, p json.RawMessage) any { _ = json.Unmarshal(p, &got); return map[string]any{} },
	}}
	c := newTestClient(t, fs)
	if err := c.SetMode(ctx(t), "sess-1", "plan"); err != nil {
		t.Fatal(err)
	}
	if got["sessionId"] != "sess-1" || got["modeId"] != "plan" {
		t.Fatalf("set_mode params = %v", got)
	}
	if err := c.Cancel("sess-1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return fs.sawNotification("session/cancel") })
}
```

  （`waitFor` 为轮询辅助：每 10ms 检查一次，2 秒超时即 `t.Fatal`；`sawNotification` 在 fakeServer 上加锁读取 `notified`。两者一并写在测试文件里。）

- [ ] **Step 3: 运行，确认失败**

Run: `PACTIFY_HOME="$(mktemp -d)" go test ./internal/acp/ -run 'TestNewSessionDetailed|TestSetConfigOption|TestSetModeAndCancel' -v`
Expected: 编译失败（新方法未定义）。

- [ ] **Step 4: 实现**

```go
func (c *Client) NewSessionDetailed(ctx context.Context, cwd string) (NewSessionResult, error) {
	if cwd != "" {
		c.cwd = cwd
	}
	raw, err := c.call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return NewSessionResult{}, err
	}
	var wire struct {
		SessionID     SessionID      `json:"sessionId"`
		ConfigOptions []ConfigOption `json:"configOptions"`
		Modes         *struct {
			CurrentModeID  string        `json:"currentModeId"`
			AvailableModes []SessionMode `json:"availableModes"`
		} `json:"modes"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return NewSessionResult{}, fmt.Errorf("acp: session/new result: %w", err)
	}
	res := NewSessionResult{SessionID: wire.SessionID, ConfigOptions: wire.ConfigOptions}
	if wire.Modes != nil {
		res.Modes, res.CurrentModeID = wire.Modes.AvailableModes, wire.Modes.CurrentModeID
	}
	return res, nil
}

func (c *Client) SetConfigOption(ctx context.Context, sid SessionID, configID, value string) ([]ConfigOption, error) {
	raw, err := c.call(ctx, "session/set_config_option", map[string]any{"sessionId": sid, "configId": configID, "value": value})
	if err != nil {
		return nil, err
	}
	var wire struct {
		ConfigOptions []ConfigOption `json:"configOptions"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("acp: session/set_config_option result: %w", err)
	}
	return wire.ConfigOptions, nil
}

func (c *Client) SetMode(ctx context.Context, sid SessionID, modeID string) error {
	_, err := c.call(ctx, "session/set_mode", map[string]any{"sessionId": sid, "modeId": modeID})
	return err
}

func FindConfig(opts []ConfigOption, idOrCategory string) (ConfigOption, bool) {
	for _, o := range opts {
		if o.ID == idOrCategory {
			return o, true
		}
	}
	for _, o := range opts {
		if o.Category == idOrCategory {
			return o, true
		}
	}
	return ConfigOption{}, false
}
```

  `Cancel` 用客户端已有的写通知路径发 `{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":sid}}`（无 `id`）；若现有代码没有通知写入函数，参照 `replyPermission` 的写法新增私有 `notify(method string, params any) error`。

- [ ] **Step 5: 运行 acp 全部测试**

Run: `PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/acp/...`
Expected: PASS。

- [ ] **Step 6: checkpoint**

**验收命令**
```
verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/acp/... && go build ./...
```

### Task 4: kimi 会话：设模型 / 只读模式 / 回合结束 / 会话记录核实（AJ-615 后半）

**Files:**
- Modify: `internal/cockpit/acp.go`
- Create: `internal/cockpit/kimirecord.go`
- Test: `internal/cockpit/acp_test.go`、Create `internal/cockpit/kimirecord_test.go`

**Interfaces:**
- Consumes: Task 2 的 `StartOpts` / `Access` / `SessionInfo` / `InfoSession` / `OptsResumer` / `StateTurnCompleted` / `StateTurnFailed`；Task 3 的 `NewSessionDetailed` / `SetConfigOption` / `SetMode` / `Cancel` / `FindConfig`。
- Produces：

```go
// acp.go
var ErrModelUnavailable = errors.New("requested model is not offered by this agent")
var ErrModelUnsupported = errors.New("agent exposes no model option over ACP")

// applyACPOpts 在新会话上兑现模型、推理强度与只读档，返回会话自报信息。
func applyACPOpts(ctx context.Context, c acpConfigurer, sid acp.SessionID, res acp.NewSessionResult, opts StartOpts) (SessionInfo, error)

type acpConfigurer interface {
	SetConfigOption(ctx context.Context, sid acp.SessionID, configID, value string) ([]acp.ConfigOption, error)
	SetMode(ctx context.Context, sid acp.SessionID, modeID string) error
}

// kimirecord.go
type KimiRecord struct {
	Models       []string // 按出现顺序去重后的 llm.request.model（带 provider 前缀的 modelAlias）
	InputTokens  int      // Σ usage.inputOther + usage.inputCacheRead + usage.inputCacheCreation
	OutputTokens int      // Σ usage.output
	CacheRead    int      // Σ usage.inputCacheRead
	Turns        int      // usageScope=="turn" 的 usage.record 条数
}

// ReadKimiRecord 读 <kimiHome>/session_index.jsonl 找到 sessionDir，
// 再读 <sessionDir>/agents/main/wire.jsonl。kimiHome 一般为 ~/.kimi-code。
func ReadKimiRecord(kimiHome, sessionID string) (KimiRecord, error)
```

- [ ] **Step 1: 写失败测试——模型、推理强度、只读模式的兑现**

```go
type fakeConfigurer struct {
	set   map[string]string
	modes []string
}

func (f *fakeConfigurer) SetConfigOption(_ context.Context, _ acp.SessionID, id, v string) ([]acp.ConfigOption, error) {
	if f.set == nil {
		f.set = map[string]string{}
	}
	f.set[id] = v
	return []acp.ConfigOption{{ID: id, CurrentValue: v}}, nil
}
func (f *fakeConfigurer) SetMode(_ context.Context, _ acp.SessionID, m string) error {
	f.modes = append(f.modes, m)
	return nil
}

func kimiLikeSession() acp.NewSessionResult {
	return acp.NewSessionResult{
		SessionID: "s1",
		ConfigOptions: []acp.ConfigOption{
			{ID: "model", Category: "model", CurrentValue: "kimi-code/k3-256k",
				Options: []acp.ConfigValue{{Value: "kimi-code/kimi-for-coding"}, {Value: "kimi-code/k3-256k"}}},
			{ID: "thinking", Category: "thought_level", CurrentValue: "high",
				Options: []acp.ConfigValue{{Value: "low"}, {Value: "high"}}},
		},
		Modes:         []acp.SessionMode{{ID: "default"}, {ID: "plan"}},
		CurrentModeID: "default",
	}
}

func TestApplyACPOptsModelEffortReadOnly(t *testing.T) {
	f := &fakeConfigurer{}
	info, err := applyACPOpts(context.Background(), f, "s1", kimiLikeSession(), StartOpts{
		Model: "kimi-code/kimi-for-coding", Effort: "low", Access: AccessReadOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.set["model"] != "kimi-code/kimi-for-coding" || f.set["thinking"] != "low" {
		t.Fatalf("set = %v", f.set)
	}
	if len(f.modes) != 1 || f.modes[0] != "plan" {
		t.Fatalf("modes = %v", f.modes)
	}
	want := SessionInfo{Model: "kimi-code/kimi-for-coding", Effort: "low", Access: "plan"}
	if info != want {
		t.Fatalf("info = %+v", info)
	}
}

// Review Focus 2：不认识的模型名必须失败并列出可选项。
func TestApplyACPOptsUnknownModelFailsWithChoices(t *testing.T) {
	_, err := applyACPOpts(context.Background(), &fakeConfigurer{}, "s1", kimiLikeSession(), StartOpts{Model: "gemini-3.7-flash-low"})
	if !errors.Is(err, ErrModelUnavailable) || !strings.Contains(err.Error(), "kimi-code/kimi-for-coding") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyACPOptsNoModelOptionFails(t *testing.T) {
	res := kimiLikeSession()
	res.ConfigOptions = nil
	_, err := applyACPOpts(context.Background(), &fakeConfigurer{}, "s1", res, StartOpts{Model: "x"})
	if !errors.Is(err, ErrModelUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyACPOptsDefaultAccessTouchesNothing(t *testing.T) {
	f := &fakeConfigurer{}
	info, err := applyACPOpts(context.Background(), f, "s1", kimiLikeSession(), StartOpts{})
	if err != nil || len(f.set) != 0 || len(f.modes) != 0 {
		t.Fatalf("err=%v set=%v modes=%v", err, f.set, f.modes)
	}
	if info.Model != "kimi-code/k3-256k" || info.Access != "default" {
		t.Fatalf("info = %+v", info)
	}
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `PACTIFY_HOME="$(mktemp -d)" go test ./internal/cockpit/ -run TestApplyACPOpts -v`
Expected: 编译失败（`applyACPOpts` 未定义）。

- [ ] **Step 3: 实现 `applyACPOpts`**

```go
func applyACPOpts(ctx context.Context, c acpConfigurer, sid acp.SessionID, res acp.NewSessionResult, opts StartOpts) (SessionInfo, error) {
	info := SessionInfo{Access: res.CurrentModeID}
	if m, ok := acp.FindConfig(res.ConfigOptions, "model"); ok {
		info.Model = m.CurrentValue
	}
	if e, ok := acp.FindConfig(res.ConfigOptions, "thought_level"); ok {
		info.Effort = e.CurrentValue
	}
	if opts.Model != "" {
		m, ok := acp.FindConfig(res.ConfigOptions, "model")
		if !ok {
			return info, fmt.Errorf("%w (asked for %q)", ErrModelUnsupported, opts.Model)
		}
		if !offers(m, opts.Model) {
			return info, fmt.Errorf("%w: %q; available: %s", ErrModelUnavailable, opts.Model, strings.Join(values(m), ", "))
		}
		if _, err := c.SetConfigOption(ctx, sid, m.ID, opts.Model); err != nil {
			return info, fmt.Errorf("set model: %w", err)
		}
		info.Model = opts.Model
	}
	if opts.Effort != "" {
		e, ok := acp.FindConfig(res.ConfigOptions, "thought_level")
		if !ok || !offers(e, opts.Effort) {
			return info, fmt.Errorf("effort %q is not offered by this agent", opts.Effort)
		}
		if _, err := c.SetConfigOption(ctx, sid, e.ID, opts.Effort); err != nil {
			return info, fmt.Errorf("set effort: %w", err)
		}
		info.Effort = opts.Effort
	}
	if opts.Access == AccessReadOnly && hasMode(res.Modes, "plan") {
		if err := c.SetMode(ctx, sid, "plan"); err != nil {
			return info, fmt.Errorf("set plan mode: %w", err)
		}
		info.Access = "plan"
	}
	return info, nil
}
```

  `offers`、`values`、`hasMode` 为三个不导出的小函数（遍历 Options / Modes），写在同一文件。没有 plan 模式的 agent 在只读档下不改模式，只读由 `internal/sub` 的「拒绝所有权限请求」策略保证。

- [ ] **Step 4: 运行，确认通过**

Run: 同 Step 2。Expected: PASS。

- [ ] **Step 5: 写失败测试——回合结束事件、取消、会话信息**

```go
// Review Focus 1（ACP 侧）：Prompt 返回后发出回合结束事件，且缓冲区满时不丢。
func TestACPPromptEmitsTurnEndEvenWhenBufferFull(t *testing.T) {
	s := newACPSessionForTest(func(ctx context.Context, text string) (acp.StopReason, error) { return "end_turn", nil })
	for i := 0; i < cap(s.events); i++ {
		s.events <- Event{Kind: EventMessage}
	}
	done := make(chan error, 1)
	go func() { done <- s.Prompt(context.Background(), UserMessage{Text: "x"}) }()
	for i := 0; i < cap(s.events); i++ {
		<-s.events
	}
	select {
	case e := <-s.events:
		if e.Kind != EventState || e.State != StateTurnCompleted {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn_completed dropped")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestACPPromptErrorEmitsTurnFailed(t *testing.T) {
	s := newACPSessionForTest(func(ctx context.Context, text string) (acp.StopReason, error) { return "", errors.New("boom") })
	go func() { _ = s.Prompt(context.Background(), UserMessage{Text: "x"}) }()
	e := <-s.events
	if e.State != StateTurnFailed || !strings.Contains(e.Err, "boom") {
		t.Fatalf("got %+v", e)
	}
}
```

  `newACPSessionForTest(prompt func(context.Context, string) (acp.StopReason, error)) *acpSession` 是测试种子：`acpSession` 新增字段 `promptFn` 与 `cancelFn func() error`，生产代码在 `newACPSession` 里把它们设为 `client.Prompt(ctx, sid, text)` 与 `client.Cancel(sid)`。

- [ ] **Step 6: 运行，确认失败**

Run: `PACTIFY_HOME="$(mktemp -d)" go test ./internal/cockpit/ -run 'TestACPPrompt' -v`
Expected: FAIL / 编译失败。

- [ ] **Step 7: 实现**

- `acpSession.Prompt`：调用 `promptFn`；成功则发 `Event{Kind: EventState, State: StateTurnCompleted, Text: string(stop)}`，失败发 `Event{Kind: EventState, State: StateTurnFailed, Err: err.Error()}`，两者都走「不丢弃」路径（与 Task 2 相同的 `dispatchEvent` 规则，`done` 通道已存在）。
- `acpSession.Interrupt`：调用 `cancelFn`（即 `session/cancel`），不再是空操作。
- `acpSession` 存 `info SessionInfo` 并实现 `Info()`。
- `acpBackend.Start(ctx, opts)`：`acp.Spawn(ctx, cmd, args, opts.Env, opts.RepoDir)`；`NewSessionDetailed`；`applyACPOpts`，出错则关闭客户端并返回该错误（保留 `errors.Is` 链）。
- `acpBackend.ResumeWith(ctx, threadID, opts)`：`LoadSession` 之后无法拿到 configOptions（ACP 的 load 不回报），所以若 `opts.Model`、`opts.Effort` 或 `opts.Access` 非空，按配置 ID `model`、`thinking` 与模式 `plan` 直接调用设置方法（失败即返回错误）；`Resume` 等价于 `ResumeWith(ctx, id, StartOpts{})`。
- `opts.Stderr`：`acp.Spawn` 目前把子进程 stderr 接到哪里，就保持；若它接到 `os.Stderr`，给 `Spawn` 加一个 `stderr io.Writer` 可选参数（nil 保持原行为）。

- [ ] **Step 8: 写失败测试——kimi 会话记录**

```go
func TestReadKimiRecord(t *testing.T) {
	home := t.TempDir()
	sessDir := filepath.Join(home, "sessions", "wd_x", "session_abc")
	if err := os.MkdirAll(filepath.Join(sessDir, "agents", "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `{"sessionId":"session_other","sessionDir":"/nope","workDir":"/w"}
{"sessionId":"session_abc","sessionDir":"` + sessDir + `","workDir":"/w"}
`
	wire := `{"type":"config.update","modelAlias":"kimi-code/kimi-for-coding"}
{"type":"llm.request","provider":"openai","model":"kimi-for-coding","modelAlias":"kimi-code/kimi-for-coding"}
{"type":"usage.record","model":"kimi-code/kimi-for-coding","usage":{"inputOther":100,"output":20,"inputCacheRead":1000,"inputCacheCreation":0},"usageScope":"turn"}
{"type":"context.append_loop_event","event":{"type":"step.end","usage":{"inputOther":100,"output":20,"inputCacheRead":1000,"inputCacheCreation":0}}}
{"type":"llm.request","provider":"openai","model":"kimi-for-coding","modelAlias":"kimi-code/kimi-for-coding"}
{"type":"usage.record","model":"kimi-code/kimi-for-coding","usage":{"inputOther":50,"output":5,"inputCacheRead":0,"inputCacheCreation":10},"usageScope":"turn"}
`
	mustWrite(t, filepath.Join(home, "session_index.jsonl"), index)
	mustWrite(t, filepath.Join(sessDir, "agents", "main", "wire.jsonl"), wire)

	r, err := ReadKimiRecord(home, "session_abc")
	if err != nil {
		t.Fatal(err)
	}
	want := KimiRecord{Models: []string{"kimi-code/kimi-for-coding"}, InputTokens: 1160, OutputTokens: 25, CacheRead: 1000, Turns: 2}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("record = %+v, want %+v", r, want)
	}
}

func TestReadKimiRecordUnknownSession(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "session_index.jsonl"), `{"sessionId":"a","sessionDir":"/x"}`+"\n")
	if _, err := ReadKimiRecord(home, "zzz"); err == nil {
		t.Fatal("want error for unknown session")
	}
}
```

  （`mustWrite` 为测试辅助：`os.WriteFile` 失败即 `t.Fatal`。注意 `step.end` 里的用量是 `usage.record` 的重复，**不计入**；只统计 `usageScope == "turn"` 的 `usage.record`。模型取 `llm.request` 的 `modelAlias`，为空时取 `model`。）

- [ ] **Step 9: 实现 `ReadKimiRecord`**，逐行 `json.Unmarshal` 到一个只含 `type`、`model`、`modelAlias`、`usage`、`usageScope`、`sessionId`、`sessionDir` 的结构；索引里找不到时返回 `fmt.Errorf("kimi session %q not found in %s", ...)`。

- [ ] **Step 10: 运行全部测试**

Run: `PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/cockpit/... ./internal/acp/... ./internal/serve/...`
Expected: PASS。

- [ ] **Step 11: 真机探测（evidence 必附）**

在临时 git 仓库里，用临时小程序以 `kimi-cli` 后端分别执行：(a) `Model: "kimi-code/kimi-for-coding"`、`AccessWrite`，提示新建文件；(b) `AccessReadOnly`，同一提示。Expected：(a) 文件存在、`ReadKimiRecord(~/.kimi-code, sessionID).Models == ["kimi-code/kimi-for-coding"]`；(b) 文件不存在、`Info().Access == "plan"`。(b) 若出现反复申请退出 plan 的情况，记录工具调用次数（上限由 Task 6 的 worker 实施，本任务只观察并写进 evidence）。临时小程序不要提交。

  **同时核实一个计划假设**：ACP `session/new` 返回的 `sessionId` 是否就是 `~/.kimi-code/session_index.jsonl` 里的 `sessionId`（形如 `session_<uuid>`）。若相同，Task 6 直接用 `ThreadID()` 查记录；若不同，给 `ReadKimiRecord` 增加按 `workDir` 加「创建时间不早于会话开始时间」匹配最新一条的兜底，并在 evidence 里写明实测结果与采用的方式。

- [ ] **Step 12: checkpoint**

**验收命令**
```
verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/cockpit/... ./internal/acp/... ./internal/serve/... && go build ./...
```

---

## Feature `p0-sub`（AJ-616）

### Task 5: `internal/sub` 基础件——job、收集器、diff、结果摘要

**Files:**
- Modify: `internal/orchestrate/env.go`（导出 `CrossVendorStrip`）
- Create: `internal/sub/job.go`、`internal/sub/collect.go`、`internal/sub/diff.go`、`internal/sub/envelope.go`
- Test: `internal/sub/job_test.go`、`collect_test.go`、`diff_test.go`、`envelope_test.go`

**Interfaces:**
- Consumes: `cockpit.Event`、`cockpit.EventMessage`、`cockpit.EventTool`、`cockpit.EventUsage`、`cockpit.EventState`、`cockpit.EventSystem`、`cockpit.StateTurnCompleted`、`cockpit.StateTurnFailed`、`cockpit.SessionInfo`。
- Produces：

```go
// orchestrate/env.go
func CrossVendorStrip(kind string) []string { return crossVendorStrip(kind) }

// sub/job.go
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

type Request struct {
	Harness string `json:"harness"` // "codex" | "kimi"
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	Access  string `json:"access"` // "read-only" | "write"
	Cwd     string `json:"cwd"`
	Resume  string `json:"resume,omitempty"` // 续接的会话 id；空 = 新会话
	Brief   string `json:"brief"`
}

type State struct {
	ID         string    `json:"id"`
	Status     Status    `json:"status"`
	Request    Request   `json:"request"`
	PID        int       `json:"pid,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type Store struct{ Root string } // Root = <PACTIFY_HOME>/jobs

func DefaultStore() (Store, error)                        // PACTIFY_HOME 优先，否则 ~/.pactify
func NewJobID(now time.Time, rnd io.Reader) string         // "20261015-091500-a1b2c3"
func (s Store) Create(id string, req Request) (State, error)
func (s Store) Load(id string) (State, error)
func (s Store) Save(st State) error                        // 原子写：临时文件 + rename
func (s Store) Dir(id string) string
func (s Store) LogPath(id string) string                   // <dir>/log.jsonl
func (s Store) ResultPath(id string) string                // <dir>/result.json
func (s Store) LastSessionFor(harness, cwd string) (string, error) // 同仓库同 harness 最近一次成功 job 的会话 id

// sub/collect.go
type Collector struct {
	finalText string
	chunks    strings.Builder
	Usage     cockpit.Usage
	ToolCalls int
	TurnDone  bool
	TurnErr   string
	RateLimit json.RawMessage
}

func (c *Collector) Add(e cockpit.Event)
func (c *Collector) FinalText() string // codex 的 Final 正文优先；否则为最后一次工具调用之后累加的消息块

// sub/diff.go
type Snapshot struct {
	Base      string   // `git stash create` 的提交；工作区干净时为 HEAD
	Untracked []string // 开始时已存在的未跟踪文件
}

func TakeSnapshot(dir string) (Snapshot, error)
func DiffSince(dir string, snap Snapshot) (Diff, error)

type Diff struct {
	Files   []FileDelta
	Added   int
	Deleted int
}
type FileDelta struct {
	Path    string
	Added   int
	Deleted int
	New     bool // callee 新建的未跟踪文件
}

// sub/envelope.go
type Result struct {
	JobID          string    `json:"jobId"`
	Status         Status    `json:"status"`
	Harness        string    `json:"harness"`
	CLIVersion     string    `json:"cliVersion"`
	ModelRequested string    `json:"modelRequested,omitempty"`
	ModelActual    string    `json:"modelActual,omitempty"`
	ModelVerified  bool      `json:"modelVerified"` // 实际模型来自 harness 的独立证据
	Access         string    `json:"access"`
	Duration       string    `json:"duration"`
	InputTokens    int       `json:"inputTokens"`
	OutputTokens   int       `json:"outputTokens"`
	CostUSD        float64   `json:"costUSD,omitempty"`
	ToolCalls      int       `json:"toolCalls"`
	Diff           Diff      `json:"diff"`
	FinalText      string    `json:"finalText"` // 截断到 4096 字节
	Truncated      bool      `json:"truncated"`
	SessionID      string    `json:"sessionId"`
	LogPath        string    `json:"logPath"`
	Warnings       []string  `json:"warnings,omitempty"` // 例如「工具调用超限」「实际模型与请求不一致」
}

const FinalTextLimit = 4096

func TruncateFinal(s string) (string, bool) // 按 UTF-8 字符边界截断
func (r Result) Markdown() string
```

- [ ] **Step 1: 写失败测试（四个文件各一组）**

```go
// job_test.go
func TestJobIDFormat(t *testing.T) {
	id := NewJobID(time.Date(2026, 10, 15, 9, 15, 0, 0, time.UTC), bytes.NewReader([]byte{0xa1, 0xb2, 0xc3}))
	if id != "20261015-091500-a1b2c3" {
		t.Fatalf("id = %q", id)
	}
}

func TestStoreRoundTripAndLastSession(t *testing.T) {
	s := Store{Root: t.TempDir()}
	st, err := s.Create("j1", Request{Harness: "codex", Cwd: "/repo", Access: "write", Brief: "b"})
	if err != nil || st.Status != StatusQueued {
		t.Fatalf("create: %v %+v", err, st)
	}
	st.Status = StatusSucceeded
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ResultPath("j1"), []byte(`{"sessionId":"th-9","status":"succeeded"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.LastSessionFor("codex", "/repo")
	if err != nil || got != "th-9" {
		t.Fatalf("last session = %q, %v", got, err)
	}
	if got, _ := s.LastSessionFor("kimi", "/repo"); got != "" {
		t.Fatalf("other harness must not match, got %q", got)
	}
}

// collect_test.go
func TestCollectorPrefersCodexFinal(t *testing.T) {
	var c Collector
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "WRO"})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "TE"})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "WROTE"})
	c.Add(cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted})
	if c.FinalText() != "WROTE" || !c.TurnDone {
		t.Fatalf("final=%q done=%v", c.FinalText(), c.TurnDone)
	}
}

func TestCollectorACPChunksAfterLastTool(t *testing.T) {
	var c Collector
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "thinking out loud"})
	c.Add(cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "Write"}})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "WRO"})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "TE"})
	if c.FinalText() != "WROTE" || c.ToolCalls != 1 {
		t.Fatalf("final=%q tools=%d", c.FinalText(), c.ToolCalls)
	}
}

func TestCollectorUsageKeepsLatestTotals(t *testing.T) {
	var c Collector
	c.Add(cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 10, OutputTokens: 1}})
	c.Add(cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 40, OutputTokens: 7, CostUSD: 0.01}})
	if c.Usage.InputTokens != 40 || c.Usage.OutputTokens != 7 || c.Usage.CostUSD != 0.01 {
		t.Fatalf("usage = %+v", c.Usage)
	}
}

// diff_test.go —— Review Focus 3：用户原有改动不计入
func TestDiffSinceExcludesPreexistingChanges(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "t@t.t")
	git(t, dir, "config", "user.name", "t")
	write(t, dir, "a.txt", "one\n")
	write(t, dir, "b.txt", "one\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "base")
	write(t, dir, "a.txt", "one\nuser edit\n") // 用户原有、未提交的改动
	write(t, dir, "notes.tmp", "user untracked\n")

	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "b.txt", "one\ncallee edit\ncallee edit 2\n") // callee 改动
	write(t, dir, "new.go", "package x\n")                     // callee 新建

	d, err := DiffSince(dir, snap)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]FileDelta{}
	for _, f := range d.Files {
		paths[f.Path] = f
	}
	if _, ok := paths["a.txt"]; ok {
		t.Fatalf("pre-existing change counted: %+v", d)
	}
	if _, ok := paths["notes.tmp"]; ok {
		t.Fatalf("pre-existing untracked file counted: %+v", d)
	}
	if paths["b.txt"].Added != 2 || !paths["new.go"].New || d.Added != 3 {
		t.Fatalf("diff = %+v", d)
	}
	if out := gitOut(t, dir, "stash", "list"); out != "" {
		t.Fatalf("snapshot must not touch the stash list, got %q", out)
	}
}

// envelope_test.go
func TestTruncateFinalRespectsUTF8(t *testing.T) {
	s := strings.Repeat("好", 2000) // 6000 字节
	got, cut := TruncateFinal(s)
	if !cut || len(got) > FinalTextLimit || !utf8.ValidString(got) {
		t.Fatalf("len=%d cut=%v valid=%v", len(got), cut, utf8.ValidString(got))
	}
}

func TestMarkdownFlagsModelMismatchFirst(t *testing.T) {
	r := Result{Status: StatusSucceeded, Harness: "kimi", ModelRequested: "kimi-code/kimi-for-coding", ModelActual: "kimi-code/k3-256k",
		Warnings: []string{"实际模型与请求不一致"}, FinalText: "done", LogPath: "/tmp/l"}
	md := r.Markdown()
	first := strings.SplitN(md, "\n", 2)[0]
	if !strings.Contains(first, "实际模型与请求不一致") {
		t.Fatalf("first line must carry the warning, got %q", first)
	}
	for _, want := range []string{"kimi", "kimi-code/k3-256k", "done", "/tmp/l"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}
```

  （`git`、`gitOut`、`write` 为 diff_test.go 内的测试辅助：在 dir 里执行 git 命令失败即 `t.Fatal`，`gitOut` 返回去掉首尾空白的输出；`write` 写文件。）

- [ ] **Step 2: 运行，确认失败**

Run: `PACTIFY_HOME="$(mktemp -d)" go test ./internal/sub/ -v`
Expected: 编译失败（包不存在）。

- [ ] **Step 3: 实现**

- `TakeSnapshot`：`git stash create`（不改 stash 列表、不动工作区；输出为空表示工作区干净，此时 Base 取 `git rev-parse HEAD`）；`Untracked` 取 `git ls-files --others --exclude-standard`。
- `DiffSince`：`git diff --numstat <Base>`（与工作区比较）解析成 `FileDelta`；再取当前 `git ls-files --others --exclude-standard`，不在 `snap.Untracked` 里的视为 callee 新建，`New=true`，`Added` 为其行数（读文件按 `\n` 计数；二进制文件计 0）。路径排序后输出。
- `Collector.Add`：EventMessage 且 `Final && Text != ""` → `finalText = Text`；EventMessage 非 Final → 追加到 `chunks`；EventTool 且 `Phase == "start"` → `ToolCalls++` 并清空 `chunks`；EventUsage → 用新值覆盖（各 harness 发的是累计值）；EventState 为回合结束 → `TurnDone = true`，失败时 `TurnErr = e.Err`；EventSystem 且 `State == "rate_limits"` → 存 `Raw`。
- `Result.Markdown()`：首行为 `⚠️ <Warnings 用「；」连接>`（有警告时）或 `✓ <harness> 完成`；其后依次为：harness 与 CLI 版本、模型（请求 → 实际，`ModelVerified` 为假时注明「未经独立核实」）、权限档、耗时、token（与成本）、工具调用数、改动（文件数、+/-，最多列 20 个文件）、最终回复（代码块包裹）、会话 id、日志路径。
- `DefaultStore`：`os.Getenv("PACTIFY_HOME")` 非空用它，否则 `os.UserHomeDir()` + `.pactify`；`Root` = 其下 `jobs`。
- `LastSessionFor`：遍历 `Root` 下各 job 的 `state.json`，挑 `Request.Harness == harness`、`Request.Cwd == cwd`、`Status == succeeded` 中 `FinishedAt` 最新者，读其 `result.json` 的 `sessionId`。

- [ ] **Step 4: 运行，确认通过**

Run: `PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/sub/ ./internal/orchestrate/ -v -run 'TestJob|TestStore|TestCollector|TestDiff|TestTruncate|TestMarkdown|TestCrossVendor'`
Expected: PASS。

- [ ] **Step 5: checkpoint**

**验收命令**
```
verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/sub/... ./internal/orchestrate/... && go build ./...
```

### Task 6: 后台 worker 与 `pactify sub` 命令

**Files:**
- Create: `internal/sub/harness.go`、`internal/sub/worker.go`、`cmd/pactify/cmd_sub.go`
- Modify: `cmd/pactify/commands.go`（注册 `newSubCmd()`）
- Test: `internal/sub/worker_test.go`、`cmd/pactify/cmd_sub_test.go`

**Interfaces:**
- Consumes: Task 2/4 的 cockpit 接口与 `ReadKimiRecord`；Task 5 的全部导出符号；`orchestrate.CrossVendorStrip`。
- Produces：

```go
// sub/harness.go
type Harness struct {
	Name    string // "codex" | "kimi"
	Kind    string // "codex-cli" | "kimi-cli"
	Backend func() cockpit.Backend
	Version func(ctx context.Context) (string, error) // `codex --version` / `kimi --version` 的最后一个字段
	// Verify 用 harness 自己的独立证据核实实际模型与 token；返回 ok=false 表示无独立证据。
	Verify func(sessionID string, info cockpit.SessionInfo) (model string, in, out int, ok bool)
}

func Lookup(name string) (Harness, bool)
func Names() []string // 排序后的已注册 harness 名

// sub/worker.go
type WorkerConfig struct {
	ToolCallLimit int           // 只读档的工具调用上限；0 = 不限；默认 40
	IdleTimeout   time.Duration // 无事件且工作区无变化的时长；默认 10 分钟
	Now           func() time.Time
}

// RunJob 是后台 worker 的全部逻辑：起会话 → 设审批策略 → 发任务 → 收集 → 写结果。
func RunJob(ctx context.Context, store Store, id string, h Harness, cfg WorkerConfig) (Result, error)
```

  CLI（`cmd/pactify/cmd_sub.go`）：

```
pactify sub run <harness> [--model M] [--effort E] [--read-only | --write] [--resume | --fresh]
                          [--wait 540s] [--cwd DIR] [--json]        # 任务说明从 stdin 读
pactify sub wait <job> [--timeout 0] [--json]                       # 0 = 一直等
pactify sub status [<job>] [--json]                                 # 不给 job 时列最近 10 个
pactify sub result <job> [--json]
pactify sub cancel <job>
pactify sub __worker <job>                                          # 隐藏；只由 run 调用
```

  退出码：结果成功 0；job 失败或被取消 1；参数错误、未知 harness、模型不可用 2；`--wait` 到点仍在运行 3（stdout 输出 job id 与「仍在运行」提示）。

- [ ] **Step 1: 写失败测试——用假后端跑完整 worker**

```go
// fakeBackend 按脚本吐事件；Approvals 通道上可注入权限请求。
type fakeSession struct {
	events    chan cockpit.Event
	approvals chan cockpit.ApprovalRequest
	script    func(s *fakeSession)
	info      cockpit.SessionInfo
	canceled  atomic.Bool
}

func (s *fakeSession) Prompt(ctx context.Context, m cockpit.UserMessage) error { go s.script(s); return nil }
func (s *fakeSession) Interrupt(ctx context.Context) error {
	s.canceled.Store(true)
	s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "interrupted"}
	return nil
}
func (s *fakeSession) Events() <-chan cockpit.Event                  { return s.events }
func (s *fakeSession) Approvals() <-chan cockpit.ApprovalRequest     { return s.approvals }
func (s *fakeSession) ThreadID() string                              { return "th-fake" }
func (s *fakeSession) Close() error                                  { return nil }
func (s *fakeSession) Info() cockpit.SessionInfo                     { return s.info }

type fakeBackend struct{ sess *fakeSession; gotOpts cockpit.StartOpts }

func (b *fakeBackend) Start(ctx context.Context, o cockpit.StartOpts) (cockpit.Session, error) {
	b.gotOpts = o
	return b.sess, nil
}
func (b *fakeBackend) Resume(ctx context.Context, id string) (cockpit.Session, error) { return b.sess, nil }

func newFakeHarness(b *fakeBackend) Harness {
	return Harness{Name: "fake", Kind: "codex-cli", Backend: func() cockpit.Backend { return b },
		Version: func(context.Context) (string, error) { return "9.9.9", nil },
		Verify:  func(string, cockpit.SessionInfo) (string, int, int, bool) { return "", 0, 0, false }}
}

func gitRepo(t *testing.T) string // 建临时仓库并提交一个文件（实现同 diff_test 辅助）

func TestRunJobWriteSucceeds(t *testing.T) {
	dir := gitRepo(t)
	sess := &fakeSession{events: make(chan cockpit.Event, 16), approvals: make(chan cockpit.ApprovalRequest, 4),
		info: cockpit.SessionInfo{Model: "gpt-6-luna"}}
	sess.script = func(s *fakeSession) {
		_ = os.WriteFile(filepath.Join(dir, "out.txt"), []byte("hi\n"), 0o644)
		s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 100, OutputTokens: 5}}
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "WROTE"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	b := &fakeBackend{sess: sess}
	store := Store{Root: t.TempDir()}
	if _, err := store.Create("j1", Request{Harness: "fake", Model: "gpt-6-luna", Access: "write", Cwd: dir, Brief: "write it"}); err != nil {
		t.Fatal(err)
	}
	res, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusSucceeded || res.FinalText != "WROTE" || res.ModelActual != "gpt-6-luna" || res.CLIVersion != "9.9.9" {
		t.Fatalf("result = %+v", res)
	}
	if b.gotOpts.Access != cockpit.AccessWrite || b.gotOpts.Model != "gpt-6-luna" || b.gotOpts.RepoDir != dir {
		t.Fatalf("start opts = %+v", b.gotOpts)
	}
	if len(res.Diff.Files) != 1 || res.Diff.Files[0].Path != "out.txt" || !res.Diff.Files[0].New {
		t.Fatalf("diff = %+v", res.Diff)
	}
	if st, _ := store.Load("j1"); st.Status != StatusSucceeded {
		t.Fatalf("state = %+v", st)
	}
}

func TestRunJobReadOnlyDeniesEveryApproval(t *testing.T) {
	dir := gitRepo(t)
	decided := make(chan cockpit.Decision, 2)
	sess := &fakeSession{events: make(chan cockpit.Event, 16), approvals: make(chan cockpit.ApprovalRequest, 4)}
	sess.script = func(s *fakeSession) {
		s.approvals <- cockpit.ApprovalRequest{Kind: "permission", ToolName: "ExitPlanMode",
			Respond: func(d cockpit.Decision) error { decided <- d; return nil }}
		<-time.After(50 * time.Millisecond)
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	store := Store{Root: t.TempDir()}
	_, _ = store.Create("j2", Request{Harness: "fake", Access: "read-only", Cwd: dir, Brief: "look"})
	if _, err := RunJob(context.Background(), store, "j2", newFakeHarness(&fakeBackend{sess: sess}), WorkerConfig{}); err != nil {
		t.Fatal(err)
	}
	if d := <-decided; d != cockpit.DecisionDeny {
		t.Fatalf("decision = %q", d)
	}
}

// Review Focus 4：只读档工具调用超限即中断，并在结果首行标明。
func TestRunJobToolCallLimitInterrupts(t *testing.T) {
	dir := gitRepo(t)
	sess := &fakeSession{events: make(chan cockpit.Event, 64), approvals: make(chan cockpit.ApprovalRequest, 1)}
	sess.script = func(s *fakeSession) {
		for i := 0; i < 50 && !s.canceled.Load(); i++ {
			s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "ExitPlanMode"}}
		}
	}
	store := Store{Root: t.TempDir()}
	_, _ = store.Create("j3", Request{Harness: "fake", Access: "read-only", Cwd: dir, Brief: "look"})
	res, err := RunJob(context.Background(), store, "j3", newFakeHarness(&fakeBackend{sess: sess}), WorkerConfig{ToolCallLimit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !sess.canceled.Load() || res.Status != StatusFailed || !strings.HasPrefix(res.Markdown(), "⚠️") || !strings.Contains(res.Markdown(), "工具调用超限") {
		t.Fatalf("res = %+v", res)
	}
}

func TestRunJobFlagsModelMismatch(t *testing.T) {
	dir := gitRepo(t)
	sess := &fakeSession{events: make(chan cockpit.Event, 4), approvals: make(chan cockpit.ApprovalRequest, 1),
		info: cockpit.SessionInfo{Model: "kimi-code/k3-256k"}}
	sess.script = func(s *fakeSession) { s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted} }
	store := Store{Root: t.TempDir()}
	_, _ = store.Create("j4", Request{Harness: "fake", Model: "kimi-code/kimi-for-coding", Access: "write", Cwd: dir, Brief: "x"})
	res, _ := RunJob(context.Background(), store, "j4", newFakeHarness(&fakeBackend{sess: sess}), WorkerConfig{})
	if !contains(res.Warnings, "实际模型与请求不一致") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}
```

  （`contains(ss []string, sub string) bool` 为 worker_test.go 内的辅助：任一元素包含 sub 即返回 true。`gitRepo` 的实现与 Task 5 diff_test 的 `git` / `write` 辅助相同，在本文件内各自定义，不跨文件共享测试辅助。）

- [ ] **Step 2: 写失败测试——CLI 的后台与等待（Review Focus 5）**

```go
// cmd_sub_test.go：用环境变量 PACTIFY_SUB_FAKE=1 让 sub 使用内置的假 harness "fake"
// （在 harness.go 中仅当该变量为 1 时注册，脚本：睡 2 秒后写 out.txt 并回复 DONE）。
func TestSubRunWaitTimesOutThenWaitCollects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	repo := gitRepoForCLI(t)
	bin := buildPactify(t) // go build 到 t.TempDir()，因为 run 要 exec 自身的 __worker

	out, code := runCLI(t, bin, repo, "brief text", "sub", "run", "fake", "--write", "--wait", "200ms", "--json")
	if code != 3 {
		t.Fatalf("want exit 3 (still running), got %d: %s", code, out)
	}
	var started struct{ JobID string `json:"jobId"` }
	if err := json.Unmarshal([]byte(out), &started); err != nil || started.JobID == "" {
		t.Fatalf("bad output %q", out)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "wait", started.JobID, "--json")
	if code != 0 || !strings.Contains(out, `"finalText": "DONE"`) {
		t.Fatalf("wait: code=%d out=%s", code, out)
	}
}

func TestSubRunUnknownHarnessExits2(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	_, code := runCLI(t, bin, gitRepoForCLI(t), "x", "sub", "run", "nope", "--write")
	if code != 2 {
		t.Fatalf("code = %d", code)
	}
}
```

  （`buildPactify`、`runCLI(t, bin, dir, stdin string, args ...string) (stdout string, exitCode int)`、`gitRepoForCLI` 为该测试文件内的辅助；`runCLI` 继承测试设置的环境变量。注意 `cmd/pactify` 已有 `testmain_test.go` 调用 `testenv.Isolate()`。）

- [ ] **Step 3: 运行，确认失败**

Run: `PACTIFY_HOME="$(mktemp -d)" go test ./internal/sub/ ./cmd/pactify/ -run 'TestRunJob|TestSub' -v`
Expected: 编译失败（`RunJob`、`Lookup`、`newSubCmd` 未定义）。

- [ ] **Step 4: 实现 `harness.go`**

```go
var registry = map[string]Harness{
	"codex": {
		Name: "codex", Kind: "codex-cli",
		Backend: cockpit.NewCodexBackend,
		Version: cliVersion("codex"),
		Verify: func(_ string, info cockpit.SessionInfo) (string, int, int, bool) {
			return info.Model, 0, 0, info.Model != "" // 启动响应即独立证据
		},
	},
	"kimi": {
		Name: "kimi", Kind: "kimi-cli",
		Backend: func() cockpit.Backend { return cockpit.NewACPBackend("kimi-cli") },
		Version: cliVersion("kimi"),
		Verify: func(sid string, _ cockpit.SessionInfo) (string, int, int, bool) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", 0, 0, false
			}
			rec, err := cockpit.ReadKimiRecord(filepath.Join(home, ".kimi-code"), sid)
			if err != nil || len(rec.Models) == 0 {
				return "", 0, 0, false
			}
			return rec.Models[len(rec.Models)-1], rec.InputTokens, rec.OutputTokens, true
		},
	},
}
```

  `cliVersion(bin)` 运行 `<bin> --version`（超时 10 秒），取输出第一行最后一个空白分隔字段。`PACTIFY_SUB_FAKE=1` 时额外注册 `"fake"`（仅供 CLI 测试；脚本见 Step 2 说明），在 `Lookup` 内判断，生产路径不可见。

- [ ] **Step 5: 实现 `RunJob`**

顺序：
1. `store.Load(id)`；状态置 running、写入 `PID = os.Getpid()`。
2. `snap := TakeSnapshot(req.Cwd)`；`ver, _ := h.Version(ctx)`。
3. 打开 `store.LogPath(id)`（追加写）作为 `StartOpts.Stderr`，同时每个事件以一行 JSON 记入此日志。
4. `opts := cockpit.StartOpts{RepoDir: req.Cwd, Model: req.Model, Effort: req.Effort, Access: <read-only→AccessReadOnly, write→AccessWrite>, Env: orchestrate.CrossVendorStrip(h.Kind), Stderr: log}`。
5. 会话：`req.Resume` 非空时，若后端实现 `cockpit.OptsResumer` 用 `ResumeWith(ctx, req.Resume, opts)`，否则 `Resume`；为空时 `Start(ctx, opts)`。出错 → 状态 failed，`Result.Warnings` 记录错误原文（模型不可用的错误原样保留可选列表），返回。
6. 审批协程：从 `Approvals()` 读，只读档一律 `DecisionDeny`，可写档一律 `DecisionAllow`。
7. `Prompt(ctx, UserMessage{Text: req.Brief})`。
8. 主循环：读 `Events()` 交给 `Collector`；只读档且 `ToolCallLimit > 0` 且 `ToolCalls > ToolCallLimit` → `Interrupt`，记警告「工具调用超限（N 次）」，10 秒内没有回合结束事件就 `Close`；空闲看门狗：`IdleTimeout` 内既无事件、`gitx.TreeFingerprint(req.Cwd)` 也没变 → `Interrupt`，记警告「空闲超时」；`ctx` 取消（`cancel` 命令）→ `Interrupt`，状态 canceled；`Collector.TurnDone` → 退出循环。
9. 核实：`info := sess.(cockpit.InfoSession).Info()`（未实现则零值）；`vModel, vin, vout, ok := h.Verify(sess.ThreadID(), info)`；`ModelActual` 取 `ok ? vModel : info.Model`，`ModelVerified = ok`；kimi 的 token 用 `vin/vout`（ok 时），codex 用 Collector 的累计值；`req.Model != ""` 且 `ModelActual != ""` 且二者不等（codex 比较原样字符串，kimi 比较时允许请求值为 `modelAlias`）→ 警告「实际模型与请求不一致」。
10. `DiffSince(req.Cwd, snap)`；组装 `Result`（`TruncateFinal`、`Duration` 格式 `1m02s`），写 `result.json`；状态 succeeded（回合正常结束且无超限、无空闲超时）或 failed；`Close` 会话。

- [ ] **Step 6: 实现 `cmd_sub.go`**

- `run`：校验 harness（`Lookup` 失败 → 列出 `Names()`，退出 2）与 `--read-only` / `--write` 二选一（都没给时默认 `--write`）；读 stdin 作 brief（为空则退出 2）；`--resume` 时用 `LastSessionFor` 取会话 id，取不到退出 2 并提示「该仓库还没有可续接的 <harness> 会话」；`Create` job；以 `exec.Command(os.Args[0], "sub", "__worker", id)` 启动 worker：`SysProcAttr{Setsid: true}`、stdin 为 `/dev/null`、stdout/stderr 追加到 `<dir>/worker.out`，`Start` 后立即 `Release`；然后轮询 `state.json`（每 250ms）直到终态或 `--wait` 到期。到期 → 输出 `{"jobId":..., "status":"running", "hint":"pactify sub wait <id>"}`（`--json`）或一行中文提示，退出 3；终态 → 输出 `result.Markdown()` 或 JSON，按状态给退出码。
- `__worker`：`ctx` 绑定 SIGTERM；`RunJob`；panic 时把状态写成 failed 并记录堆栈到日志。
- `wait`：轮询直到终态（`--timeout` 非 0 时到期退出 3），输出同 `run` 的终态输出。
- `status`：给 job 时输出状态与耗时；不给时列 `Root` 下按创建时间倒序的最近 10 个。
- `result`：读 `result.json`；job 未结束时退出 3。
- `cancel`：读 `PID`，向其进程组发 SIGTERM（worker 收到后走 `Interrupt` 路径），等待最多 15 秒，状态非终态则 SIGKILL 并置 canceled。
- 在 `commands.go` 的 `root.AddCommand(...)` 列表里加入 `newSubCmd()`。

- [ ] **Step 7: 运行，确认通过**

Run: `PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/sub/... ./cmd/pactify/ -run 'TestRunJob|TestSub|TestJob|TestStore|TestCollector|TestDiff|TestTruncate|TestMarkdown' -v`
Expected: PASS。

- [ ] **Step 8: 真机端到端（evidence 必附）**

```bash
export PACTIFY_HOME="$(mktemp -d)"
TMPREPO="$(mktemp -d)"; git -C "$TMPREPO" init -q && echo hi > "$TMPREPO/a.txt" && git -C "$TMPREPO" add -A && git -C "$TMPREPO" -c user.email=t@t -c user.name=t commit -qm base
echo "Create a file named probe.txt containing hi. Reply with one line: WROTE." | go run ./cmd/pactify sub run codex --model gpt-6-luna --write --cwd "$TMPREPO"
echo "Create a file named probe2.txt containing hi. Reply with one line: WROTE." | go run ./cmd/pactify sub run kimi --model kimi-code/kimi-for-coding --write --cwd "$TMPREPO"
echo "List the files in this repo. Do not modify anything." | go run ./cmd/pactify sub run kimi --read-only --cwd "$TMPREPO"
```
Expected：前两次结果摘要首行为 `✓`，模型与请求一致且标注已核实，改动各含 1 个新文件；第三次无文件改动，工具调用数不超过 40。把三份 Markdown 输出贴进 evidence。

- [ ] **Step 9: checkpoint**

**验收命令**
```
verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/sub/... ./internal/cockpit/... ./internal/acp/... ./cmd/pactify/ && go build ./... && go run ./cmd/pactify sub --help | grep -q "wait"
```

---

## Feature `p0-plugin`（AJ-617）

### Task 7: Claude Code 插件最小版

**Files:**
- Create: `plugins/pactify/.claude-plugin/plugin.json`
- Create: `plugins/pactify/skills/codex/SKILL.md`、`plugins/pactify/skills/kimi/SKILL.md`
- Create: `plugins/pactify/agents/codex.md`、`plugins/pactify/agents/kimi.md`
- Create: `plugins/pactify/hooks/hooks.json`、`plugins/pactify/hooks/check-pactify-sub.sh`
- Create: `plugins/pactify/README.md`
- Modify: `.claude-plugin/marketplace.json`
- Test: `tests/plugin_pactify.bats`

**Interfaces:**
- Consumes: Task 6 的 CLI（`pactify sub run/wait/result`、退出码 0/1/2/3、`--json` 输出含 `jobId`）。
- Produces: `/codex <任务>`、`/kimi <任务>` 两个入口；插件名 `pactify`（`/pactify:codex` 始终可用）。

- [ ] **Step 1: 写失败测试（bats）**

```bash
#!/usr/bin/env bats
REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"
P="$REPO/plugins/pactify"

@test "marketplace lists the pactify plugin" {
  run python3 -c "import json;d=json.load(open('$REPO/.claude-plugin/marketplace.json'));print([p['name'] for p in d['plugins']])"
  [ "$status" -eq 0 ]
  [[ "$output" == *"'pactify'"* ]]
}

@test "plugin.json is valid and named pactify" {
  run python3 -c "import json;print(json.load(open('$P/.claude-plugin/plugin.json'))['name'])"
  [ "$output" = "pactify" ]
}

@test "forwarding agents use haiku and only Bash" {
  for h in codex kimi; do
    grep -q '^model: haiku$' "$P/agents/$h.md"
    grep -q '^tools: Bash$' "$P/agents/$h.md"
    grep -q "pactify sub run $h" "$P/agents/$h.md"
  done
}

@test "skills route large tasks and wait on the main thread" {
  for h in codex kimi; do
    grep -q '^name: '"$h"'$' "$P/skills/$h/SKILL.md"
    grep -q 'run_in_background' "$P/skills/$h/SKILL.md"
    grep -q 'pactify sub wait' "$P/skills/$h/SKILL.md"
  done
}

@test "session-start hook reports a missing pactify without failing the session" {
  run env PATH=/usr/bin:/bin bash "$P/hooks/check-pactify-sub.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"pactify"* ]]
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `bats tests/plugin_pactify.bats`
Expected: 全部 FAIL（文件不存在）。

- [ ] **Step 3: 写插件文件**

`plugins/pactify/.claude-plugin/plugin.json`：
```json
{
  "name": "pactify",
  "description": "Delegate large coding tasks from Claude Code to other coding agent harnesses (codex, kimi) as subagents, with verified model and cost.",
  "version": "0.1.0",
  "author": { "name": "agentjoey" },
  "homepage": "https://github.com/agentjoey/pactify",
  "repository": "https://github.com/agentjoey/pactify",
  "license": "MIT"
}
```

`plugins/pactify/agents/codex.md`（kimi.md 相同，把 codex 换成 kimi、模型示例换成 `kimi-code/kimi-for-coding`）：
```markdown
---
name: codex
description: Forward one large, self-contained coding task to the codex harness via `pactify sub`. Use only when the main thread has already written a self-contained brief.
model: haiku
tools: Bash
---

You are a thin forwarding wrapper. Your only job is one Bash call:

    pactify sub run codex <flags> --json <<'BRIEF'
    <the brief you were given, verbatim>
    BRIEF

Flags: add `--read-only` if the brief says the task must not modify files, otherwise `--write`.
Add `--model <name>` only if the brief names a model. Add `--resume` only if the brief says to continue
the previous codex session in this repository. Add `--wait 540s`.

Do not read files, search, plan, or summarize. Return the command's stdout verbatim. If the exit code is 3,
return the stdout verbatim as well: it contains the job id that the main thread will wait on.
```

`plugins/pactify/skills/codex/SKILL.md`（kimi 同构；kimi 版另加一段「只读任务：kimi 用 plan 模式，结果首行若出现『工具调用超限』，说明 kimi 在只读模式下没收敛，改用 codex 或改成可写任务」）：
```markdown
---
name: codex
description: Delegate a large coding task (multi-file change, investigation, or anything that needs running tests) to the codex harness and get back a verified result summary. Not for small edits you can do directly.
---

# Delegate to codex

Use this only for **large** tasks: touching 3+ files, or needing exploration and test runs, or likely to take
you more than ~10 minutes. Small edits: do them yourself — each delegation carries a fixed cost of tens of
thousands of tokens on the callee side.

1. Write a self-contained brief. The callee starts cold and cannot see this conversation. Include:
   goal; relevant file paths; constraints (what must not change); acceptance criteria (exact commands to run);
   whether it is read-only.
2. Start it in the background from the **main thread** (not a subagent — a subagent's background command
   notifies only that subagent):

       pactify sub run codex --write --wait 0s --json <<'BRIEF'
       ...brief...
       BRIEF

   It exits 3 immediately with `{"jobId": ...}`. Then run, with `run_in_background: true`:

       pactify sub wait <jobId>

3. When the background command completes, read its output: the result summary. The first line carries any
   warning (model mismatch, tool-call limit, idle timeout). Review the diff it lists before building on it.
4. To continue the same codex session in this repository, pass `--resume` on the next `run`.

For a quick task you are willing to block on (under ~9 minutes), you may instead delegate to the `codex`
subagent, which calls `pactify sub run codex --wait 540s` once and returns the summary.
```

`plugins/pactify/hooks/hooks.json`：
```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/check-pactify-sub.sh" } ] }
    ]
  }
}
```

`plugins/pactify/hooks/check-pactify-sub.sh`（可执行）：
```bash
#!/usr/bin/env bash
# Report whether the pactify CLI with the `sub` command is available. Never fail the session.
if ! command -v pactify >/dev/null 2>&1; then
  echo "pactify plugin: the pactify CLI is not on PATH; /codex and /kimi will not work. Install: go install github.com/agentjoey/pactify/cmd/pactify@latest"
  exit 0
fi
if ! pactify sub --help >/dev/null 2>&1; then
  echo "pactify plugin: the installed pactify has no 'sub' command; upgrade pactify."
  exit 0
fi
exit 0
```

`.claude-plugin/marketplace.json` 的 `plugins` 数组追加：
```json
{ "name": "pactify", "source": "./plugins/pactify", "description": "Delegate large tasks to codex / kimi as subagents with verified model and cost." }
```

`plugins/pactify/README.md`：安装（`/plugin marketplace add agentjoey/pactify` → `/plugin install pactify@pactify`）、前提（`pactify` 在 PATH 上、codex / kimi 已登录）、两个入口的用法、结果摘要字段说明、已知限制（kimi 只读依赖 plan 模式；callee 每次委派有 2–5 万 token 固定开销）。

- [ ] **Step 4: 运行，确认通过**

Run: `bats tests/plugin_pactify.bats`
Expected: 5 个用例 PASS。

- [ ] **Step 5: checkpoint**（Claude 会话评审时会在真实 Claude Code 里装上插件，完成 Task 8 Step 1 的冒烟后才 accept。）

**验收命令**
```
verify: bats tests/plugin_pactify.bats
```

---

## AJ-618 · Phase 0 实测（Claude 会话亲自执行）

### Task 8: 10 个大任务 × codex / kimi，加 3 个只读任务，出报告

**Files:**
- Create: `docs/phase0-report.md`
- Modify: `docs/specs/harness-subagents.md`（§6 结论、§10 补记）

- [ ] **Step 1: 插件冒烟**：在 clone 里 `/plugin marketplace add ./`，安装 `pactify` 插件；用 `/codex` 与 `/kimi` 各跑一个小任务，确认结果摘要回到主线程；后台路径（`run --wait 0s` + `run_in_background` 的 `sub wait`）跑 3 次，完成通知都回到主线程（Q3 的前 3 次）。
- [ ] **Step 2: 选题并冻结**：从 Linear / Notion Requests / 本仓近期 PR 中挑 10 个符合「≥3 个文件，或需要探索与跑测试，Claude 自己做预计 ≥10 分钟」的大任务，外加 3 个只读任务（调研或评审）。写进报告的「任务表」：编号、描述、验收命令、预期结果。开跑后不换题。
- [ ] **Step 3: 执行**：每个大任务在全新 worktree 里分别交给 codex 与 kimi（共 20 次），每次记录：harness、请求 / 实际模型、是否核实、callee token、Claude 侧额外 token（主线程该次委派前后的用量差 + 转发 subagent 用量）、耗时、是否一次完成（跑验收命令判定）、失败原因。3 个只读任务两家各跑一次，额外记录工具调用数与是否有文件改动。期间若 codex 或 kimi 自动更新（kimi 2.2.0 已在待装），记录 `sub` 是否发现版本变化并给出提示（K3）。
- [ ] **Step 4: 计算指标**：Q1（每家 ≥7/10 一次完成）、Q2（Claude 侧额外 token 中位数 < 10000）、Q3（≥5 次后台完成通知全部到达）、Q4（20 次都有 token，codex 附额度事件）、Q5（20 次模型一致）、K1、K2、K3。
- [ ] **Step 5: 写报告与结论**：`docs/phase0-report.md` 含任务表、逐次记录表、指标表、失败案例分析、结论（进入 M1 / 先改交接方式 / kimi 只读是否开放）。spec §6 末尾追加「Phase 0 结论（日期）」一段。开 PR，CI 绿后合入。
- [ ] **Step 6: 收尾**：Linear AJ-618 与 M0 里程碑置 Done；在 Notion Slices 写一条记录（Delivered、Evidence、Deviation、Issues 列出 AJ-612 … AJ-618 的结果）；把 `pactify config base-branch main` 改回，删除 `p0-integration` 分支（远端分支删除前确认已全部合入 main）。

---

## 自查记录（计划作者填写）

- **Spec 覆盖**：§4.1 CLI 面 → Task 6；§4.2 adapters → Task 2、4；§4.3 结果摘要 → Task 5；§5.1 交接 → Task 7 的 skill 与 Task 8；§5.2 前后台 → Task 6、7；§5.3 隔离（不提交、diff 只算 callee）→ Task 5、6；§5.4 权限映射 → Task 2、4、6；§5.5 host 开销 → Task 8；§6 → Task 8；§10 实测结论 → Task 2、3、4 的真机步骤；§11 版本记录 → Task 6（`CLIVersion`）。§11 的动态模型表在 M0 只覆盖「模型不可用时列出可选项」（Task 4 / Task 2 由服务端报错），完整版在 M1。
- **类型一致性**：`StartOpts`、`Access*`、`SessionInfo`、`InfoSession`、`OptsResumer`、`StateTurn*` 在 Task 2 定义，Task 4、5、6 原名引用；`NewSessionResult` / `ConfigOption` / `FindConfig` 在 Task 3 定义，Task 4 引用；`Store` / `Request` / `Result` / `Collector` / `Snapshot` 在 Task 5 定义，Task 6 引用。
- **Review Focus**：五条均已在对应任务中写成测试（Task 2 `TestCodexTurnEndNeverDropped`、Task 4 `TestACPPromptEmitsTurnEndEvenWhenBufferFull` 与 `TestApplyACPOptsUnknownModelFailsWithChoices`、Task 5 `TestDiffSinceExcludesPreexistingChanges`、Task 6 `TestRunJobToolCallLimitInterrupts` 与 `TestSubRunWaitTimesOutThenWaitCollects`）。
