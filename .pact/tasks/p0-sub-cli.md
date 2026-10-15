# Task p0-sub-cli

**Feature:** p0-sub · **Owner:** codex-worker · **Reviewer:** claude-session · **Linear:** AJ-616 · **Deps:** p0-sub-core

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

## 验收命令

verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/sub/... ./internal/cockpit/... ./internal/acp/... ./cmd/pactify/ && go build ./... && go run ./cmd/pactify sub --help | grep -q "wait"
