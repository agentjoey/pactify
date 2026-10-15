# Task p0-codex-backend

**Feature:** p0-codex · **Owner:** codex-worker · **Reviewer:** claude-session · **Linear:** AJ-614 · **Deps:** p0-schema

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

## 验收命令

verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/cockpit/... ./internal/serve/... && go build ./...
