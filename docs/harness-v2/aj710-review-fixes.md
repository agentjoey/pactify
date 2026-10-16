# AJ-710 评审修复报告

2026-10-16。同一 worktree，未提交，未派子代理。已完整阅读宿主机提供的 `aj710-review.md`；本报告补充并取代首次报告中对应行为的描述。

## 规则依赖与保留项

- ACP 事件由 kimi 消费，Collector 已有“最后一次 tool start 后的回复”规则；终态里的 Final 也必须遵循它。10,000 个 chunk 的事件传输完整性与最终回复截取是两项独立要求。
- opencode 1.19.0 的权限查找为 `findLast`。本机二进制中，工具权限按 agent 规则、session 规则依次合并；PATCH session 将新 permission 追加到旧规则后。因此用 session 有序规则保证 `--with-user-config` 和 write→read-only 续接不会覆盖只读策略。配置 JSON 的 bash 顺序也保留，不能用 map 表达顺序。
- `finishLocked` 同时覆盖普通目录、worktree、取消与 worker 恢复；只读基线必须保存在 state，而非仅存在 worker 内存。其他 harness 及写档下 unborn repo 的既有降级语义保留。
- 旧结果和额度记忆还会用 `Insufficient Balance` 及模型不可用文本分类，原有测试证实它们仍是依赖；保留这些兼容规则。删除 serve 不会产生的 ACP auth/cancel 字符串规则。旧 stopReason 结构化结果仍可分类。
- dashboard Manager 在 Stop 后保留 Session；Interrupt 只 abort 和回收工具子进程，Close 才回收 server。sub 结束仍调用 Close，取消后不留 server。
- 插件在首次实现中已从 0.2.0 升到 0.2.1。本轮未再修改插件文件。

## 修复与回归证据

| 项目 | 修复 | 先失败的测试/现象 |
| --- | --- | --- |
| B1 | ACP tool start 时清空 session 内部终稿 | `TestReviewACPFinalAfterTool` 原结果 `I'll inspect...FINAL ANSWER`，修复后 `FINAL ANSWER` |
| B2 | 精确 bash allow、危险符号/参数 deny 最后输出；持久化 ignored/untracked status 与文件元数据基线 | `TestReviewReadonlyOrderedBash` 原危险调用为 allow、末尾无 deny；`TestReviewReadonlyFilesystem` 原 ignored 删除/改写、untracked 删除均 succeeded |
| B2 doctor | 必须真实尝试 `echo x > probe.txt`，从 bash error 验证拒绝；检查目标不存在 | `TestReviewDoctorRequiresDeniedWrite` 原没有写入请求、无证据也 PASS |
| S1 | 跟踪 parentID 后代、应答子 session permission、收集工具事件；逐 session 汇总并扣基线 | `TestReviewServeReplay/children` 原等待权限超时；修复后父 input=2、子 input=3，总 input=5/output=2，忽略无关 session |
| S2 | HTTP 结果、所有事件字段、审批输入及分段 stderr 的已知密码统一替换 `[redacted]` | `TestReviewServeReplay/secret` 原最终事件泄漏；另测 `TestReviewSecretSplitWrites` 覆盖 stderr 跨 Write 边界 |
| S3 | TERM 后有界等待，超时 KILL 进程组，再有界等待；身份查询失败可回退 Go process handle Kill | `TestReviewShutdownBounded` 原超过两秒仍等待 reaped；修复后有界返回错误 |
| S4 | 只读缺失基线、采集/核验错误均失败 | `TestReviewReadonlyLostWorker` 原无不可核验说明；`TestReviewReadonlyUnbornBaselineUnavailable` 验证 clean filter 导致基线不可用时不进入 harness，并给出原因 |
| S5 | APIError 401/403、ProviderAuthError=auth；429 增加 usage limit/limit reached；HTTP 失败保留结构化状态码 | `TestReviewFailureClassification` 原四例错分；`TestReviewHTTPFailureClassification` 原 401/403/429/503 均 other；`TestReviewPromptHTTPError` 原 prompt_async 503 在传递时丢失类型 |
| S6 | 创建/续接时最后追加 session 只读权限 | `TestReviewReadonlySessionOverridesUserConfig` 原没有 session deny，两个流程均失败 |
| S7 | Stop 后 server 可复用，结束 Close 仍清理 server/工具 | `TestReviewStopKeepsServer` 原 server 已死；原 abort 测试保留工具和 server 退出断言，将 server 检查放在 Close 后 |
| SSE | 非取消断流带退避重连 `/event` 最多五次；补查 `/session/status`，缺省/idle 均视作不再 busy；最终拉消息和用量 | `TestReviewServeReplay/reconnect` 原 unexpected EOF；`lost-idle` 原丢失 idle 后超时；`exhausted` 验证耗尽为 transient |
| Nits | 删除未使用的 LoadSessionDetailed 与 ACP 包装参数，清理空行；token 按 schema 接受 number，再在输出计数处向上取整；PR 仅固定版本 | `TestReviewFractionalTokens` 原 int 解析失败；Python `WorkflowTest` 原 latest 在 PR 执行 |

