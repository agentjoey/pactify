# Harness v2：kimi、opencode、agy 转正，接入 Muse

> 状态：方向已定（Owner 2026-10-16：「同意按这个顺序做」，顺序为 opencode → agy → kimi，Muse 等 Owner 装好并登录后再排）。验收线以 codex 为标杆（§2）。
> 依据（均为 2026-10-16 的实测或代码审阅，原文见 [`docs/harness-v2/`](../harness-v2/)）：
> - [`harness-review.md`](../harness-v2/harness-review.md)：四个 harness 的接入评审，列出差距 G1–G19；
> - [`probe-report.md`](../harness-v2/probe-report.md)：agy 1.4.0 与 opencode 1.19.0 的探测报告，共 13 项，在隔离 HOME 中运行，只用 flash 模型和 free 模型；
> - [`muse-research.md`](../harness-v2/muse-research.md)：Muse Code 调研。

## 1. 为什么要重做

三家目前标为「实验性」，主要原因不在模型，而在 pactify 的适配层有几类**会被静默吞掉的错误**，评估分数也因此失真：

- **kimi 失败被报成成功**：kimi 2.2.0 的 ACP 把除鉴权错误以外的所有失败轮都映射成 `end_turn`，真正的错误码只写在 `wire.jsonl` 里，pactify 没有去读（G1）。
- **kimi 和 opencode 不重试临时错误，也识别不出额度耗尽**，因此回退链永远不会从这两家触发（G2）。
- **ACP 返回 `refusal`、`max_tokens` 时被当成成功**（G3）。
- **kimi 的 token 统计漏掉子代理**（G4）。
- **背压时丢事件**：ACP 和 codex 在事件缓冲满时会丢弃事件，ACP 的最终文本是靠拼接这些事件得到的，所以可能缺片段（G5）。
- **agy 只读为 0/3 的根因**：plan 模式下连只读命令也被自动拒绝，最后返回一个空答案，状态却是 SUCCESS（探测 A1）。agy 大任务 1/10 中，有 3 题是后端临时错误，而评估时临时错误自动续接（AJ-693）还没有合入。

只读不作为转正门槛：实际使用中只读 job 只有 2 次，都是评估。所以只读的做法是：harness 有原生 plan 模式就用 plan 模式，没有就在 worktree 里跑完再丢弃。

## 2. 标杆：codex 有哪些能力

每个 harness 都按这张表逐项对照。「—」表示上游做不到，需要在结果和文档中注明。

| # | 能力 | codex 的做法 |
|---|---|---|
| C1 | 常驻进程 + 有类型的协议 | `app-server` JSON-RPC，每个 job 一个进程，多轮共用 |
| C2 | 流式事件不丢 | item/turn 事件；终态事件阻塞发送（G5 修完后，非终态事件也不能丢） |
| C3 | 最终文本准确 | turn 完成时拿到 final message |
| C4 | 会话续接 | thread id + resume |
| C5 | 失败分类 | 错误有类型，区分 transient / quota / auth / other |
| C6 | 临时错误自动续接 | AJ-693 |
| C7 | 额度：识别、重置时间、快照 | `rate_limits` 事件、quota.json |
| C8 | 用量精确 | 每轮 tokens，不标 partial |
| C9 | 不计费的模型发现和 effort 列表 | `model/list` |
| C10 | 模型兑现核验 | 服务端回报实际使用的模型 |
| C11 | 取消，并清理进程树 | `turn/interrupt` + 进程组 |
| C12 | 只读 | OS 沙箱 read-only |
| C13 | 写档隔离 | OS 沙箱 workspace-write |
| C14 | 受管配置、与用户配置隔离 | `-c` 覆盖项 + 禁用 MCP |
| C15 | doctor 全项 | 版本、模型、网络、只读 |
| C16 | 协议漂移 CI | `codex-schema.yml` 每周比对 schema |
| C17 | 回放测试 | fake app-server |

## 3. 各 harness 的方案

