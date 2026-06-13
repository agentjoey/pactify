# Task cli · cli

**Feature:** agents · **Owner:** opencode-worker · **Reviewer:** claude · **Deps:** reg

## 目标
`pactify agent scan/register/unregister` 子命令，挂在已有 `pactify agent` 下（与 add/docs 并列，见 cmd/pactify/cmd_agent.go 的 newAgentCmd）。

## 改文件
`cmd/pactify/cmd_agent.go`（加三个子命令）+ `cmd/pactify/cli_test.go`（加 smoke）。

## 行为
- `pactify agent scan`：agent.Scan() → 表格打印每 kind：`<kind>  installed|—  <detail>  [registered]`（registered 查 agentreg.Load().Has）。
- `pactify agent register <kind> [--label <s>]`：校验 agent.Get(kind) 已知 → agentreg.Load() → Register(kind,label, time.Now().Format("2006-01-02T15:04:05Z07:00")) → Save()。未知 kind 报错并列出 agent.Kinds()。
- `pactify agent unregister <kind>`：Load → Unregister → Save。
- 时间戳在 CLI 层取，传给 agentreg。

## 验收
cli_test（建 binary 跑，PACTIFY_HOME=tempdir 隔离）：agent scan 退出 0 且输出含某 kind；register opencode 后 scan 标 registered；unregister opencode 后不再 registered；register bogus 非零退出 + 列出已知 kinds。

## verify
```
go test ./cmd/pactify/ -run Agent
```

## 完成方式
TDD。座席 opencode-worker。checkpoint cli 附 verify 输出。不自标 accepted。
