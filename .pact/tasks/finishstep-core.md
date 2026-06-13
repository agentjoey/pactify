# finishstep-core — 收尾交付步（push + 开 PR）

为 Pactify 实现"链路终点：交付到远端"。orchestrate 把 feature 合并到本地 main 后，
用户需要一步把成果推到 origin、并可选开 PR。新增独立包 + CLI 命令，**不碰** orchestrate
内部。

## 要实现的文件

### 1) `internal/finish/finish.go`（新建）

实现一个可注入命令执行器的交付器，便于单测（不真的跑 git/gh）：

```go
package finish

import (
	"fmt"
	"strings"
)

// Runner runs an external command in dir and returns combined output. Injected so
// tests can fake git/gh without spawning processes.
type Runner func(dir string, name string, args ...string) (string, error)

// Finisher delivers merged work to the remote: push a branch, optionally open a PR.
type Finisher struct {
	Run      Runner
	HasGH    func() bool // reports whether the `gh` CLI is available
}

// Push pushes branch to remote (e.g. origin main). Returns the command output.
func (f Finisher) Push(dir, remote, branch string) (string, error) {
	if remote == "" || branch == "" {
		return "", fmt.Errorf("finish: remote and branch are required")
	}
	return f.Run(dir, "git", "push", remote, branch)
}

// OpenPR opens a pull request from head into base via `gh pr create`. When gh is
// not available it returns the manual command the user can run instead (NOT an
// error — degrade gracefully).
func (f Finisher) OpenPR(dir, base, head, title, body string) (string, error) {
	if base == "" || head == "" || title == "" {
		return "", fmt.Errorf("finish: base, head and title are required")
	}
	if f.HasGH != nil && !f.HasGH() {
		return fmt.Sprintf("gh not found — run manually:\n  gh pr create --base %s --head %s --title %q --body %q", base, head, title, body), nil
	}
	return f.Run(dir, "gh", "pr", "create", "--base", base, "--head", head, "--title", title, "--body", body)
}
```

要求（用 TDD，先写失败测试）：
- `Push` 在 remote 或 branch 为空时返回 error，不调用 Run。
- `Push` 用 `git push <remote> <branch>` 调用 Run，并把 Run 的输出原样返回。
- `OpenPR` 在 base/head/title 任一为空时返回 error。
- `OpenPR` 在 `HasGH()` 返回 false 时**不**调用 Run，返回一段包含 `gh pr create` 的手动指令字符串、err 为 nil。
- `OpenPR` 在 gh 可用时用 `gh pr create --base .. --head .. --title .. --body ..` 调用 Run。
- 用 fake Runner（记录收到的 name/args，返回预设输出）断言以上。

### 2) `internal/finish/finish_test.go`（新建）
表驱动 + fake Runner，覆盖上面每一条。`go test ./internal/finish/...` 必须全绿。

### 3) `cmd/pactify/cmd_finish.go`（新建）
加 `pactify finish` 命令（用 cobra，参照 cmd 目录里其它 `newXxxCmd` 的写法）：

- 默认：`pactify finish --branch main` → 调用 Finisher.Push(cwd, remote, branch)，打印输出。
- flags：`--remote`（默认 `origin`）、`--branch`（默认 `main`）、`--pr`（bool）、`--head`、`--title`、`--body`。
- 当 `--pr` 时：调用 OpenPR(cwd, branch, head, title, body)，打印输出。`--head`/`--title` 缺失时报错。
- 生产 Runner 用 `os/exec`（`exec.Command(name, args...)`，`cmd.Dir=dir`，`CombinedOutput`）；HasGH 用 `exec.LookPath("gh")`。
- 把命令注册进 root（在 `cmd/pactify/` 里找到 root command 的 `AddCommand(...)` 处，加上 `newFinishCmd()`）。

## 验收口径

```
go build ./...                       # 干净
go test ./internal/finish/...        # 全绿
pactify finish --help                # 显示 finish 命令与 flags
```

边界：只新建上述文件 + 在 root 注册命令那一行加 `newFinishCmd()`。不要改 orchestrate、agent、serve 等其它包。
verify: go build ./... && go test ./internal/finish/...