### 3.1 opencode：从 ACP 改为 `opencode serve`（第一个做）

- **起服务**：每个 job 起一个 `opencode serve --hostname 127.0.0.1 --port 0`，并设随机的 `OPENCODE_SERVER_PASSWORD`（basic auth，用户名为 `opencode`）。不设密码时，本机任何进程都可以调用它（O1）。沿用现有的 XDG 镜像受管目录。数据目录 `XDG_DATA_HOME/opencode/opencode.db` 必须在续接之间保留（O6）。
- **只用 v1 接口**：`/session`、`/session/{id}/message`、`/session/{id}/prompt_async`、`/session/{id}/abort`、`/permission`、`/permission/{id}/reply`、`/event`、`/config/providers`。1.19.0 的 `/api/...`（v2）大多是空壳：prompt 被接受但不执行，`interrupt` 什么都不做，permission 列表为空（O2、O3、O5）。
- **只读**：用 `agent: "plan"`。探测中它能跑 `ls`，不写文件，并给出结论（O2）。
- **写档权限**：通过 `/permission/{id}/reply {"reply":"once"}` 应答（O3）。
- **失败分类**：
  - 消息上 `info.error = APIError{statusCode, isRetryable}`：`isRetryable` 或 5xx 记为 transient；429 加上余额或额度相关文本记为 quota。
  - 模型不存在、配置错误这类问题只出现在 SSE 的 `session.error` 事件里（O4）。
- **用量**：以会话汇总 `GET /session/{id}` 的 `tokens/cost` 为准。同步 `/message` 只返回最后一条消息的用量（O2）。不再事后跑 `opencode export`。
- **模型发现**：`GET /config/providers`。判断是否免费要看 id 是否以 `-free` 结尾，不能看 cost 是否为 0，因为 minimax 订阅模型的 cost 也标成 0（O7）。
- **取消**：`POST /session/{id}/abort`，实测 3 秒内子进程全部退出（O5）。
- **漂移 CI**：每周拉一次 `GET /doc`（OpenAPI），与仓库里的快照比对 v1 路径和关键 schema。
- **写档隔离**：上游没有沙箱，结果中标 `sandboxed=false`。

### 3.2 agy：推倒重来（第二个做）

agy 没有 ACP，也没有可以程序化调用的服务模式（`remote-control` 是远程遥控用的守护进程），所以继续用 stream-json，但交互方式全部重写：

- **一个 job 一个常驻进程**：`--input-format stream-json --output-format stream-json`。每轮写一行 `{"event":"user","message":{"content":…}}`，多轮共用同一个 conversation id（A3）。
  - 写入时必须严格编码：只要有一行格式不对，整个进程就会退出。
  - `result.usage` 在常驻进程里会逐轮累加，所以每轮的用量要按 step 级别的 usage 来算。
- **必须显式传 `--model`**：不传时默认使用 Argon（A5）。
- **自动续接**：遇到后端的 EOF、503、stream 中断时，在同一进程、同一会话里退避后发送「continue」。
- **完成判定**：以下情况判为失败，不算成功：
  - 空答案；
  - 带 `denied_actions` 的空答案；
  - `--print-timeout` 到期（此时退出码为 0、状态为 SUCCESS，但没有内容，A5）。

  另外，agent 以「稍后再看」这类话结束本轮时，在同一进程里追加一轮，让它等结果并报告。agy 本身会等后台命令跑完，最长 30 分钟（A6）。
- **只读**：`--mode plan`，并在受管 `settings.json` 里放只读命令白名单，例如 `"permissions":{"allow":["command(git log)",…]}`，实测可以给出正确结论（A1）。
- **策略层**：在受管目录的 `~/.gemini/config/hooks.json` 里配置：
  - `PreToolUse` 的 deny 不会结束本轮，可用于拦截危险操作；
  - `Stop` hook 返回 `{"decision":"continue"}` 可以让它继续干活（A2）。
