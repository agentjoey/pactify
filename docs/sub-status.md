# 委派任务摘要契约

成本汇总使用独立的只读命令 `pactify sub cost`，JSON 与容错约定见 [sub-cost.md](sub-cost.md)。

`internal/sub.JobSummary` 是 `/jobs`（AJ-658）和本地 panel（AJ-659）共用的数据层。
`Store.Summarize(state, peers, now)` 接收通过 `Store.Load` / `Store.Recover` 原子读取的 State，读取该 job 的可选 result 和日志尾部；`peers` 应包含全部 job 的 State，以生成完整的反向续接关系。该方法不修改 job。

```sh
pactify sub status --json                    # 最新 10 条
pactify sub status --all --json              # 全部
pactify sub status --repo /path/to/repo --json
pactify sub status <id> --json                # 单个对象
```

列表按 `createdAt` 倒序，同时间按 id 倒序；空列表为 `[]`。`--repo` 在截取 10 条前过滤，按原始仓库根目录匹配，接受仓库子目录、相对路径和符号链接。人类输出的用量汇总也只统计该仓库，但仍包括显示上限以外的 job。不存在的历史仓库按绝对路径匹配。单个 job 不符合 `--repo` 时退出 2。

## JSON 兼容性

AJ-701：删除 `issue` 字段及编号提取。标题只取 brief 第一条非空行（不再跳过 `Background:` 等段落标签），保留 Markdown 清理与 80 字符上限；使用 issue tracker 时可自行把编号写进标题。

列表仍为对象数组，保留 `jobId`、`status`、`harness`、`cwd`、`createdAt`、可选 `finishedAt`，新增下面的摘要字段；`id` 与 `jobId` 相同。
单个 job 仍为对象，保留完整 State 的既有字段（包括 `request`、`updatedAt`、`error`、进程信息、worktree 元数据）和 `duration`，新增摘要字段；它没有 `jobId` 别名。单个对象仍保留历史的零值 `finishedAt` 表示未结束；列表则省略未结束的 `finishedAt`。

**目录字段的兼容例外**：Go 的 `JobSummary.Cwd` 是实际执行目录；CLI 的 `cwd` 保持历史含义，为 `request.cwd`（原始请求目录），实际执行目录统一通过新增的 **`runCwd`** 获取。消费者应使用 `repo` 定位原始仓库，使用 `runCwd` 定位执行目录。这样可以新增信息而不改变已有 `cwd` 的含义。

## 字段

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | string | job id |
| `title` | string | brief 第一条非空行，去掉 Markdown 标题符号、`*` / `_` 强调符号及反引号，压成单行；最多 80 个 Unicode 字符（含截断省略号） |
| `harness` | string | 请求的 harness |
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
| `duration` | string | 从创建到结束或查询时刻的耗时，包含排队时间，四舍五入到秒；保持旧语义，如 `3s`、`1m2s` |
| `attention` | array | `{kind, message}` 列表；无需处理时为 `[]`；只有终态 job 会产生条目 |
| `activity` | object，可省略 | 仅 running job 存在，详见下文 |
| `resumeOf` | string | `--resume` 创建时选中的直接来源 job id；未续接或未记录时为空 |
| `resumedBy` | string array | `resumeOf` 指向本 job 的直接后继 id，按 id 排序；支持分叉，为空时 `[]` |
| `inputTokens` / `outputTokens` | number | result 的最终 token 统计；尚无结果或未记录时为 0 |
| `usagePartial` | bool，可省略 | 沿用 result 的标记；为 true 时只有已记录的用量，可能缺少末段，不能视为全 job 精确总量 |
| `changedFiles` | number | result 中的改动文件数 |
| `addedLines` / `deletedLines` | number | result 中的增删行数 |
| `warningCount` | number | result 中的 warnings 条数 |
| `sessionId` | string | result 中的 harness session id；未知为空 |

`attention.kind` 为以下字符串枚举，`message` 是供人阅读的一句说明，消费者应按 kind 判断：

| kind | 条件 |
| --- | --- |
| `worktree_pending` | job 已结束，worktree 尚未记录 apply 或 discard |
| `failed` | job 状态为 failed |
| `warnings` | result 包含至少一条 warning |

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

## 失败分类与额度记忆（AJ-692 / R1）

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


## 临时错误自动续接（AJ-693 / R2）

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

## 结果信封的沙箱字段（AJ-710）

`result.json` 和 `pactify sub result --json` 总是包含布尔字段 `sandboxed`：codex 为 true，其他 harness 为 false。
它声明 OS 沙箱能力，与请求的 access 独立。opencode 只读依赖 plan、permission 拒绝和最终 diff 检查，
仓库外的副作用没有 OS 级限制。ACP 的累计终稿随终态事件的 `text` / `final` 发布，事件背压不会丢文本或工具计数。
