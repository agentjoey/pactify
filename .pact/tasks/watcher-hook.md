# Task t2 · watcher-hook

**Feature:** relay (M3.4) · **Owner:** gemini-worker · **Reviewer:** claude · **Deps:** t1（relay client 已 accepted）

## 目标
把 t1 的 relay client 接进 serve——watcher 每广播一条事件就旁路一份给 relay；`pactify serve` 暴露 `--relay-url`/`--relay-token`（env `PACT_RELAY_TOKEN` 兜底）。**relay 未配置时 serve 行为字节级不变**（硬约束）。

## 要碰的文件
- 改 `internal/serve/api.go`：`Server` 结构加 `relay *relay` 字段；`New(projects)` 维持现签名，**另加** `SetRelay(url, token string)`（mirror 既有 `SetSeat`），内部 `s.relay = newRelay(url, token)`。
- 改 `internal/serve/watch.go`：`drainNew` 在 `s.hub.broadcast(id, t)` 之后加 `s.relay.enqueue(id, t)`（nil-safe）。`Stop()` 加 `if s.relay != nil { s.relay.stop() }`。
- 改 `cmd/pactify/cmd_serve.go`：加 `--relay-url`（默认 ""）、`--relay-token`（默认 `os.Getenv("PACT_RELAY_TOKEN")`）两 flag；`srv.SetSeat(seat)` 后调 `srv.SetRelay(relayURL, relayToken)`。
- **不要碰** relay.go 的内部实现（t1 已定）。

## 行为要求
- relay-url 为空 → `newRelay("",...)` 返回 nil → drainNew 的 enqueue no-op → **SSE 扇出、offset 推进、所有现有行为零变化**（硬约束，现有 watcher 测试不许回归）。
- relay 启用时，每条被 broadcast 的完整行也 enqueue（同一条、同一 id）。
- token flag > env `PACT_RELAY_TOKEN`（flag 非空用 flag）。serve 启动可加一行 relay 目标提示（**不打印 token**）。

## 验收测试
- relay==nil 时 drainNew 行为与现状一致（现有 watcher 测试不回归）。
- relay 启用时 drainNew 既 broadcast 又 enqueue（用记录型 fake 或断言 relay 收到；t2 只需证明"接上了"，端到端在 t3）。
- `go build ./...` 通过；`pactify serve --help` 显示新 flag。

## 验收命令
```
cd /Users/xtation/AgentWorks/Code_Claude/pactify && go test ./internal/serve/ && go build ./... && ./pactify serve --help 2>&1 | grep relay
```
（注：你是 antigravity/MCP 座席——经 pact MCP 工具 join/checkpoint。t1 accepted 后本 task deps 解锁，STATE.yml 会显示 t2 可领。）

## 完成方式
TDD。完成后经 MCP `checkpoint` 工具置 awaiting_review，evidence 附测试 + build 输出。不要自标 accepted。