- **不用 `--sandbox`**：它不拦网络和端口，却会挡住 Go 缓存和仓库内的写入，而且 agent 会自己设置 `BypassSandbox` 绕过它（A4）。写档在结果中标 `sandboxed=false`。
- **额度快照**：`agy -p /usage` 不计费，返回结构化的 bucket、剩余比例和重置时间（A5）。job 开始前和结束后各取一次，写入 quota 和 cost。
- **重新评估**：用 `gemini-3.8-flash` 跑 10 个大任务加 3 个只读任务，评估方法和 [`agy-eval.md`](../agy-eval.md) 相同，结果可以和上一轮直接对比。

### 3.3 kimi：保留 ACP，修 P0（第三个做）

- **G1 错误码**：从 `wire.jsonl` 读 `turn.ended.reason` 和 `error.code`，按下面的规则映射失败类别：
  - `rate_limit` → quota，如果拿得到重置时间就带上；
  - `connection_error`、`overloaded` → transient；
  - `auth_error` → auth；
  - 其他 → other。
- **G4 子代理用量**：统计 `agents/*/wire.jsonl` 全部文件，续接时的基线按每个 agent 分别记录。
- **G13 模型发现**：改用 ACP `session/new` 返回的 configOptions，config.toml 只作离线时的兜底。
- **只读**：不提供原生只读，改为在 worktree 里跑完再丢弃（结果里写明这一点）；或者继续关闭。
- **写档隔离**：标 `sandboxed=false`。

### 3.4 公共部分（随 opencode 一起先做）

- **G2、G3**：ACP 客户端保留错误里的 `data` 字段；按 stopReason 映射结果：`end_turn` 记为 completed，`cancelled`、`refusal`、`max_tokens`、`max_turn_requests` 记为 failed 并附带类别。
- **G5**：非终态事件也改为阻塞发送（带 `<-done` 退出）；ACP 的最终文本在 session 内部累积。codex 用同样的修法。
- **结果字段 `sandboxed`**：结果信封和 `/api/sub/harnesses` 都加上这个字段，skill 文案同步说明。

### 3.5 Muse：走 `muse serve`（MSP，等 Owner 安装）

- **计费**：用订阅，在受管配置里关闭 Contributor mode（Owner 2026-10-16 已定）。
- **协议**：MSP 是 stdio 上的 JSON-RPC 2.0，和 codex app-server 几乎同构，可以以 `codex.go` 为模板，复用 `jsonrpc.go` 和进程组的代码。它提供：
  - 不计费的 `model/list`；
  - 每轮的用量，以及服务端计算的成本；
  - 有类型的 quota 错误，附 `resetsAtMs`。
- **开工前先冒烟**：在受管目录里确认以下几点：
  - 凭据存放在什么位置；
  - 自动更新能否关闭（`MUSE_NO_AUTO_UPDATE`）；
  - 沙箱会不会挡住 Go、npm 缓存和 worktree 内的 git 提交；
  - 子代理的用量是否计入总数；
  - Read-only profile 的 id 是什么。

## 4. 转正门槛（草案，按 §2 逐项对照）

两道门：

1. **适配一致性**（按 harness 评）：§2 中能做到的项全部做到，并有测试覆盖，包括：
   - fake 回放测试；
   - 用录制的真实会话作 golden 回放，每家至少 5 条：写、只读、续接、取消、注入失败；
   - 协议漂移 CI 保持绿；
   - `sub doctor` 的检查项和 codex 一样多，没有 SKIP。

   做不到的项必须是上游限制，并在结果和 skill 文案中注明（例如 `sandboxed=false`）。
2. **路线质量**（按 harness + 模型评）：10 个大任务至少 7/10 一次完成；有只读能力的，3 个只读任务要 3/3 收敛。达标的路线写进 router 的默认值。

两道门都过了，才去掉 `Experimental` 标签和 skill 里的横幅，并按规则升插件版本。

### AJ-713 capability status (2026-10-17)

