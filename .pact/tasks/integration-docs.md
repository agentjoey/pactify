# Task t3 · integration-docs

**Feature:** relay (M3.4) · **Owner:** opencode-worker · **Reviewer:** claude · **Deps:** t2（钩子+flag 已 accepted）

## 目标
端到端集成测试证明"append 日志 → relay 端点收到正确信封"，且"relay 端点失败不影响 watcher/SSE"；更新文档。

## 要碰的文件
- 新建 `internal/serve/relay_integration_test.go`
- 改 `docs/architecture.md`：加 "M3.4 relay 接口" 小节
- 改 `docs/operations.md`：加 relay 配置/运维说明
- **不要碰** relay.go / watch.go 的实现逻辑（t1/t2 已定）；只加测试 + 文档。

## 集成测试要求（relay_integration_test.go）
- 起 `httptest.Server` 当 relay receiver（记录收到的 body）。
- 构造 Server（`New` + `SetRelay(receiver.URL, "")`）over 一个临时 .pact/ 项目（参考既有 watch_test.go 怎么建临时 project + 写 log.jsonl）。
- 启 watcher → append 一条合法 event 行到该项目 log.jsonl → 超时内断言 receiver 收到 `{"project":...,"event":{...}}` 且字段正确。
- 第二例：receiver 恒返回 500 → append 行 → 断言 SSE 订阅者仍收到该行、offset 仍推进（relay 失败隔离）。
- **必须真实经过 fsnotify→drainNew→relay 全链路**（不是直接调 enqueue）。

## 文档要求
- architecture.md：挂载点（drainNew 旁路）、best-effort 异步、配置（serve --relay-url/--relay-token + PACT_RELAY_TOKEN）、线格式信封、失败隔离语义。
- operations.md：如何指端点、token 走 env/Keychain、丢弃计数含义、未配置=禁用。
- **文档示例用 env/Keychain，不写明文 token。**

## 验收命令
```
cd <repo> && go test ./internal/serve/ -v && go test ./...
```
（全绿）

## 完成方式
TDD。完成后 `pactify checkpoint` 置 awaiting_review，evidence 附全量测试输出。不要自标 accepted。
