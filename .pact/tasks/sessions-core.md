# sessions-core — agent 任务 session 清理框架

orchestrator 每驱动一棒都会 spawn 一个 agent session（opencode run / claude -p /
gemini -p）。跨多 task + 返工 + 重试会累积大量 session，可能拖累 agent。本任务实现
一个**可测的 session 清理框架**：按 agent kind 声明 list/prune 命令，提供清理入口。
**不碰** orchestrate/agent/serve 内部，只新建独立包 + CLI。

## 要实现的文件

### 1) `internal/sessions/sessions.go`（新建）

```go
package sessions

import "fmt"

// Runner runs an external command and returns combined output. Injected so tests
// fake the agent CLIs without spawning processes.
type Runner func(name string, args ...string) (string, error)

// Spec declares how to manage one agent kind's sessions. Command is the CLI
// binary; List lists sessions; Prune deletes them. An empty Prune means the kind
// has no verified headless session-prune command (cleanup unsupported — a no-op
// that reports so, never an error).
type Spec struct {
	Command string
	List    []string
	Prune   []string
}

// specs holds the per-kind session commands. Only gemini-cli has a verified
// headless session command set; the others are unsupported until confirmed (kept
// here as the single place to add them later).
var specs = map[string]Spec{
	"gemini-cli": {Command: "gemini", List: []string{"--list-sessions"}, Prune: nil}, // delete-session takes an index; bulk prune unverified → leave Prune empty (list only)
}

// Manager prunes/lists sessions via an injected Runner.
type Manager struct{ Run Runner }

// Supported reports whether kind has any known session command.
func Supported(kind string) bool {
	s, ok := specs[kind]
	return ok && s.Command != ""
}

// CanPrune reports whether kind has a verified prune command.
func CanPrune(kind string) bool {
	s, ok := specs[kind]
	return ok && len(s.Prune) > 0
}

// List returns the kind's session listing (or an error if unsupported).
func (m Manager) List(kind string) (string, error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" || len(s.List) == 0 {
		return "", fmt.Errorf("sessions: listing not supported for kind %q", kind)
	}
	return m.Run(s.Command, s.List...)
}

// Prune deletes the kind's sessions. When the kind has no verified prune command
// it returns (skipped=true, nil) — a graceful no-op, NOT an error — so callers can
// report "nothing to prune (unsupported)" instead of failing.
func (m Manager) Prune(kind string) (output string, skipped bool, err error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" || len(s.Prune) == 0 {
		return "", true, nil
	}
	out, err := m.Run(s.Command, s.Prune...)
	return out, false, err
}
```

要求（TDD，先写失败测试，用 fake Runner 记录收到的 name/args 并返回预设输出）：
- `Supported("gemini-cli")` 为 true；`Supported("opencode")`、`Supported("nope")` 为 false。
- `CanPrune("gemini-cli")` 为 false（Prune 为空）。
- `List("gemini-cli")` 用 `gemini --list-sessions` 调 Run，返回其输出。
- `List("opencode")` 返回 error（unsupported）。
- `Prune("gemini-cli")` 返回 skipped=true、err=nil、**不**调用 Run（Prune 为空）。
- `Prune("nope")` 返回 skipped=true、err=nil。
- 额外覆盖：若某 kind 的 Prune 非空（可在测试里临时构造一个 Manager 走通 prune 调用路径——通过给一个已知 kind 的 fake，或导出一个测试可注入 specs 的方式；若不便，至少用 fake Runner 断言 List 路径 + skipped 路径）。

### 2) `internal/sessions/sessions_test.go`（新建）
表驱动 + fake Runner，覆盖上面每条。`go test ./internal/sessions/...` 全绿。

### 3) `cmd/pactify/cmd_sessions.go`（新建）
加 `pactify sessions` 命令（cobra，参照其它 `newXxxCmd`）：
- 子命令 `list <kind>`：用生产 Runner（`exec.Command(name,args...).CombinedOutput`）调 Manager.List，打印输出；unsupported 时打印友好提示。
- 子命令 `prune <kind>`：调 Manager.Prune；skipped 时打印 `kind <kind>: session 清理暂不支持（无已验证命令）`，否则打印输出。
- 把命令注册进 root：在 `cmd/pactify/commands.go` 的 `root.AddCommand(...)` 那一处加上 `newSessionsCmd()`。

## 验收口径
```
go build ./...                       # 干净
go test ./internal/sessions/...      # 全绿
pactify sessions --help              # 显示 list/prune 子命令
pactify sessions prune opencode      # 打印 unsupported 友好提示，exit 0
```
边界：只新建上述文件 + root 注册一行。不要改其它包。
verify: go build ./... && go test ./internal/sessions/...
