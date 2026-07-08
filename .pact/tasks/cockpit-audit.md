# Task cockpit-audit (E1b 部分) — cockpit 工具/审批直写 audit + 危险分级

## 目标(orchestrator-cockpit-spec §4.3)
cockpit 会话的**工具调用**与**审批决策**直接写进 pactify 的 audit 管道(不依赖 vendor hook),
带**危险分级**(risk)。这样 cockpit 里 agent 的每次工具动作/每个审批都有审计行。纯加性。

## 改文件
- `internal/cockpit/session.go`(CockpitSession 加可选 audit sink;eventPump 的 tool 事件 + Respond 的审批决策调 sink)
- `internal/cockpit/manager.go`(Session 注入一个绑定 (project,seat) 的 sink)
- `internal/cockpit/audit_grade.go`(新增:危险分级 gradeRisk)
- `internal/serve/cockpit.go`(Manager 用真实 audit.Append 作 sink 后端;需要 project→repo 已有)
- 各自 `_test.go`

## 契约
### session.go
- `AuditEvent` 结构(cockpit 内部,喂给 sink):
  `type AuditEvent struct { Tool string; Summary string; Risk string; Decision string }`
- CockpitSession 加字段 `audit func(AuditEvent)`(可空)。`NewCockpitSession` 保持现签名不变;
  新增 `NewCockpitSessionWithAudit(sess, jsonlPath, audit func(AuditEvent)) (*CockpitSession, error)`
  (老构造转调新的传 nil)。audit 为 nil 时全程 no-op(现有行为字节不变)。
- eventPump:当 `e.Kind == EventTool && e.Tool != nil` 且是一次工具"开始"(Phase=="start")时,
  调 `cs.emitAudit(AuditEvent{Tool:e.Tool.Name, Summary:e.Tool.Name+" "+e.Tool.Phase, Risk:gradeRisk(e.Tool.Name, e.Tool.Text)})`
  (Decision 空)。别在每个 output delta 都写(只 start,避免刷屏)。
- 审批 Respond:在 approvalPump 造的 Respond 闭包里,收到 Decision 后调
  `cs.emitAudit(AuditEvent{Tool:<pending.ToolName>, Summary:"approval "+<kind>, Risk:gradeRisk(toolName, string(rawInput)), Decision:string(d)})`。
- `emitAudit`:若 cs.audit != nil 则调它(在锁外,别阻塞 pump;可直接同步调,sink 自身要快)。

### audit_grade.go
- `func gradeRisk(tool, detail string) string`:返回 "read"|"write"|"exec"|"mcp"。
  简单规则(小写匹配 tool + detail):
  - tool 含 bash/shell/exec/command/run 或 detail 含常见危险命令 → "exec"
  - tool 含 write/edit/create/patch/apply/delete/rm/mv → "write"
  - tool 含 mcp → "mcp"
  - 其它(read/grep/glob/ls/fetch 等) → "read"
  (尽力分类,默认 "read"。)

### manager.go
- `NewManagerCtx`/`NewManager` 加一个可选的 audit sink 工厂:
  `type AuditSink func(key SessionKey, ev AuditEvent)`。加 `NewManagerCtxAudit(baseCtx, baseDir, factory, sink AuditSink)`;
  老构造转调传 nil sink。Session 建 CockpitSession 时,若 sink!=nil,用
  `NewCockpitSessionWithAudit(sess, jsonlPath, func(ev){ sink(key, ev) })`;否则老构造。

### serve/cockpit.go
- `ensureCockpit` 用 `NewManagerCtxAudit(..., s.cockpitAudit)`。
- `func (s *Server) cockpitAudit(key cockpit.SessionKey, ev cockpit.AuditEvent)`:
  取 repo=s.projects[key.Project].Path,调 `audit.Append(audit.Record{TS: now RFC3339, Project:key.Project,
  Repo:repo, Seat:key.Seat, Kind:"cockpit", Tool:ev.Tool, Summary:ev.Summary, Risk:ev.Risk, Decision:ev.Decision,
  Session:key.Project+"/"+key.Seat})`。best-effort,忽略错误(别阻塞)。summary 走 audit 已有脱敏? audit.Append
  不脱敏 summary——所以这里 summary 用工具名/短语,别塞原始命令(RawInput 可能含密钥)。**Risk 用 rawInput 分级但 Summary 不含 rawInput。**

## 测试
- session_test:用 FakeSession + 一个记录 sink,Emit 一个 EventTool(Phase start)→ sink 收到一条含 Tool+Risk;
  EmitApproval + Respond(DecisionAllow)→ sink 收到一条含 Decision="allow"。audit 为 nil 时不 panic、无写。
- audit_grade_test:gradeRisk("Bash","rm -rf")→"exec";("Write",...)→"write";("Read",...)→"read";("mcp__x",...)→"mcp"。
- manager_test:注入计数 AuditSink,建 session + Emit tool → sink 被调且 key 正确。
- serve cockpit_test:可选——注入 fake + 断言 cockpitAudit 不 panic(真 audit.Append 落 PACTIFY_HOME tmp 可选)。

## 验收 / Acceptance(视角: security — 工具/审批有审计行、risk 分级、Summary 不泄 rawInput、nil sink 零回归、-race)
- reviewer 独立跑 verify(含 -race)。

## verify
verify: go build ./... && go test -race ./internal/cockpit/ ./internal/serve/
