# Task cockpit-codex-backend (A2) — codexAppServer 后端(Go 原生 codex app-server 直连)

## 目标
实现 `internal/cockpit` 的 `Backend`/`Session`(A1 已定义)的 **codex** 后端:Go 原生 spawn
`codex app-server`,走 JSON-RPC 2.0 / stdio,把 codex 的 thread/turn/审批/事件映射到统一信封。
**结构参照 `internal/acp/acp.go`**(现成的 JSON-RPC/stdio 客户端:writeCh + pending map +
readLoop + Spawn(Setpgid/killGroup) + newClient(pipe 版供测试));**协议细节见下方 cheatsheet(真机实测)**。

## 改文件(仅新增)
- `internal/cockpit/codex.go` — codexAppServer 后端 + 内部 JSON-RPC 客户端
- `internal/cockpit/codex_test.go` — pipe-backed fake app-server 单测 + gated 真机 smoke
- `internal/cockpit/codexschema/schema_drift_test.go` — schema 漂移守卫(见 §漂移)
（`internal/cockpit/codexschema/codex_app_server_protocol.schemas.json` 与 `CODEX_VERSION` 已由 orchestrator 放好)

## 协议 cheatsheet(全部真机 codex 0.142.5 实测/schema 提取,照此实现)
JSON-RPC 2.0，**逐行 JSONL**（一帧一行，无 Content-Length 头）。id 用递增 int。
- **initialize**(client→server, req)：params `{"clientInfo":{"name":"pactify","version":"0","title":"pactify"}}`
  → result `{"userAgent","codexHome","platformFamily","platformOs"}`。
- **initialized**(client→server, notification, 无 params)：initialize 成功后立即发。
- **thread/start**(req)：params `{"cwd": <repoDir>}` → result `{"thread":{"id": <threadId>, "sessionId",...}}`。
  **threadId = result.thread.id**。
- **thread/resume**(req)：params `{"threadId": <id>}` → 恢复(默认回放历史与 usage)。用于 Resume()。
- **turn/start**(req)：params `{"threadId": <id>, "input":[{"type":"text","text": <msg>}]}` → 启动一轮。用于 Prompt()。
- **turn/interrupt**(req)：params `{"threadId": <id>}`（或含 turnId，取 schema）。用于 Interrupt()。
- **审批 = server→client 请求**(server 发 req 给我们，我们回 result)。三类 method：
  - `item/commandExecution/requestApproval` params 含 `{threadId,approvalId,turnId,command,cwd,reason,...}`
  - `item/fileChange/requestApproval` params 含 `{threadId,turnId,itemId,grantRoot,reason,...}`
  - `item/permissions/requestApproval`（权限）
  回 result `{"decision": <enum>}`。**decision 枚举(命令/文件同)**：`accept` | `acceptForSession` | `decline` | `cancel`。
  → cockpit.Decision 映射：DecisionAllow→"accept"、DecisionAllowForSession→"acceptForSession"、DecisionDeny→"decline"。
  把每个审批请求翻成 cockpit.ApprovalRequest{Kind(取 method 尾段:"command"/"file_change"/"permission"),
  ToolName, RawInput(该 req 的完整 params 原文), Respond(收决策→回 JSON-RPC result)}，投进 Approvals()。
  **RawInput 必须是完整 params 原文**(审批卡信任根)。
- **事件通知**(server→client notification) → cockpit.Event 信封映射：
  | notification method | → Event |
  |---|---|
  | `item/agentMessage/delta` | EventMessage{Text:delta, Final:false} |
  | `item/completed`(agentMessage 类) | EventMessage{Final:true}（其它 item 类型见下）|
  | `item/commandExecution/outputDelta` | EventTool{Phase:"output", Text} |
  | `item/started`(command/file) | EventTool{Phase:"start", Name} |
  | `item/completed`(command/file/mcpTool) | EventTool{Phase:"end", Name} |
  | `thread/tokenUsage/updated` | EventUsage{Usage: 取 token 字段} |
  | `turn/started` | EventState{State:"turn_started"} |
  | `turn/completed` | EventState{State:"turn_completed"} + 若含 usage 也可发 EventUsage |
  | `turn/failed`/`error` | EventError{Err: 摘要} |
  | `turn/diff/updated` | EventDiff |
  | `turn/plan/updated` | EventPlan |
  每个 Event.Raw 存原始 params。未列出的 method 忽略(不 panic、不伪造)。字段名以 vendored schema 为准。

## 实现要点
1. spawn：稳定性纪律——固定 `clientInfo.name="pactify"`(OpenAI 合规)、只用稳定面(不传 experimentalApi)。
   进程组 Setpgid + 组杀(照 internal/acp 的 Spawn/killGroup;可复用其模式，别 import 私有符号——各自实现或抽取)。
2. env：codex 沿 `~/.codex/auth.json`，无需注 API key;env 用白名单(PACT_* + 继承 PATH/HOME 等必要;
   剔 PACT_RELAY_TOKEN、PACTIFY_* —— 照 internal/acp filteredEnviron 的 denylist 思路)。
3. 分发器：单 reader goroutine 读 stdout 逐行 → 分三类：有 id+result/error = 响应(喂 pending)；
   有 method+id = server 请求(审批,需回复)；有 method 无 id = 通知(→ Event)。
   **注意 per-session**：cockpit 一个 Session = 一个 codex app-server 进程 + 一个 thread，
   所以本后端一个 Session 独占一个子进程,不存在多 session 串台(那是 ACP 档的问题)。
4. Prompt 串行：turn/start 之间串行(一个 mu 或 writeCh 保证)。
5. Close：幂等,killGroup + close channels(照 A1 fake 的 sync.Once 思路)。

## 测试
- **pipe-backed fake**(照 internal/acp 的 newClient 测试法):测试里用 io.Pipe 造一个假 app-server，
  按 cheatsheet 脚本化回应 initialize/thread.start，推送若干通知 + 一个 commandExecution 审批请求。
  断言：Start 后 ThreadID 正确;通知按映射变成对应 Event(从 Events() 收);审批请求出现在 Approvals()、
  Respond(DecisionAllow) 写回的 JSON-RPC result decision=="accept";Close 幂等。
- **gated 真机 smoke**(`COCKPIT_SMOKE=1` 才跑,否则 t.Skip):真起 `codex app-server`，
  initialize→thread/start→拿到 threadId 断言非空(与 orchestrator 实测一致)。无 COCKPIT_SMOKE 跳过。
- **schema 漂移守卫**(codexschema/schema_drift_test.go):若 PATH 有 codex 则跑
  `codex app-server generate-json-schema --out <tmp>`，把 `codex_app_server_protocol.schemas.json`
  与 vendored 的比对，**不一致则 t.Fatalf**(提示 codex 版本漂移，需重新 vendored + 复核映射);
  codex 不在 PATH 则 t.Skip。这守住"codex 二进制版本=协议版本"(spec §3.4)。

## 验收 / Acceptance(视角: correctness — JSON-RPC 分发三分支正确、审批 RawInput 完整、映射贴 cheatsheet)
- reviewer 独立跑 verify(含 `go test -race`)+ 真机 COCKPIT_SMOKE=1 smoke 由 reviewer 亲跑。

## verify
verify: go build ./... && go test -race ./internal/cockpit/
