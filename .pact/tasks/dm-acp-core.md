# dm-acp-core — `internal/acp`:Go ACP 客户端(JSON-RPC/stdio)

> 母 spec:`docs/specs/driver-modernization.md` §A.1(先读)。参考(思路非逐行):`~/AgentWorks/Code_Opencode/opencode-remote-control/src/core/agent/acp-connect.ts` + `acp-backend.ts`。

## 交付
新包 `internal/acp`:对一个 stdio 子进程说 Agent Client Protocol(JSON-RPC 2.0)。

## 要求
1. `Spawn(ctx, command, args, env, dir) (*Client, error)` — 起子进程,接管 stdin/stdout;JSON-RPC 帧按行(LSP 风格 Content-Length 不用;ACP 用 newline-delimited JSON)。request/response 用 id→chan 关联;notification 按 method 分发到注册的 handler。进程退出 → 所有 pending 收 error,Client 置 dead(后续调用即错)。
2. 方法:`Initialize`(能力协商,保存 server capabilities)、`NewSession(cwd)`、`LoadSession(id)`(server 声明支持才可调,否则返回 ErrNotSupported)、`Prompt(sid, text)`(阻塞到回合终止,返回 StopReason)、`Close`。
3. 通知回调:`OnSessionUpdate(fn)`(agent 消息/工具调用/plan 更新,含 usage 字段时解析出 `Usage{InputTokens,OutputTokens,Cost}`)、`OnPermissionRequest(fn) PermissionOutcome`(这是 server→client 的**请求**,要回 response)。
4. 类型最小化:只定义需要的字段,未知部分 `json.RawMessage` 兜底。宽进严出。
5. 并发安全:writer 单 goroutine(chan 序列化);reader 单 goroutine 分发。

## 测试(hermetic,不依赖任何真 vendor CLI)
包内 `fakeserver_test.go`:用 `os/exec` 起 `go run` 的内嵌假 server(或 io.Pipe 直连注入——二选一,pipe 更快)。覆盖:握手、prompt 完整回合、update 流、permission 请求往返、进程死亡时 pending 全部收错、malformed JSON 帧跳过不崩。
`go test ./internal/acp/` 绿;`go vet` 干净。

## 边界
不做 orchestrate 集成(那是 dm-acp-runner);不做真 vendor 调用。