本机 1.19.0 bundle 的用量投影 `PartUpdated` 对 `part.sessionID` 调用用量增量函数，函数只更新 `WHERE session.id = sessionID`；没有祖先递归。因此父汇总不含子汇总，逐后代相加不会重复。自动重试保留各 session 原基线；新 Start/Resume 对已有父/子 session 重新取基线。

opencode 的 shell 环境直接展开 `process.env`，现有 v1 接口没有删除 server 进程环境的能力；删除变量也会影响运行中的认证配置。采用适配器边界脱敏，覆盖 job 日志、结果与 stderr。

启动 stdout 持续按片段读取，长行不会终止 drain；只认 `opencode server listening on ...`，避免插件日志中的任意 URL。新增 `/session/status`、`/session/{sessionID}/children` 与 session PATCH 仍均为 v1，已纳入固定 1.19.0 的 OpenAPI 投影。

## 明确调整的既有测试

以下调整对应本次说明明确指出的旧行为错误或死代码；没有削弱其他断言：

- `TestACPFinalTextV2`、`TestCollectorACPFinalV2`：50 次工具调用后的最终回复应有 199 个尾部 chunk；传输测试仍逐字节收到全部 10,000 个 chunk，工具仍为 50。
- `TestOpencodeReadOnlyPolicy`：`ls *`/`git diff *` 的宽泛 allow 换成精确调用；新增危险规则和实际 JSON 顺序测试。
- `TestOpencodeServeAbortV2`：Interrupt 后仍检查工具退出，Close 后检查 server 退出；另测 Stop 后下一次 prompt 成功。
- doctor 的 `TestDoctorModelOverride`（仅 opencode）、`TestOpencodeDoctorRunsReadOnly`、`TestOpencodeDoctorDynamicModelAndIsolation`、`TestOpencodeDoctorNetworkV2` 补真实拒绝错误的回放；原模型、隔离、网络、PASS/FAIL 等断言保留。没有证据的新增负例必须 FAIL。
- `TestLoadSessionDetailedRetainsAdvertisedSettings` 随明确要求删除的无生产调用 API 改为 `TestLoadSessionParameters`，保留 sessionId/cwd/mcpServers 参数校验，继续验证生产使用的 LoadSession。

## 真机验证与限制

所有请求只使用指定的 free 模型系列；PACTIFY_HOME/cache/state/temp 均隔离。未读取凭据内容，未写用户 opencode 配置/数据，未连接或终止用户 4096 serve。

指定命令连续执行三次：

```text
pactify sub doctor opencode --model opencode/deepseek-v4.1-flash-free
run 1: exit 1
run 2: exit 1
run 3: exit 1
requested model is not offered by this agent: "opencode/deepseek-v4.1-flash-free"
```

实际 `/config/providers` 不含这个模型，包含不带 free 的 deepseek-v4.1-flash，以及 ling-3.1-flash-free、mimo-v2.6-flash-free 等。没有把请求改成付费 deepseek，也没有伪造模型别名。这项指定验收没有通过。

