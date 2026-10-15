# Task p0-sub-core

**Feature:** p0-sub · **Owner:** codex-worker · **Reviewer:** claude-session · **Linear:** AJ-616

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

// 2026-10-15 真机发现：codex 一轮会发出多条 Final 的 agentMessage（先是开场说明，最后才是答案），必须取最后一条。
func TestCollectorTakesLastCodexFinal(t *testing.T) {
	var c Collector
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "我会尝试在当前工作区创建 probe.txt。"})
	c.Add(cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "shell"}})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "BLOCKED"})
	if c.FinalText() != "BLOCKED" {
		t.Fatalf("final=%q", c.FinalText())
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

## 验收命令

verify: PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/sub/... ./internal/orchestrate/... && go build ./...
