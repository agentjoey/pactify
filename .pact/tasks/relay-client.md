# Task t1 · relay-client

**Feature:** relay (M3.4) · **Owner:** opencode-worker · **Reviewer:** claude · **Deps:** 无

## 目标
实现一个 best-effort 异步 relay client——把 serve 的日志事件 POST 到可配置 HTTP 端点，**永不阻塞调用方、永不向调用方传播失败**。纯组件，本 task 不接 watcher（那是 t2）。

## 要碰的文件
- 新建 `internal/serve/relay.go`
- 新建 `internal/serve/relay_test.go`
- **不要碰** watch.go / api.go / cmd_serve.go（那是 t2）

## 契约（签名照此实现）
```go
package serve

// relay POSTs serve log events to a configured endpoint, best-effort & async.
// NEVER blocks the caller; NEVER propagates failures back to the watcher.
type relay struct { /* url, token, queue chan relayMsg, done chan struct{}, dropped 计数 ... */ }

type relayMsg struct { project, line string }

// newRelay builds a relay posting to url with optional bearer token.
// A "" url returns nil (relay disabled — callers nil-check before enqueue).
func newRelay(url, token string) *relay

// enqueue 把一条事件（project id + 原始 log.jsonl 行）投进有界队列。
// 非阻塞：队列满则丢【最旧】一条 + dropped 计数自增。
// nil relay 上调用是安全 no-op（调用方无需分支）。
func (r *relay) enqueue(project, line string)

// stop 排空/关闭并等后台 goroutine 退出（serve Stop 时调）。
func (r *relay) stop()
```
（后台 POST goroutine 的启动方式 newRelay 内启或显式 start 由你定，注释写清。暴露一个读 dropped 计数的方法供测试断言。）

## 行为要求
- 队列有界（cap 256）；满时丢最旧 + dropped 计数。
- POST：`Content-Type: application/json`；token 非空时加 `Authorization: Bearer <token>`。
- POST 失败 → 有界指数退避重试（≤3 次，退避上限几秒），仍失败则丢弃 + 计数。**失败绝不向调用方传播、绝不 panic。**
- POST body = `{"project":"<id>","event":<line 解析成的 JSON 对象>}`。line 本身是一条 JSON event；解析失败的 line 安全处理（跳过或原样包裹 + 计数），别 panic。
- `newRelay("","")` 返回 nil；`(*relay)(nil).enqueue(...)` 安全 no-op。

## 验收测试（relay_test.go 必须覆盖）
- `newRelay("","")` 返回 nil；nil relay 的 enqueue 不 panic。
- token header 注入（httptest.Server 断言收到的 Authorization）。
- body 信封格式正确（project + event 对象）。
- 队列满 → 丢最旧 + dropped 计数自增（灌 cap+N 条，确定性断言收到 ≤cap 且 dropped≥N）。
- receiver 返回 500 → enqueue 不阻塞不报错；重试后最终丢弃 + 计数。

## 验收命令
```
cd <repo> && go test ./internal/serve/ -run Relay -v
```

## 完成方式
TDD（先写失败测试）。完成后 `pactify checkpoint`（置 awaiting_review，evidence 附测试输出）。不要自标 accepted——reviewer 是 claude。
