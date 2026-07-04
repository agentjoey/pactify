# dm-acp-usage — ACP usage → per-task token 指标接线

> 母 spec:`docs/specs/driver-modernization.md` §A.3。依赖 dm-acp-runner。

## 背景
v0.5.1 建立了 orchestrate per-task token 捕获,但 CLI 路径无数据源(backlog 旧债「TOK 始终为 0」)。ACP 的 usage 通知有真数据。

## 交付
1. 找到 orchestrate 现有 token 捕获落点(grep `token` in internal/orchestrate/,v0.5.1 机制:runner capture → 记录),把 AcpRunner 的 OnUsage 回调接进同一落点——ACP 棒记真值,CmdRunner 棒行为不变。
2. serve /stats 路径确认自动透出(应零改动,验证即可)。

## 测试
假 ACP server 发带 usage 的 update → 断言 token 记录落盘/进事件与 CmdRunner 捕获同构。`go test ./internal/orchestrate/` 绿。

## 边界
不改 stats DTO(dm-stats 管);不做成本换算,只记 token 数。