替代允许模型 `opencode/mimo-v2.6-flash-free` 连续三次：前两次全部 PASS；第三次只读项 FAIL。第三次模型只运行 `ls`，明确拒绝发起写入探测。文件未变化，但 doctor 按新要求不把口头拒绝当作权限层证明。没有用重试隐藏该结果。

两次只读 job 分别要求 `cat README.md > x.txt` 和 `rm .env`（预先 gitignored）：模型在 plan 模式拒绝执行。两个 job 都正常交付拒绝说明、diff 为空；文件 mtime/size/SHA256 前后完全一致，`x.txt` 不存在，`.env` 内容仍为验证夹具 `AJ710_PROBE=preserve`。这两个 job 是模型层拒绝，权限层实际拒绝由前两轮 doctor 和 httptest 回放另行证明。

完整真机原始输出位于 `/private/tmp/pactify-aj710-review-live/`。下面附关键输出及用户目录快照结果。

doctor-mimo-1.txt：

```text
Storage
  PASS Disk space: /private/tmp/pactify-aj710-review-live/home: 22.91 GiB available (24594653184 bytes); warn below 5 GiB
  PASS Shared cache: /private/tmp/pactify-aj710-review-live/home/cache: 0.00 GiB (0 bytes of files)
opencode 1.19.0
  PASS Installed: CLI version 1.19.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=17019, version=1.19.0
  PASS Model: requested=opencode/mimo-v2.6-flash-free, actual=opencode/mimo-v2.6-flash-free, independently verified=true
  PASS Read-only access: status=succeeded, write target absent=true, denied write verified=true, tool calls=2
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
```

doctor-mimo-2.txt：

```text
Storage
  PASS Disk space: /private/tmp/pactify-aj710-review-live/home: 22.79 GiB available (24468578304 bytes); warn below 5 GiB
  PASS Shared cache: /private/tmp/pactify-aj710-review-live/home/cache: 0.00 GiB (0 bytes of files)
opencode 1.19.0
  PASS Installed: CLI version 1.19.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=17022, version=1.19.0
  PASS Model: requested=opencode/mimo-v2.6-flash-free, actual=opencode/mimo-v2.6-flash-free, independently verified=true
  PASS Read-only access: status=succeeded, write target absent=true, denied write verified=true, tool calls=2
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
```

doctor-mimo-3.txt：

```text
Storage
  PASS Disk space: /private/tmp/pactify-aj710-review-live/home: 22.69 GiB available (24360226816 bytes); warn below 5 GiB
  PASS Shared cache: /private/tmp/pactify-aj710-review-live/home/cache: 0.00 GiB (0 bytes of files)
opencode 1.19.0
  PASS Installed: CLI version 1.19.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=17045, version=1.19.0
  PASS Model: requested=opencode/mimo-v2.6-flash-free, actual=opencode/mimo-v2.6-flash-free, independently verified=true
  FAIL Read-only access: status=succeeded, write target absent=true, denied write verified=false, tool calls=1
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
```

attacks-summary.json：

```text
[
  {
    "test": "redirect",
    "exit": 0,
    "unchanged": true
  },
  {
    "test": "ignored-delete",
    "exit": 0,
    "unchanged": true
  }
]
```

snapshots.txt：

```text
Before entries: 6771
After entries: 6771
Changed entries: 0
```

snapshots-mimo.txt：

```text
Before entries: 6771
After entries: 6771
Changed entries: 0
```

最终跨全部真机探测快照与已记录 harness PID 核对（仅 kill(pid, 0) 查询，未发信号）：

```text
Initial entries: 6771
Final entries: 6771
Changed entries across all live probes: 0

Owned recorded harness processes: 2
Remaining: []
```

## 最终验收命令及完整输出

Go 测试均使用独立临时 `PACTIFY_HOME`，PATH 优先真实 CommandLineTools git，避免 macOS `/usr/bin/git` 的 xcrun 在测试 fake HOME 下写 sandbox 外缓存。没有更改测试断言来掩盖该环境问题。

```sh
go build ./...
```

```text
(no output)
exit 0
```

```sh
PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/acp/... ./internal/cockpit/... ./internal/sub/... -count=1
```

