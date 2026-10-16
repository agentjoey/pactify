# AJ-712 — kimi P0，独立评审修订

2026-10-17 · kimi 2.2.0 · 当前 worktree，未提交，未派子代理。

按 Owner 决定撤回隔离只读，kimi 继续不提供只读；只读不作为转正门槛。保留 wire 失败、子 agent 用量、ACP 模型发现和 Network doctor。插件版本为 **0.2.4**（main 为 0.2.3）。本轮新增真机 prompt **0** 次，沿用宿主机已经完成的真机验证；自动测试使用 fake ACP 和隔离 home。

## 改动前核对的依赖与撤回范围

已读 `docs/specs/harness-v2.md` §2、§3.3，`docs/harness-v2/harness-review.md` 的 G1/G4/G8/G9/G13，宿主机 `aj712-review.md` 和两份 `zz_*_probe_test.go`。

- `NoReadOnly` 由 CLI、`actions.go`、`worktree.go` 续接、router、catalog 和 doctor 消费。恢复 kimi 的声明；worker 也在启动后端前拒绝排队或旧 state 中的只读请求。K1 CLI 测试及 plugin bats 的不提供只读断言恢复。
- 删除 `kimi_readonly.go`、`Harness.ReadOnlyIsolation`、`State.ReadOnlyWorktree` 及 worker/doctor/lifecycle/router 配套和隔离测试。`job.go`、`lifecycle.go`、`readonly.go`、`router.go` 恢复为 HEAD，`worktree.go` 没有改动。AJ-710/AJ-711 的 tracked/untracked/ignored、fail-closed 基线和 opencode/agy 原有只读路径保持原样。
- 隔离方案无法保护 Git common dir 的 refs/config/hooks，也无法阻止绝对 symlink、绝对路径、继承 PWD 或续接历史访问原目录；另有全量复制、特殊文件和清理成本。B1/B2/S1–S4 通过撤回该能力解决，不再宣称它能强制只读。
- doctor 的 kimi Read-only 恢复 **SKIP**；Network 仍验证脚本生成的 bind/HTTPS 证据。`sandboxed=false` 保留。用户文案明确：`kimi does not offer read-only access: it cannot enforce it. Use codex, opencode or agy with --read-only.`
- 原标量 wire API 仍供旧调用方使用；worker 续接发送 prompt 前重新采集所有 agent 的 offset map。删除只写不读的 `State.KimiWireOffsets`；旧 state 无该字段仍可续接，多余旧 JSON 字段被忽略，不依赖持久化 offset。
- AJ-693 续接和 R3 回退仍消费既有 `FailureClass`；没有新增一套回退流程。AJ-704 的十分钟缓存、刷新、失败保留旧列表继续适用。全局 catalog 锁只保护内存映射，同一 store/harness 独立串行，不再在全局锁内运行外部进程。

## S5：用量与 main 完成条件

评审的 `zz_usage_probe_test.go` 已改造成断言型回归：main 已结束但还没有 usage.record，只有 agent-0 的用量时必须 partial；追加 main 用量后才完整，tokens 为两者之和。`KimiRecord.Turns` 现在只计 main 的 usage 记录；模型、prompt.completed、turn.ended 本来就只认 main。

核对本机 2.2.0 二进制内嵌源码，未读取凭据。二进制 SHA256：

```text
dab42ead275f8a49e9ce30a6263f614fce5926455bed103d0d16940655619bf5
```

证据链：

1. `llmRequester` 在请求成功后调用 `this.usage.record(this.scopeContext.agentContext, request.modelAlias, usage ?? emptyUsage(), request.source)`。
2. `packages/agent-core-v2/src/session/usage/sessionUsageService.ts` 的 `SessionUsageService.record` 只调用当前 `agentSpaceOf(agent)` 的 `UsageAgentModel.record`；没有向 parent/main 再记一次。通知中保留原 agent。
3. `packages/agent-core-v2/src/features/usage/usageAgentModel.ts` 的 `UsageAgentModel.record` 发出 `UsageRecord({agentId: this.agent.agentId, model, usage, usageScope})`，仅更新本 agent 的 byModel/currentTurn。请求 source 为 turn 时 scope 才是 turn。

