# Task cockpit-serve (D2-2) — serve 的 cockpit HTTP 端点 + Manager 接线

## 目标(orchestrator-cockpit-spec E1a:HTTP 面)
把 `cockpit.Manager` 接进 serve,加 5 个端点(全走已有 SEC-1 writeGuard——注册进 Handler() 的
mux 即自动继承)。后端按座席 kind 选(claude-code→claudeSdkBridge、codex-cli→codexAppServer;
其它 kind 报错"非深度集成档")。Manager 可注入以便 httptest 用 FakeBackend。

## 改文件
- 新增 `internal/serve/cockpit.go`(registerCockpitRoutes + 5 handler + 后端 factory)
- 改 `internal/serve/api.go`(Server 加 `cockpit *cockpit.Manager` 字段 + 在 Handler() 里
  `s.registerCockpitRoutes(mux)`;lazy init)
- 新增 `internal/serve/cockpit_test.go`(httptest + 注入 FakeBackend Manager)

## 端点(全部 /api/projects/{id}/cockpit/...)
1. `POST .../cockpit/prompt` body `{"seat":"...","text":"..."}` → get-or-create session → `Prompt(text)` → 200 `{"ok":true,"threadId":...}`。
2. `GET .../cockpit/stream?seat=<seat>` → SSE(`text/event-stream`):先 replay `History()` 每条 Event(一行 `data: <json>\n\n`),再 `Subscribe()` 实时推;ctx.Done 时 Unsubscribe。用现有 SSE 写法(参考 handleAgentStream/sse.go 的 Flusher 循环)。
3. `POST .../cockpit/permission` body `{"seat":"...","approvalId":"ap1","decision":"allow|deny|allow_for_session"}` → `session.Respond(approvalId, Decision)` → 200/4xx(未知 id 400)。
4. `POST .../cockpit/cancel` body `{"seat":"..."}` → `session.Interrupt(ctx)` → 200。
5. `GET .../cockpit/status?seat=<seat>` → `{"threadId":...,"pending":[{id,kind,toolName}...]}`(session 不存在则 pending 空、threadId "")。

## Manager 接线(api.go + cockpit.go)
- Server 加字段 `cockpit *cockpit.Manager`。加方法 `ensureCockpit()`:若 nil,则
  `s.cockpit = cockpit.NewManager(baseDirForCockpit, s.backendForKey)`。baseDir 用一个进程级
  临时/状态目录(如 `os.TempDir()/pactify-cockpit` 或 serve 的工作目录下 `.pact`;简单起见用
  `filepath.Join(os.TempDir(), "pactify-cockpit")`,MkdirAll)。
- `backendForKey(key cockpit.SessionKey) (cockpit.Backend, error)`:
  - repoDir = s.projects[key.Project].Path;项目未知 → error。
  - kind = 解析该座席 kind(见下 seatKind);
  - map:`claude-code` → `cockpit.NewClaudeBackend()`;`codex-cli` → `cockpit.NewCodexBackend()`;
    其它/空 → error `fmt.Errorf("seat %q kind %q is not deep-integration (claude-code/codex-cli only)", seat, kind)`。
- `seatKind(project, seat) string`:折叠该项目 `.pact/log.jsonl`(用 `projection.Project(event.ReadAll(...))`)
  找 `st.Agents` 里 id==seat 的 `.Kind`;读不到返回 ""。(serve 里应已有折叠 state 的辅助;有就复用,
  没有就按此实现,repoDir=s.projects[project].Path,paths.LogIn(repoDir)。)
- handler 里 opts = `cockpit.StartOpts{RepoDir: s.projects[id].Path, Seat: seat}`。

## 测试可注入
- 让测试能设置 `s.cockpit`(直接给字段赋一个用 FakeBackend factory 建的 Manager)。因为同包,
  `cockpit_test.go` 可直接 `srv.cockpit = cockpit.NewManager(t.TempDir(), func(k) (cockpit.Backend,error){ return cockpit.NewFakeBackend(), nil })`。
  ensureCockpit 里 `if s.cockpit != nil { return }` 保证不覆盖注入的。

## 测试(cockpit_test.go,httptest + 注入 FakeBackend Manager)
- New(projects=[{Name:"p",Path:t.TempDir()}]);注入 fake Manager;`h := srv.Handler()`。
- POST prompt {seat:"claude",text:"hi"} → 200;之后该 session 的底层 FakeSession.Prompts 含 "hi"
  (可通过 srv.cockpit.Get(key) 拿到 CockpitSession 再... 或简单断言 200 + status 端点)。
- 先 POST prompt 建会话 → 用 FakeSession.Emit/EmitApproval 驱动 → GET status 显示 pending;
  POST permission 应答 → status pending 减少。
- GET stream:建会话 + Emit 一个 Event → SSE 响应体含该 event 的 data 行(用 httptest + 读一段 body)。
- 未知项目 → prompt 404/400;非深度集成 kind → 相应 error(可另测 backendForKey 直接)。
- 注意 SEC-1:POST 需带 writeGuard 允许的头(参考现有 POST 测试怎么过 writeGuard,如 same-origin/无 Origin)。

## 验收 / Acceptance(视角: correctness — 端点接通 Manager、SSE replay+live、SEC-1 继承、kind 选择正确)
- reviewer 独立跑 verify(含 -race);真机不必(FakeBackend 覆盖)。

## verify
verify: go build ./... && go test -race ./internal/serve/ ./internal/cockpit/