✅ implemented with the linked evidence; ⚠️ upstream limitation; ❌ pending work.
These are adapter capabilities, not promotion or route-quality results. Experimental labels
are unchanged. The host owns promotion. Source paths are relative to this repository.

| Capability | codex | opencode | kimi | agy |
|---|---|---|---|---|
| C1 Typed persistent session | ✅ [app-server](../../internal/cockpit/codex.go) | ✅ [serve](../../internal/cockpit/opencode_server.go) | ✅ [ACP](../../internal/cockpit/acp.go) | ✅ [stream-json](../../internal/cockpit/agy.go) |
| C2 Lossless streaming | ✅ [TestHarnessBackpressureV2](../../internal/cockpit/harness_v2_test.go) | ✅ [blocking dispatch](../../internal/cockpit/opencode_serve.go) | ✅ [TestHarnessBackpressureV2](../../internal/cockpit/harness_v2_test.go) | ✅ [emit / process.read](../../internal/cockpit/agy_process.go) |
| C3 Accurate final text | ✅ [codex tests](../../internal/cockpit/codex_test.go) | ✅ [TestGoldenReplay](../../internal/cockpit/golden_replay_test.go) | ✅ [TestACPFinalTextV2](../../internal/cockpit/harness_v2_test.go) | ✅ [TestAgyV2EmptyFailuresAndFollowups](../../internal/cockpit/agy_v2_test.go) |
| C4 Resume | ✅ [thread resume](../../internal/cockpit/codex.go) | ✅ [opencode-resume.json](../../internal/cockpit/testdata/golden/opencode-resume.json) | ✅ [kimi-resume.json](../../internal/cockpit/testdata/golden/kimi-resume.json) | ✅ [conversation reuse](../../internal/cockpit/agy_v2_test.go) |
| C5 Failure classification | ✅ [typed error info](../../internal/cockpit/codex.go) | ✅ [TestOpencodeFailureEvidenceV2](../../internal/cockpit/harness_v2_test.go) | ✅ [TestKimiP0FailureCodes](../../internal/cockpit/kimi_p0_test.go) | ✅ [TestAgyV2TransientTable](../../internal/cockpit/agy_v2_test.go) |
| C6 Transient continuation | ✅ [retry tests](../../internal/sub/retry_test.go) | ✅ [TransientRetry / worker](../../internal/sub/harness.go) | ✅ [TestKimiP0WireFailureRetries](../../internal/sub/kimi_p0_test.go) | ✅ [TestAgyV2TransientContinuation](../../internal/cockpit/agy_v2_test.go) |
| C7 Quota, resets, snapshots | ✅ [quota tests](../../internal/sub/quota_test.go) | ⚠️ [typed quota; no reset/snapshot API](../../internal/cockpit/opencode.go) | ⚠️ [wire quota; reset only when supplied, no snapshot API](../../internal/cockpit/kimi_failure.go) | ✅ [non-billable usage snapshots](../../internal/cockpit/agy_quota_test.go) |
| C8 Per-turn usage | ✅ [token usage mapping](../../internal/cockpit/codex.go) | ✅ [session/child deltas](../../internal/cockpit/opencode_sessions.go) | ✅ [all-agent offsets, partial on missing wire](../../internal/sub/kimi_usage.go) | ✅ [TestAgyV2PersistentAndStepUsage](../../internal/cockpit/agy_v2_test.go) |
| C9 Non-billable model / effort discovery | ✅ [model/list](../../internal/cockpit/codex_models.go) | ❌ [model discovery implemented; variant/effort list not exported](../../internal/cockpit/opencode.go) | ✅ [TestKimiP0ACPModelsNoPrompt](../../internal/sub/kimi_p0_test.go) | ✅ [model table + effort constraints](../../internal/sub/agy_v2_test.go) |
| C10 Model fulfillment | ✅ [actual model checks](../../internal/cockpit/codex.go) | ✅ [assistant model](../../internal/cockpit/opencode_serve.go) | ✅ [wire request model](../../internal/cockpit/kimirecord.go) | ✅ [init.model validation](../../internal/cockpit/agy.go) |
| C11 Cancel + process cleanup | ✅ [lifecycle tests](../../internal/cockpit/lifecycle_test.go) | ✅ [TestOpencodeServeAbortV2](../../internal/cockpit/opencode_serve_test.go) | ✅ [ACP process cleanup](../../internal/acp/process_test.go) | ✅ [descendant cleanup](../../internal/cockpit/agy_process.go) |
| C12 Read-only | ✅ [OS sandbox](../../internal/cockpit/codex.go) | ⚠️ [native plan + baseline; no OS confinement](../../internal/sub/readonly.go) | ⚠️ [not offered; entry rejection](../../internal/sub/kimi_review_test.go) | ⚠️ [plan / hook / baseline; no OS confinement](../../internal/sub/readonly.go) |
| C13 Write isolation | ✅ [workspace-write sandbox](../../internal/cockpit/codex.go) | ⚠️ [not sandboxed](../../internal/sub/doctor_alignment.go) | ⚠️ [not sandboxed](../../internal/sub/doctor_alignment.go) | ⚠️ [not sandboxed](../../internal/sub/doctor_alignment.go) |
| C14 Managed config | ✅ [native config overrides](../../internal/cockpit/codex.go) | ✅ [private XDG homes](../../internal/sub/opencode_home.go) | ✅ [private KIMI_CODE_HOME](../../internal/sub/kimi_home.go) | ✅ [private HOME](../../internal/sub/agy_home.go) |
| C15 Uniform doctor | ✅ [ten checks](../../internal/sub/doctor_alignment_test.go) | ✅ [ten checks](../../internal/sub/doctor_alignment_test.go) | ✅ [ten checks; read-only N/A](../../internal/sub/doctor_alignment_test.go) | ✅ [ten checks incl. hook](../../internal/sub/doctor_alignment_test.go) |
| C16 Drift CI | ✅ [schema workflow](../../.github/workflows/codex-schema.yml) | ✅ [API workflow](../../.github/workflows/opencode-api.yml) | ✅ [help + unauthenticated initialize](../../.github/workflows/harness-drift.yml) | ✅ [help + fixture-derived constants](../../.github/workflows/harness-drift.yml) |
| C17 Replay | ✅ [fake app-server](../../internal/cockpit/codex_test.go) | ✅ [five real goldens](../../internal/cockpit/golden_replay_test.go) | ✅ [four real goldens; no read-only upstream](../../internal/cockpit/golden_replay_test.go) | ❌ [synthetic contract only; live recording deferred to host evaluation](../../internal/cockpit/golden/testdata/agy-contract.json) |

