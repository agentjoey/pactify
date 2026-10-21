# 委派任务摘要契约

成本汇总使用独立的只读命令 `pactify sub cost`，JSON 与容错约定见 [sub-cost.md](sub-cost.md)。

`internal/sub.JobSummary` 是 `/jobs` 和本地 panel 共用的数据层。
`Store.Summarize(state, peers, now)` 接收通过 `Store.Load` / `Store.Recover` 原子读取的 State，读取该 job 的可选 result 和日志尾部；`peers` 应包含全部 job 的 State，以生成完整的反向续接关系。该方法不修改 job。

```sh
pactify sub status --json                    # 最新 10 条
pactify sub status --all --json              # 全部
pactify sub status --repo /path/to/repo --json
pactify sub status <id> --json                # 单个对象
```

列表按 `createdAt` 倒序，同时间按 id 倒序；空列表为 `[]`。`--repo` 在截取 10 条前过滤，按原始仓库根目录匹配，接受仓库子目录、相对路径和符号链接。人类输出的用量汇总也只统计该仓库，但仍包括显示上限以外的 job。不存在的历史仓库按绝对路径匹配。单个 job 不符合 `--repo` 时退出 2。

## JSON 兼容性

删除 `issue` 字段及编号提取。标题只取 brief 第一条非空行（不再跳过 `Background:` 等段落标签），保留 Markdown 清理与 80 字符上限；使用 issue tracker 时可自行把编号写进标题。

列表仍为对象数组，保留 `jobId`、`status`、`harness`、`cwd`、`createdAt`、可选 `finishedAt`，新增下面的摘要字段；`id` 与 `jobId` 相同。
单个 job 仍为对象，保留完整 State 的既有字段（包括 `request`、`updatedAt`、`error`、进程信息、worktree 元数据）和 `duration`，新增摘要字段；它没有 `jobId` 别名。单个对象仍保留历史的零值 `finishedAt` 表示未结束；列表则省略未结束的 `finishedAt`。

**目录字段的兼容例外**：Go 的 `JobSummary.Cwd` 是实际执行目录；CLI 的 `cwd` 保持历史含义，为 `request.cwd`（原始请求目录），实际执行目录统一通过新增的 **`runCwd`** 获取。消费者应使用 `repo` 定位原始仓库，使用 `runCwd` 定位执行目录。这样可以新增信息而不改变已有 `cwd` 的含义。

