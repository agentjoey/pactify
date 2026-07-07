# Task validate-dynamic-seats (FU-1) — validate 认可 add-seat / 动态 join 座席

## 背景(确认的 bug)
`internal/pact/rules.go` 的 `validateLog()` 把「合法座席集合 declared」**只**从 `init` 事件的
seats payload 里收集(约第 362-379 行)。但座席也可经 `add-seat`(AddSeat)或**动态 join**
(JoinKind,spec §6 WS-K,座席不在 init 里、只靠 join 进 roster)加入。于是这些座席后续产生的
事件(join/checkpoint/...)会在第 387-388 行被误判 `agent_id %q not in seat roster`,validate 假红。

## 根因与正确修法
`validateLog` 开头已经算了 `st := projection.Project(evs)`。projection 只在
`init`(seatFromPayload)、`add-seat`、`join` 三种事件时把座席加进 `st.Agents`——即 `st.Agents`
**正好**是"合法注册过的 roster",既含 init 也含 add-seat / 动态 join 座席。
把 declared 改成从 `st.Agents` 的 id 构建即可:既修误报,又**保留**对真正未注册 agent_id 的拦截
(从没 init/add-seat/join 过的垃圾 id 不会出现在 st.Agents,仍会被拒)。

## 改文件(仅这两)
- `internal/pact/rules.go`
- `internal/pact/engine_test.go`(加测试;与现有 TestValidate* 同风格)

## 契约(rules.go 的 validateLog)
1. 把构建 `declared` 的来源从「只扫 init 事件 seats」改为「遍历 `st.Agents`,`declared[a.ID]=true`」。
2. **保留** protocol_version 的提取(它来自 init 事件 payload 的 `protocol_version`)——那段逻辑
   独立,别删;可以单独留一个只为取 protocol_version 的 init 扫描,或在同一个循环里取。
3. 其余检查(event_id 非空、agent_id 是 slug、STATE drift、rule1 owner==reviewer、dup task id)全部不动。
4. 报错信息文案不变(仍 `agent_id %q not in seat roster`)。

## 测试(engine_test.go)
- `TestValidateAcceptsDynamicJoinSeat`:Init 两个座席(如 claude-opus + opencode);
  然后 `t.Setenv("PACT_AGENT_ID","dynseat")` + 用动态 join 让 dynseat 只经 join 进 roster
  (用包内可用的 JoinKind/Join 包装;dynseat 不在 init)。`Validate()` 必须返回 nil。
  (改前此用例会因 "dynseat not in seat roster" 失败,改后通过。)
- `TestValidateAcceptsAddedSeat`:Init 后由 orchestrator `AddSeat` 一个新座席,再让该新座席
  产生一个自身事件(join 或被 assign 后 checkpoint,取包内最简路径),`Validate()` 返回 nil。
- `TestValidateStillRejectsUnknownSeat`:构造/篡改 log,塞一条 agent_id 从未 init/add-seat/join
  过的事件(可复用现有篡改 log 文件的手法,如 TestValidateFailsClosedOnHigherProtocolMajor
  那样读改写 .pact/log.jsonl,把某事件 agent_id 换成 "ghost"),`Validate()` 必须仍返回非 nil。
  —— 这条证明修复没削弱 roster 校验的拦截力。

## 验收 / Acceptance(视角: correctness — 修误报同时保留未注册座席拦截)
- reviewer 独立跑 verify 门 + 阅读 diff 确认 declared 来源改对、protocol_version 保留、负例仍拒。

## verify
verify: go build ./... && go test ./internal/pact/
