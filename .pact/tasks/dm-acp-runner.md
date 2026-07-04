# dm-acp-runner — AcpRunner + 传输路由 + --transport flag

> 母 spec:`docs/specs/driver-modernization.md` §A.2。依赖 dm-acp-core(`internal/acp` 已就绪)。

## 交付
1. `internal/orchestrate/acprunner.go`:`AcpRunner` 实现 `Runner` 接口(见 runner.go:53 LaunchContext)。生命周期:Spawn(按 kind 映射表)→ Initialize → NewSession(lc.RepoDir) → Prompt(lc.Briefing) → 等 StopReason → Close。env 注入 `PACT_AGENT_ID=<lc.Seat>`(同 CmdRunner)。
2. kind→ACP 命令映射(spec §A.2 表):kimi-cli=`kimi acp`;claude-code=`npx -y @agentclientprotocol/claude-agent-acp`(env 剥离 ANTHROPIC_API_KEY);codex-cli=`npx -y @zed-industries/codex-acp`;gemini-cli=`npx -y @google/gemini-cli --acp`;opencode 无。
3. idle watchdog:OnSessionUpdate 即活;Idle 时长无 update → kill 子进程,返回软失败 error(语义同 CmdRunner IdleTimeout)。
4. PermissionPolicy 三档 auto/escalate/deny(spec §A.2);默认 auto=自动选 /allow|approve|yes|once/ 选项。escalate=写 escalation 文件(复用 orchestrate 现有 escalation 机制)并对该请求回拒。
5. `RoutedLocalRunner`:按 kind 查 mode(默认全 cmd),acp→AcpRunner,否则 CmdRunner。cmd_orchestrate.go 加 `--transport kind=acp|cmd`(repeatable,解析同 --seat-kind)接进 Options。
6. usage:AcpRunner 聚合本棒 usage,经 `OnUsage(seat, task, Usage)` 回调暴露(dm-acp-usage 接线,本任务只留钩子并在测试断言回调触发)。

## 测试
复用/扩展 internal/acp 的假 server:完整棒成功、idle kill、permission 三策略行为、usage 回调、路由(kind 无映射→报错指引 --transport kind=cmd)。`go test ./internal/orchestrate/ -run 'Acp|Routed'` + `go build ./...` 绿。

## 边界
不改 loop.go 决策逻辑;不做 session 持久化(dm-session-resume);默认模式保持全 cmd(零行为变化)。