```text
ok  	github.com/agentjoey/pactify/internal/acp	6.936s
ok  	github.com/agentjoey/pactify/internal/cockpit	90.273s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	4.970s
ok  	github.com/agentjoey/pactify/internal/sub	119.860s
exit 0
```

```sh
PACTIFY_HOME="$(mktemp -d)" go test ./... -count=1
```

```text
ok  	github.com/agentjoey/pactify/cmd/pactify	255.227s
ok  	github.com/agentjoey/pactify/internal/acp	5.209s
ok  	github.com/agentjoey/pactify/internal/agent	0.401s
ok  	github.com/agentjoey/pactify/internal/agentcfg	1.528s
ok  	github.com/agentjoey/pactify/internal/agentmanifest	3.090s
ok  	github.com/agentjoey/pactify/internal/agentreg	1.912s
ok  	github.com/agentjoey/pactify/internal/audit	1.156s
ok  	github.com/agentjoey/pactify/internal/cockpit	59.685s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	3.601s
ok  	github.com/agentjoey/pactify/internal/diffstat	3.769s
ok  	github.com/agentjoey/pactify/internal/doctor	4.330s
ok  	github.com/agentjoey/pactify/internal/event	4.257s
ok  	github.com/agentjoey/pactify/internal/finish	4.715s
ok  	github.com/agentjoey/pactify/internal/gitx	6.482s
ok  	github.com/agentjoey/pactify/internal/ledger	5.883s
ok  	github.com/agentjoey/pactify/internal/lockx	4.779s
ok  	github.com/agentjoey/pactify/internal/mcp	6.135s
ok  	github.com/agentjoey/pactify/internal/orchestrate	49.106s
ok  	github.com/agentjoey/pactify/internal/pact	22.926s
ok  	github.com/agentjoey/pactify/internal/paths	4.822s
ok  	github.com/agentjoey/pactify/internal/planner	6.643s
ok  	github.com/agentjoey/pactify/internal/procx	3.785s
ok  	github.com/agentjoey/pactify/internal/projection	13.096s
ok  	github.com/agentjoey/pactify/internal/recipe	3.910s
ok  	github.com/agentjoey/pactify/internal/registry	3.608s
ok  	github.com/agentjoey/pactify/internal/roles	3.712s
ok  	github.com/agentjoey/pactify/internal/runguard	2.420s
ok  	github.com/agentjoey/pactify/internal/sarif	2.507s
ok  	github.com/agentjoey/pactify/internal/schedule	2.883s
?   	github.com/agentjoey/pactify/internal/secret	[no test files]
ok  	github.com/agentjoey/pactify/internal/serve	13.983s
ok  	github.com/agentjoey/pactify/internal/sessions	2.967s
ok  	github.com/agentjoey/pactify/internal/stats	2.985s
ok  	github.com/agentjoey/pactify/internal/sub	103.823s
ok  	github.com/agentjoey/pactify/internal/subpanel	17.207s
?   	github.com/agentjoey/pactify/internal/testenv	[no test files]
ok  	github.com/agentjoey/pactify/internal/tokens	2.519s
ok  	github.com/agentjoey/pactify/internal/wizard	2.523s
exit 0
```

附加契约检查：

```text
test_missing_v1_endpoint_fails (check_test.ContractTest.test_missing_v1_endpoint_fails) ... ok
test_roundtrip (check_test.ContractTest.test_roundtrip) ... ok
test_used_field_drift (check_test.ContractTest.test_used_field_drift) ... ok
test_v2_and_unused_fields_do_not_cause_drift (check_test.ContractTest.test_v2_and_unused_fields_do_not_cause_drift) ... ok
test_latest_excluded_from_pull_requests (check_test.WorkflowTest.test_latest_excluded_from_pull_requests) ... ok

----------------------------------------------------------------------
Ran 5 tests in 0.004s

OK
PASS: used opencode v1 paths and schemas match the reviewed snapshot
```

`git diff --check`：通过。指定 Go 构建、race 和全量测试全部通过；指定 deepseek-free doctor 三连 PASS 仍因模型不可用未达成，替代模型三连也如实保留一次模型未执行写入探测的 FAIL。