The golden suite compares normalized turn-event order, final text, usage, failure category,
and session ID through the production adapters. Housekeeping notifications (session metadata,
command menus and ACP usage notifications without usage data) are excluded from ordering;
all raw frames remain in the transcript. Renaming `tokens` / wire `usage` to `tokenz` must
produce an outcome mismatch (`TestGoldenDetectsRenamedTokens`).

For C15, every harness prints the same ten rows. Explicit non-applicability is allowed:
kimi read-only and non-agy read-only hooks remain SKIP with reasons. This clarifies the
older blanket “no SKIP” wording above; applicable checks must still pass. Sandbox is a
declaration, not an attempted escape. Failure classification injects a known offline
protocol error at the production classifier boundary; it launches no CLI and cannot bill.
The CI skips unavailable installations with a reason; such a skip is not a passed live drift
check. C16 above records the implemented gate, not evidence that a future scheduled run passed.

## 5. 顺序

1. **AJ-710**　opencode serve + 公共部分（G2、G3、G5、`sandboxed`）
2. **AJ-711**　agy v2，然后用 flash 重新评估
3. **AJ-712**　kimi P0
4. **AJ-713**　一致性门：golden 回放、各家的漂移 CI、doctor 对齐，与 1–3 并行补齐
5. **AJ-714**　Muse（Owner 安装并登录后开始）