## 字段

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | string | job id |
| `title` | string | brief 第一条非空行，去掉 Markdown 标题符号、`*` / `_` 强调符号及反引号，压成单行；最多 80 个 Unicode 字符（含截断省略号） |
| `harness` | string | 请求的 harness |
| `effort` | string，可省略 | State 中记录的请求 effort；未知时不从 router 当前配置反推 |
| `modelRequested` / `modelActual` | string | 请求模型 / result 记录的实际模型；未知为空 |
| `access` | string | 请求的权限档 |
| `configMode` | string | State 记录的配置模式；旧 job 未记录时为空 |
| `repo` | string | 原始仓库根目录；worktree job 取 `originalRoot`；普通 job 在创建时记录，旧 job 可从仍存在的请求目录解析 |
| `cwd` / `runCwd` | string | CLI 中分别为原始请求目录 / 实际运行目录；普通 job 两者相同 |
| `worktree` | object，可省略 | 保留既有 worktree 字段，并新增 `status`：`pending` / `applied` / `discarded`；非 worktree job 省略 |
| `failureClass` | string，可省略 | 失败类别：`transient` / `quota` / `auth` / `model` / `idle_timeout` / `canceled` / `refusal` / `max_tokens` / `max_turn_requests` / `readonly_violation` / `other`；新失败和取消 job 必填，成功及未结束 job 不写；旧 job 可缺失 |
| `quotaResetAt` | RFC3339 string，可省略 | quota 的已知重置时间；无法解析时省略，其他类别不写 |
| `retryAttempt` / `retryLimit` | number，可省略 | running job 正在等待或执行的续接序号 / 上限；有这些字段时显示 `retrying (n/N)`；底层 status 仍为 running |
| `retryCount` | number，可省略 | 已发起的自动续接次数；终态显示续接次数，0 时省略；不计手动 resume |
| `status` | string | `queued` / `running` / `succeeded` / `failed` / `canceled` |
| `createdAt` | RFC3339 string | 创建时间 |
| `startedAt` | RFC3339 string，可省略 | worker 领取 job、进入 running 的时间；旧 job 不从 `updatedAt` 猜测 |
| `finishedAt` | RFC3339 string，可省略 | 结束时间（单个对象的历史零值例外见上文） |
| `dismissedAt` | RFC3339 string，可省略 | 首次 `pactify sub dismiss <id>` 确认已处理的时间；未 dismiss 时省略 |
| `duration` | string | 从创建到结束或查询时刻的耗时，包含排队时间，四舍五入到秒；保持旧语义，如 `3s`、`1m2s` |
| `attention` | array | `{kind, message}` 列表；无需处理时为 `[]`；只有终态 job 会产生条目 |
| `activity` | object，可省略 | 仅 running job 存在，详见下文 |
| `resumeOf` | string | `--resume` 创建时选中的直接来源 job id；未续接或未记录时为空 |
| `resumedBy` | string array | `resumeOf` 指向本 job 的直接后继 id，按 id 排序；支持分叉，为空时 `[]` |
| `inputTokens` / `outputTokens` | number | result 的最终 token 统计；尚无结果或未记录时为 0 |
| `costUSD` | number，可省略 | result 中已知的正美元价格；旧信封默认 0 无法区分未知与免费，因此省略 |
| `usagePartial` | bool，可省略 | 沿用 result 的标记；为 true 时只有已记录的用量，可能缺少末段，不能视为全 job 精确总量 |
| `changedFiles` | number | result 中的改动文件数 |
| `addedLines` / `deletedLines` | number | result 中的增删行数 |
| `warningCount` | number | result 中的 warnings 条数 |
| `sessionId` | string | result 中的 harness session id；未知为空 |

`attention.kind` 为以下字符串枚举，`message` 是供人阅读的一句说明，消费者应按 kind 判断：

| kind | 条件 |
| --- | --- |
| `worktree_pending` | job 已结束，worktree 尚未记录 apply 或 discard |
| `failed` | job 状态为 failed，且尚未确认处理 |
| `warnings` | result 包含至少一条需要处理的 warning，且尚未确认处理 |

终态 job 的 `dismissedAt` 非零，或 worktree 状态为 `applied` / `discarded`，即视为已确认处理，
不再产生 `failed` / `warnings` attention。`worktree_pending` 的规则不变。
apply 和 discard 会标记共享 worktree 的已有各轮，因此这些轮次的失败和警告也会清除 attention；
新续接轮次仍按自己的 pending 状态处理。`status`、`failureClass`、`warningCount` 等字段保持原值。

审查完毕且无需继续处理时，运行 `pactify sub dismiss <id>` 清除失败和警告的 attention，
包括没有 worktree 的只读 job。该命令只接受一个完整 job id，要求 job 已结束，且 worktree 已 apply 或 discard
（没有 worktree 也可）。重复执行成功，保留首次时间。它只在生命周期锁内更新 State，不删除或改写 result、日志及续接关系；
与 apply/discard 一样不提供 `--json`。

Informational warnings 不产生 `warnings` attention：harness 的 informational warning (agy experimental or opencode permission boundary)、`dirtyWorktreeWarning`、
以 `has not been checked; run pactify sub doctor first` 结尾的提示，以及
`Usage may be incomplete; only recorded usage is included`（`partialUsageWarning`）和以
`CLI version changed: ` 开头的提示。它们仍保留在 result、`pactify sub result` 输出及 `warningCount` 中。
其他警告仍需要处理，包括空回复警告。