**结论：main 的 wire usage.record 没有汇总子 agent，继续累加所有 agents/*/wire.jsonl 的 turn scope 用量是正确的；step scope 不再累加。** fixture 覆盖 main+agent-0、各 agent 的续接增量和续接后新建 agent。子 agent 不能作为 main 刷盘完成的证据。基线读取失败直接返回 partial 和 warning，不再每次等满十秒或重算历史用量。

## S6/S7：失败分类与保留证据

上轮在隔离 PACTIFY_HOME、受管 KIMI_CODE_HOME 下，用自行生成的 provider 配置指向 `127.0.0.1:1` 制造真实失败，没有修改用户 config 或使用真实 API key。实际 wire 已保存为 `internal/cockpit/testdata/kimi/connection-error.jsonl`：

```json
{"type":"turn.ended","agentId":"main","turnId":0,"reason":"failed","error":{"code":"provider.connection_error","message":"Connection error.","name":"APIConnectionError","retryable":true},"durationMs":16,"time":1792173048719}
```

该真实故障结果为 `failed / transient`（job `20261017-015229-1402e2`）；原实现误报 succeeded。当前 fixture 再验证解析的 reason/code/message 和分类。fake kimi 覆盖 ACP `end_turn` 但 wire failed，以及一次 transient 续接、quota 回退。

证据优先级：**ACP 明确 stopReason/鉴权 RPC code → wire typed code/HTTP status → 文本兜底**。ACP 泛化错误和 end_turn 不能否定 wire 的真实失败；明确的 ACP auth/refusal/max_tokens 等不会被 wire transient 覆盖。`Collector.FailureRaw`、`ErrorRaw` 保留 ACP 数据，`WireRaw` 独立保存 wire 证据，日志新增 `kimi_wire_failure` 记录；重试时只重置当前轮数据，前轮日志仍在。测试同时断言分类和两份原始日志证据。

- connection_error、overloaded、timeout、明确 HTTP 5xx 为 transient，先于文本 quota 判断。删除单独 exhausted；额度耗尽必须有 quota/balance/credit/usage limit 等明确语义；rate_limit/429 的短时限流为 transient。额度错误可从消息提取未来的 RFC3339 reset 时间。
- 5xx 仅匹配 HTTP、status、code 等上下文，以及标准 `503 Service Unavailable` 短语；数字 512 不再触发 transient。
- auth 使用完整词或短语，包含 unauthorized、authentication、auth_error、401/403；AUTHORS 不再触发鉴权失败。context overflow 为 other；filtered/content_filter 为 refusal。
- ACP prompt 返回泛化非 transient 错误、没有终止事件时，也先读取 wire，再决定续接；不再先 interrupt 而跳过真实错误。
- 反例包括 `Max retries exhausted: Connection error.`、`max_tokens <= 512`、`AUTHORS`、裸 resource exhausted、503 Service Unavailable、429 RESOURCE_EXHAUSTED RPM，以及 quota 文本伴随 timeout。fixture 覆盖 quota/transient/auth/refusal/context/HTTP 等类别。

## 模型发现及其余 nits

发现只执行 ACP initialize/session/new，读取 id=model 的 configOptions 和选项，不发送 prompt；关闭进程、移除一次性 home，不向用户可见 session_index 留会话。离线仅解析 config.toml 模型 allowlist，附英文 warning。fake ACP 核对完整方法序列、默认模型、缓存命中和无 prompt。

删除只被测试使用的 `discoverHarnessModels` 包装，保留并直接测试 `discoverHarnessModelsIn`。模型发现 stderr 写入 io.Discard；临时目录清理失败保留成功取得的列表并附 warning，关闭进程的异常也不丢弃已获取列表。专门的 fake ACP 测试复现并覆盖 cleanup/stderr 两项。慢 harness 的发现不再阻塞另一 harness 的 catalog。移除当前运行提示及插件中的旧只读收敛文案。

## 测试迁移与先失败证据

新增 `internal/sub/kimi_review_test.go`、`internal/cockpit/kimi_review_test.go`。修复前运行 TestKimiReview，确实失败：child 提前完成、基线等待、只读暴露 workspace、全局锁阻塞、ACP auth/refusal/max_tokens 被覆盖、错误提前 interrupt，以及 exhausted/512/AUTHORS 等反例。模型发现测试随后单独复现“成功列表被 cleanup error 否定”和 stderr sentinel 泄漏，再修复；最终聚焦测试均通过。

获本说明明确授权迁移的旧断言：

- 恢复 `TestSubRunKimiReadOnlyRefused` 和 plugin bats 的 K1 不提供只读断言；恢复 `TestDoctorChecksAndIsolation` 的 kimi SKIP 与一次写探测计数，删除上一版隔离测试。
- `cmd_sub_default_test.go`、`experimental_test.go`、`review_test.go` 以及 plugin bats 的提示内容断言，仅将废弃短语改为 `read-only access is not offered` / `not offered`，保留原状态及错误保留断言。
- `kimi_models_test.go` 仅迁移已删除的包装函数调用；doctor 两处 fake 补充新增 Network 所需 JSON 证据，没有削弱断言。其他既有测试未为适配新行为放宽要求。

## 真机证据与用户目录

用户提供的宿主机结果：写任务通过；续接通过并记得 alpha；取消后无残留；doctor 连续两次全部 PASS。用户 ~/.kimi-code 元数据比对只有 credentials/kimi-code.json 变化，是受管 symlink 刷新 OAuth token 的既有预期行为。此处明确标为宿主机提供的结果，未声称本轮又执行过这些调用。本轮不再发真机 prompt，不读取凭据，不操作 p0eval 或其他评估进程/目录。上轮 sandbox 中的 OAuth EPERM 不再作为宿主机验收缺口。

只读验收按 Owner 决定改为入口拒绝和 doctor SKIP，不再要求实际只读写入探测。宿主机此前的 doctor 结果来自撤回前版本；撤回后的 SKIP/Network 行为由自动回归验证。

## 最终验收

本机 /usr/bin/git 为 Apple shim，在 fake HOME 下产生含随机文件名的 xcrun EPERM，导致既有 idle 指纹测试不能稳定；仅验收命令 PATH 优先使用 Xcode 的实际 git，未修改 gitx 或测试。Darwin 默认 mktemp 目录不在 sandbox 可写范围，因此明确使用 `/private/tmp/aj712r.XXXXXX` 模板，每次新建隔离 PACTIFY_HOME。启动时 git pull 因 detached HEAD 失败，.agent/CURRENT.md 不存在；未切分支或提交。

以下为最终修订版本的验收命令和实际输出。

### go build ./...

```sh
PATH="/Applications/Xcode.app/Contents/Developer/usr/bin:$PATH" go build ./...
```

Exit 0。

stdout/stderr 为空。

### 指定包 race

```sh
PATH="/Applications/Xcode.app/Contents/Developer/usr/bin:$PATH" PACTIFY_HOME="$(mktemp -d /private/tmp/aj712r.XXXXXX)" go test -race ./internal/acp/... ./internal/cockpit/... ./internal/sub/... -count=1
```

Exit 0。

```text
ok  	github.com/agentjoey/pactify/internal/acp	6.931s
ok  	github.com/agentjoey/pactify/internal/cockpit	98.407s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	5.799s
ok  	github.com/agentjoey/pactify/internal/sub	149.666s
```

### Go 全量

```sh
PATH="/Applications/Xcode.app/Contents/Developer/usr/bin:$PATH" PACTIFY_HOME="$(mktemp -d /private/tmp/aj712r.XXXXXX)" go test ./... -count=1
```

Exit 0。

```text
ok  	github.com/agentjoey/pactify/cmd/pactify	264.256s
ok  	github.com/agentjoey/pactify/internal/acp	5.576s
ok  	github.com/agentjoey/pactify/internal/agent	2.326s
ok  	github.com/agentjoey/pactify/internal/agentcfg	1.506s
ok  	github.com/agentjoey/pactify/internal/agentmanifest	0.743s
ok  	github.com/agentjoey/pactify/internal/agentreg	0.378s
ok  	github.com/agentjoey/pactify/internal/audit	2.720s
ok  	github.com/agentjoey/pactify/internal/cockpit	60.532s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	3.526s
ok  	github.com/agentjoey/pactify/internal/diffstat	3.670s
ok  	github.com/agentjoey/pactify/internal/doctor	4.316s
ok  	github.com/agentjoey/pactify/internal/event	3.828s
ok  	github.com/agentjoey/pactify/internal/finish	4.725s
ok  	github.com/agentjoey/pactify/internal/gitx	5.910s
ok  	github.com/agentjoey/pactify/internal/ledger	5.845s
ok  	github.com/agentjoey/pactify/internal/lockx	4.816s
ok  	github.com/agentjoey/pactify/internal/mcp	6.232s
ok  	github.com/agentjoey/pactify/internal/orchestrate	46.148s
ok  	github.com/agentjoey/pactify/internal/pact	20.964s
ok  	github.com/agentjoey/pactify/internal/paths	4.528s
ok  	github.com/agentjoey/pactify/internal/planner	6.371s
ok  	github.com/agentjoey/pactify/internal/procx	3.328s
ok  	github.com/agentjoey/pactify/internal/projection	14.428s
ok  	github.com/agentjoey/pactify/internal/recipe	3.699s
ok  	github.com/agentjoey/pactify/internal/registry	2.895s
ok  	github.com/agentjoey/pactify/internal/roles	3.174s
ok  	github.com/agentjoey/pactify/internal/runguard	2.813s
ok  	github.com/agentjoey/pactify/internal/sarif	2.892s
ok  	github.com/agentjoey/pactify/internal/schedule	3.188s
?   	github.com/agentjoey/pactify/internal/secret	[no test files]
ok  	github.com/agentjoey/pactify/internal/serve	16.306s
ok  	github.com/agentjoey/pactify/internal/sessions	3.377s
ok  	github.com/agentjoey/pactify/internal/stats	3.235s
ok  	github.com/agentjoey/pactify/internal/sub	115.058s
ok  	github.com/agentjoey/pactify/internal/subpanel	17.860s
?   	github.com/agentjoey/pactify/internal/testenv	[no test files]
ok  	github.com/agentjoey/pactify/internal/tokens	2.757s
ok  	github.com/agentjoey/pactify/internal/wizard	3.137s
```

### plugin bats

```sh
bats tests/plugin_pactify.bats
```

Exit 0。

```text
1..32
ok 1 delegation guides recommend selecting the resume job explicitly
ok 2 marketplace lists the pactify plugin
ok 3 plugin.json is valid and named pactify
ok 4 forwarding agents use haiku and only Bash
ok 5 skills route large tasks and wait on the main thread
ok 6 session-start hook reports a missing pactify without failing the session
ok 7 session-start hook configuration invokes an executable script with a quoted plugin path
ok 8 session-start hook reports a CLI without sub and exits successfully
ok 9 session-start hook silently accepts a CLI with sub
ok 10 kimi skill does not offer read-only (Phase 0 K1)
ok 11 delegate skill is self-contained and uses the default harness
ok 12 kimi skill labels its experimental status and Phase 0 limitations
ok 13 README documents default delegation and experimental kimi
ok 14 skills isolate large tasks and share the review apply discard resume workflow
ok 15 all three skills embed the same complete delegation guide
ok 16 skills provide a complete brief example within 25 lines
ok 17 skills require host validation before apply and give actionable rework instructions
ok 18 forwarders preserve isolation and leave validation and rework decisions to the host
ok 19 agy skill and forwarder preserve the shared guide and explicit permissions
ok 20 agy documents restored read-only access and persistent protocol
ok 21 jobs skill exists with valid frontmatter
ok 22 jobs is read-only and never executes lifecycle commands
ok 23 jobs documents default all and single job invocations
ok 24 jobs bounds list output and preserves summary field semantics
ok 25 all delegation skills share the short title instruction and jobs uses titles without extraction
ok 26 jobs next steps require review and host acceptance before apply
ok 27 jobs details include bounded result logs diff and resume links
ok 28 READMEs and plugin description advertise jobs and all harnesses
ok 29 opencode plugin is experimental and shares the delegation contract
ok 30 agy read-only restored after AJ-683 with explicit write permission
ok 31 opencode read-only uses plan and declares its sandbox boundary (AJ-710)
ok 32 agy documents managed hook opt-out and bumps plugin version (AJ-711 review)
```
