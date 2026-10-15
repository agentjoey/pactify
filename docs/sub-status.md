# 委派任务摘要契约

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

列表仍为对象数组，保留 `jobId`、`status`、`harness`、`cwd`、`createdAt`、可选 `finishedAt`，新增下面的摘要字段；`id` 与 `jobId` 相同。
单个 job 仍为对象，保留完整 State 的既有字段（包括 `request`、`updatedAt`、`error`、进程信息、worktree 元数据）和 `duration`，新增摘要字段；它没有 `jobId` 别名。单个对象仍保留历史的零值 `finishedAt` 表示未结束；列表则省略未结束的 `finishedAt`。

**目录字段的兼容例外**：Go 的 `JobSummary.Cwd` 是实际执行目录；CLI 的 `cwd` 保持历史含义，为 `request.cwd`（原始请求目录），实际执行目录统一通过新增的 **`runCwd`** 获取。消费者应使用 `repo` 定位原始仓库，使用 `runCwd` 定位执行目录。这样可以新增信息而不改变已有 `cwd` 的含义。

## 字段

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | string | job id |
| `title` | string | brief 第一条非空行，去掉 Markdown 标题符号、`*` / `_` 强调符号及反引号，压成单行；最多 80 个 Unicode 字符（含截断省略号） |
| `issue` | string | brief 前 10 个物理行中首个符合 `[A-Z]+-[0-9]+` 的编号，带单词边界；没有则为空 |
| `harness` | string | 请求的 harness |
| `modelRequested` / `modelActual` | string | 请求模型 / result 记录的实际模型；未知为空 |
| `access` | string | 请求的权限档 |
| `configMode` | string | State 记录的配置模式；旧 job 未记录时为空 |
| `repo` | string | 原始仓库根目录；worktree job 取 `originalRoot`；普通 job 在创建时记录，旧 job 可从仍存在的请求目录解析 |
| `cwd` / `runCwd` | string | CLI 中分别为原始请求目录 / 实际运行目录；普通 job 两者相同 |
| `worktree` | object，可省略 | 保留既有 worktree 字段，并新增 `status`：`pending` / `applied` / `discarded`；非 worktree job 省略 |
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

每次最多读取 `log.jsonl` 最后 **256 KiB**，以打开后的文件大小为快照。被窗口截断的首条记录、没有换行提交的最后一条记录及损坏记录均忽略，不会整份扫描大日志。usage 可能不在窗口内，此时不虚构 token 数。最终精确用量以 result 的顶层 token 字段为准；Kimi 的独立用量验证通常只在 job 结束后进入 result。

State 沿用原子 rename 约定，status 沿用现有 Recover 和生命周期锁处理失联 worker。JSON 摘要遇到缺失或损坏的 result 仍显示 job，结果字段留空或为 0；人类输出中的全量用量汇总继续对损坏/缺失的终态 result 报错，不静默给出不完整汇总。旧 job 缺少新增元数据时正常输出。