worktree 的 `applied` 是一次成功 apply 的持久化记录（空补丁的成功 apply 也算）。成功 apply 为共享该 worktree 的已有 job 记录标记；新续接 job 的标记重置为 pending。discard 后状态优先显示 discarded。查询不会根据当前仓库内容猜测是否 apply，也不会把目录消失当作 discard。版本升级前未记录的 apply 无法还原，显示 pending。这里只记录元数据，原有 apply 冲突检测、补丁操作和 discard 行为保持原样。

续接来源记录在新 job 的 State，`request.resume` 继续保存原 session id；两者不互相替代。历史 job 即使 session id 相同，也不反推 `resumeOf`，避免把同会话里的分叉误当成直接前后关系。

## 运行中 activity

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `summary` | string | 最近一条完整工具事件或消息的可读摘要，复用 `logs_readable` 的消息合并和工具格式化；最多 240 字符加省略号，只保留一行；暂无事件时为空 |
| `inputTokens` / `outputTokens` | number，可省略 | 尾部窗口内最近的 usage；Codex 优先使用每个 thread 的 running total，再合计窗口内可见的 thread；没有 usage 时省略，已报告 0 时保留 0 |
| `usagePartial` | bool，可省略 | 日志超过尾部窗口且存在 usage 时为 true，表示可能缺少窗口外 thread 的用量；不能把它当作全 job 精确总量 |

每次最多读取 `log.jsonl` 最后 **256 KiB**，以打开后的文件大小为快照。被窗口截断的首条记录、没有换行提交的最后一条记录及损坏记录均忽略，不会整份扫描大日志。usage 可能不在窗口内，此时不虚构 token 数。最终用量以 result 的顶层 token 字段为准，并结合顶层 `usagePartial` 判断是否可能不完整；Kimi 的独立用量验证通常只在 job 结束后进入 result。

失败、取消、空闲超时或连接提前关闭时，保留已收到的用量，保守标记 `usagePartial: true`，并在 result 中警告 `Usage may be incomplete; only recorded usage is included`。用量独立核实失败也设置同一标记，不改变任务的成功状态。尚未启动就取消的 job 不因此标记 partial。worker 本身消失时，兜底结果从日志中的完整事件恢复用量；opencode serve 的 usage 事件带 `accounting: job_delta`，可恢复本轮成本；历史 ACP 日志没有此标记，其会话累计成本仍不作为本 job 成本。

opencode 使用 v1 `GET /session/{id}` 的 tokens/cost 会话汇总，扣除本次 Start 时的基线：输入为 input + cache.read + cache.write，输出为 output + reasoning。自动重试共用启动基线，手动续接在 server 重启后重新取基线，不重复计算历史。`/message` 返回的单条用量不参与累计，也不再调用 `opencode export`。无法取得完整汇总时标记 `usagePartial`。

State 沿用原子 rename 约定，status 沿用现有 Recover 和生命周期锁处理失联 worker。JSON 摘要遇到缺失或损坏的 result 仍显示 job，结果字段留空或为 0；人类输出中的全量用量汇总继续对损坏/缺失的终态 result 报错，不静默给出不完整汇总。旧 job 缺少新增元数据时正常输出。

列表查询跳过尚未写入 `state.json` 的 job 目录（包含 `/jobs` 所用的
`sub status --all --json`）；直接查询该 job 仍报告状态文件缺失。

面板日志流首次连接只读最后 1 MiB，忽略窗口截断的首条记录，最多展示 100 行；
之后从当前偏移增量读取。跟随模式中单条记录最多保留 64 KiB，超长行截断；
没有新字节时不复制未完成的行。CLI 非跟随读取及显式 `--tail -1` 的完整历史读取保留。

worker 每 100 ms 将后代进程的 PID 和内核启动时间保存到 `state.json`，
包括已观察到的独立进程组，并继续跟踪仍存活的已保存后代。恢复时合并实时快照，
逐个校验身份后清理；确认已登记的进程退出后才删除 job TMPDIR。
检查、持久化或终止失败时保留 TMPDIR。周期快照仍有采样间隙，不能保证记录到
在两次采样间创建并立即脱离整个已知进程树的进程。

