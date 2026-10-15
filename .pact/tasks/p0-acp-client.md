# Task p0-acp-client

**Feature:** p0-kimi · **Owner:** codex-worker · **Reviewer:** claude-session · **Linear:** AJ-615

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

## 验收命令

verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/acp/... && go build ./...
