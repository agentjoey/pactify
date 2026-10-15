# Task p0-kimi-backend

**Feature:** p0-kimi · **Owner:** codex-worker · **Reviewer:** claude-session · **Linear:** AJ-615 · **Deps:** p0-acp-client

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

## 验收命令

verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/cockpit/... ./internal/acp/... ./internal/serve/... && go build ./...