## 失败分类与额度记忆（R1）

`result.json` 与摘要共用 `failureClass`、`quotaResetAt`。分类是新增元数据，
State 的 `error` 原文和既有退出码保持不变。各 harness 的匹配表放在 cockpit adapter 旁；
优先使用 Codex `rateLimits.primary/secondary.usedPercent >= 100`、`codexErrorInfo`、
ACP 的 `AUTH_REQUIRED (-32000)`、opencode 的 `APIError.data` 等结构化证据，最后匹配错误文本。
opencode 的 isRetryable=true 或 5xx 为 transient；429 带 balance/quota/credit/budget 等额度说明为 quota，其他 429 为 transient。
SSE session.error 的配置/模型错误为 other，并保留可读错误。ACP 只有 end_turn 完成，cancelled/refusal/max_tokens/max_turn_requests 都失败并保留类别。
`-32603` 只是通用 Internal Error，不能单独判为 transient；例如余额不足仍为 quota。
用户取消优先于后端错误；空闲超时为 idle_timeout。read-only 拒绝、git 环境错误、
进程被杀和没有取消记录的孤儿 job 仍为 other。不会对成功回复或任务正文匹配错误关键词。

认证依据：[Kimi ACP 文档](https://github.com/MoonshotAI/kimi-cli/blob/main/docs/en/reference/kimi-acp.md)
确认未登录返回 -32000；Codex 的 unauthorized 来自仓库内固定协议 schema；
agy 的过期登录提示来自[官方 CHANGELOG 1.3.3](https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md)，
并覆盖本仓库 managed-home 明确要求 `run agy login first` 的错误。未知提示归为 other。

额度记录位于 `$PACTIFY_HOME/quota.json`，JSON 对象的键为 `harness/model`，
优先实际模型，其次请求模型；模型均未知时键仅为 `harness`。值包含
`harness`、可选 `model`、`resetAt`（未知为 null）、`jobId`、`recordedAt`、`message`
（原始错误的单行摘要，最多 512 个 Unicode 字符）。文件权限为 0600，写入持有独立
`quota.json.lock` 文件锁，再用同目录临时文件 + rename 原子替换；同路线较旧的记录不覆盖较新的记录。

**失效规则**：`now >= resetAt` 时忽略；无 resetAt 的条目在记录后 **24 小时**失效。
这是本地缓存的有效期，不代表上游确认的额度重置。读取不修改磁盘，后续写入会清除过期记录。
Codex 多个窗口耗尽时采用最晚重置时间；有耗尽窗口未报告时间时保守记录为未知。
额度文件损坏时读取报错；写入失败保留原文件，并在 job result 中追加 warning，
仍发布原始失败及分类，不改写 State.error。

R3 使用 `Store.ExhaustedRoutes(now)` 读取全部有效路线（按键排序，空列表为 `[]`）。
CLI 对应只读接口 `pactify sub quota --json`，返回 QuotaRoute 数组，不修改 status 的既有 JSON 形状。
`sub status` 人类输出在存在耗尽路线时显示顶部额度行（不限于当前仓库或显示的 10 条 job），
无耗尽路线时保留原输出；`sub quota` 人类输出可显式查询空状态。
`sub status` / `sub result` 显示如 `failed (quota, resets in 6d 23h)`；
`/jobs` skill 通过独立 quota 查询展示当前耗尽路线，Jobs 卡片显示类别标签。
历史 result 缺少分类时不回写或猜测，继续兼容显示。


## 临时错误自动续接（R2）

`sub run --retries N` 接受 0–5，默认 2；0 关闭自动续接。Request 中保存
`retries` 数值，旧请求未记录时采用默认值。仅在一轮失败经 R1 分类为 transient、
job 尚未取消且会话仍可用时续接。quota、auth、model、idle_timeout、canceled、other
不续接；成功回复中的错误关键词也不触发。首次等待 30 秒，第二次及后续均等待 120 秒。
context 取消立即结束等待；没有收到信号时，每 100 ms 检查持久化取消标记。
退避不触发 idle timeout，下一轮重新计算空闲时间。

续接发送固定提示词：

> The previous turn was interrupted by a transient backend error. Continue the task from where you left off; do not redo completed steps.

同一个 job、session 对象、harness、模型和 effort 继续使用，不调用 Start/Resume 创建
另一个会话，也不提前关闭或隐藏会话。各 adapter 的路径已从实现确认：

| Harness | 同会话下一轮 |
| --- | --- |
| codex | 原 threadId 上再次 `turn/start`；最终 Close 才 archive |
| kimi | 原 ACP sessionId 上再次 `session/prompt` |
| agy | 下一轮进程传 `--conversation <id>`；未显式选模型时锁定首轮报告的模型 |
| opencode | 原 v1 session id 上再次 `prompt_async`，手动续接保留受管数据库 |

四者都有续接接口。分类仍完全沿用 R1；目前 Kimi 的匹配表尚无 transient
证据，因此未识别的后端错误仍归 other，不因支持同会话 prompt 就扩大重试范围。
不支持续接的 adapter、或没有 session id 时，在 result.warnings 中记录
`transient retry unsupported for <harness>`，保留原失败，不另开会话。

退避和续接执行期间仍为 `status: running`；`retryAttempt` / `retryLimit` 表示正在尝试
第几次续接，供 CLI、`/jobs` 与 Jobs 页显示 `retrying (n/N)`。`retryCount` 只计已发起
的续接，取消在退避期间发生时不增加。result 新增：

```json
{"retries":[{"class":"transient","error":"UNAVAILABLE (code 503)","at":"2026-10-16T04:00:00Z"}]}
```

每个条目对应一次续接，error 是触发该次续接的上一轮错误，at 为发起时间。
State 同时保存历史用于 worker 消失后的兜底结果；终态摘要的 retryCount 以 result 为准。
终稿只取最后一轮回复；最后一轮失败时按该轮分类（次数耗尽仍为 transient），成功则清空
failureClass。用户取消仍优先。先前的 transient 错误移入 retries，不遮盖最后一轮错误。

多轮的 token / 成本按现有口径累计：Codex thread running totals、agy / opencode
adapter 累计值不重复相加；Kimi prompt usage 在轮间累加。Kimi wire 和 opencode 会话汇总
仍只在最终结束时验证，基线覆盖当前 job 的所有 prompts，包含失败轮次。一次异常结束可能
漏末段用量，即使续接成功也保守保留 usagePartial。`sub cost` 的 RETRIES 是有过续接的
job 数，单个 job 续接多次也只计 1；手动 `--resume` 产生新 job，不计自动续接。

## 空回复补问

所有 harness（codex、kimi、agy、opencode）正常结束一轮、且最终文本经 `strings.TrimSpace`
仍为空时，worker 在同一 live session 内恰好补问一次：

> You ended without a written answer. Reply now with your result: what you found or changed and which checks you ran. Do not start new work.

插入点是轮次失败分类之后、transient retry 判断之前。只有 turn done、无失败类别、无 prompt/turn
错误、未取消/中断/超时或 grace expired、session 未关闭时才补问。补问前排空已排队的轮次尾部事件以识别已关闭的 stream，并再次检查持久化取消请求。
补问独立于 `--retries`，不占重试次数，`--retries 0` 仍生效；补问后若 transient retry 再次产生空回复，
不会二次补问。补问轮失败沿用正常失败处理。只读策略、累计 tool-call limit、idle/total timeout 继续生效。

`result.json` / `pactify sub result --json` 新增可选数字 `emptyReplyFollowups`：
未补问时省略，补问后为 1；不加入 status 摘要的 `retryCount`。日志记录 system 事件
`state: "empty_reply_followup"`，`text` 为 harness 名。轮内输出按 transient retry 的方式清空，
用量与 tool calls 累计保留，Live 日志也沿用相同用量口径。

补问得到文本时正常成功，只记录次数，不因此警告。补问正常结束仍为空时依然 `succeeded`，
并在 `Result.Warnings` 中追加下列原文（`<id>` 替换为实际 job id）：

> empty final reply after one follow-up: the harness ended without a text answer. Check pactify sub logs <id>, or resume to ask for the result.

若会话在空回复的那一轮后已关闭、无法补问，同样判 `succeeded` 并追加：

> empty final reply: the session ended before a follow-up could be sent. Check pactify sub logs <id>, or resume to ask for the result.

两条警告都需要处理，不属于 informational 过滤项；`sub status` 的 `attention.kind` 包含 `warnings`。
只读任务此时尚未交付：先查看 `pactify sub logs <id>`，再通过 `--resume <id>` 用简短 brief 索取结果；
仍为空则换 harness 或 model 重跑。写任务照常审查完整 diff 并自行重跑验收，通过 `--resume <id>`
取得交接说明，或根据 diff 与自己的检查补写。续接时保留原 harness、cwd、权限及 worktree。

agy 正常空回复不再生成 `empty_answer` 失败；无内容的 `print_timeout` 仍为 `timeout`。
agy 独立的 `CompletionFollowups`（默认 2 次，等待运行中的命令并报告实际结果）保持原行为，
与 worker 的这一次空回复补问分开计数。

## 结果信封的沙箱字段

`result.json` 和 `pactify sub result --json` 总是包含布尔字段 `sandboxed`：codex 为 true，其他 harness 为 false。
它声明 OS 沙箱能力，与请求的 access 独立。opencode 只读依赖 plan、permission 拒绝和最终 diff 检查，
仓库外的副作用没有 OS 级限制。ACP 的累计终稿随终态事件的 `text` / `final` 发布，事件背压不会丢文本或工具计数。


## Live 事件流

Dashboard 的 `GET /api/sub/jobs/{id}/events` 与本地只读 panel 的
`GET /api/jobs/{id}/events` 复用 `Store.Logs` 的读取、跟随和取消逻辑。
原 `/logs` 仍返回可读字符串，`/events` 的 SSE `data` 返回结构化 JSON 对象；
终止与读取失败仍使用 `done` / `log-error`。两个入口均只读，沿用各自的认证边界。

初始读取限最后 1 MiB、最多 100 个已识别事件；跟随时单条物理记录限 64 KiB。
破损、被窗口截断、未提交的跟随行及未知事件不渲染。
结构化输出不传 `raw`，仅投影已知工具身份/类型/阶段、累计 usage、diff 计数和额度摘要；
保留 adapter 写日志前的脱敏，并对显示文本中的常见凭据赋值补充遮盖。
Codex 用每个 thread 的 running total，Kimi 自动续接沿用 Collector 的逐轮累加口径；
opencode 只有带 `accounting: job_delta` 的已知价格才进入事件 `costUSD`。
无价格时不把 Usage 的默认零值当成免费。

Live 仅在 Dashboard 或详情 Live 页签挂载时打开事件流，全局最多 4 条；
其余 job 显示现有两秒 summary 轮询的数字，等待空闲连接。
页面隐藏、切换页面或关闭详情时取消流，重新显示页面时重新读取有界尾部，重置观测计数。
工具 calls、工具节点和 session log 仅代表当前连接观测窗口；最终 token/diff 以 summary 为准。
没有运行中的 job 时静态展示最近运行的记录尾部，不模拟时序或概率，也不推测内部子 agent。
动画按事件触发，`prefers-reduced-motion` 下禁用。

验收使用 `web/e2e/live.spec.ts`；最终嵌入构建截图可运行
`cd web && node scripts/shots-live.mjs`，默认输出 `artifacts/live/`（仓库根目录），
覆盖 Dashboard、详情、panel 和手机宽度。浏览器无法启动时不会生成替代截图。
