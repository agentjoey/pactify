# Task acp-per-session (A4) — ACP Client 按 sessionID 分发(修多 session 串台)

## 目标(deep-integration-backends-spec §5.2 / cockpit E3 前置)
`internal/acp` 的 Client 现在 `dispatchUpdate`/permission 只有**单个全局** handler
(`onUpdate`/`onPerm`),不按 `SessionID` 过滤——一个 ACP client 同时跑多个 session(cockpit
多会话 / worker-attach)会**串台**。加**按 sessionID 的 per-session handler**,全局 handler 作 fallback。
纯加性,单 session 现有行为字节不变。

## 改文件(仅这些)
- `internal/acp/acp.go`
- `internal/acp/acp_test.go`(加 per-session 路由测试)

## 契约
Client 加两个 per-session 注册表(用现有 `c.hmu` 锁保护):
```go
perUpdate map[SessionID]func(SessionUpdate)
perPerm   map[SessionID]func(PermissionRequest) PermissionOutcome
```
新方法:
- `OnSessionUpdateFor(sid SessionID, fn func(SessionUpdate))`:注册/覆盖该 session 的 update handler;
  fn==nil 则删除该 session 条目。
- `OnPermissionRequestFor(sid SessionID, fn func(PermissionRequest) PermissionOutcome)`:同上,permission。
- `ClearSession(sid SessionID)`:删除该 session 的两个 per-session 条目(会话结束时收)。
- 在 newClient 里初始化两个 map(非 nil)。

分发改造:
- `dispatchUpdate(u)`:先查 `perUpdate[u.SessionID]`,命中就用它;否则 fallback 到全局 `onUpdate`
  (现有行为)。
- `handleServerRequest` 的 permission 分支:先查 `perPerm[p.SessionID]`,命中就用它;否则 fallback
  到全局 `onPerm`;都没有则维持现状(auto-cancel)。
- 全局 `OnSessionUpdate`/`OnPermissionRequest` **保留不变**(向后兼容:单 session 调用方零改动)。

锁:所有 map 读写走 `c.hmu`;handler 调用**不要**持锁(先取出 fn 再解锁再调,照现有 dispatchUpdate 的写法)。

## 测试(acp_test.go)
- 用现有的 pipe-backed 测试法(newClient over io.Pipe)或直接构造 Client 调 dispatchUpdate:
  - 注册 sid "A"、"B" 两个 per-session update handler + 一个全局;dispatch 三条 update
    (sid=A、sid=B、sid=C 未注册)→ A 进 A、B 进 B、C 进全局 fallback。
  - permission 同理:per-session perm 命中优先,未注册 fallback 全局。
  - `ClearSession("A")` 后,sid=A 的 update 改走全局 fallback。
  - 单 session 回归:只注册全局(不注册 per-session)→ 所有 update 进全局(现有行为不变)。

## 验收 / Acceptance(视角: correctness — 按 sid 精确路由、fallback 正确、单 session 零回归、无锁内回调)
- reviewer 独立跑 verify(含 -race,因涉并发 handler)。

## verify
verify: go build ./... && go test -race ./internal/acp/
