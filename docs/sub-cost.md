# 委派成本汇总契约

```sh
pactify sub cost                         # --since 7d --by harness
pactify sub cost --since 3d --by harness
pactify sub cost --since 30d --by repo
pactify sub cost --since 24h --json
pactify sub cost --since 2026-10-01 --by model
pactify sub cost --by repo --json
pactify sub cost --by day
```

只读取与其他 `sub` 命令相同的 `$PACTIFY_HOME/jobs/*/{state,result}.json`；未设置
`PACTIFY_HOME` 时使用 `~/.pactify/jobs`。不调用 Recover、不创建目录或锁、不恢复或
修改 job，不读取日志、harness home 或凭据。Jobs 页汇总不在本命令的实现范围内。

## 时间、分组与容错

- 按 State 的 `createdAt` 过滤，范围为 `[since, until]`，`until` 是命令启动时间。
  `--since` 接受 `<N>d`、`<N>h` 或 `YYYY-MM-DD`，默认仍为 `7d`。
- `<N>d` 沿用 status 的日历日约定：包含本地时区今天及之前 N−1 天，从首日零点
  起算。`1d` 从今天零点开始，`3d` 从前天零点开始，`7d` 从六天前零点开始；
  `30d` 同理。跨夏令时时仍按日历日计算。
- `<N>h` 从命令启动时刻精确回溯 N 小时，保留秒与纳秒；`24h` 表示过去 24 小时，
  与 `1d` 的日历日含义不同。日期格式仍从本地时区该日零点起算，包含当天。
- N 必须是仅含十进制数字的正整数，可以有前导零；拒绝 0、负数、带符号或小数的
  数值、其他单位以及组合时长（例如 `1d2h`）。超大数值或使起始时间超出 RFC3339
  年份范围 `0000–9999` 的值会报错，不发生溢出或时间回绕。日期不接受未来日期。
  不支持 `all`。
- `harness` 使用 State 的请求 harness，缺失时取 result 的 harness；`model` 依次取
  result 的 `modelActual`、`modelRequested`、State 的请求模型。
  `repo` 复用 `JobRepo`：worktree 的 `originalRoot` 优先于 State 的 `repo`，
  旧 job 可以从仍存在的请求目录解析仓库根目录。`day` 按本地时区创建日分组。
- 分组按 key 升序。未知模型或仓库保留空字符串 key，文本显示 `(unknown)`，
  不根据 session、目录名或 job id 猜测元数据。
- 成功、失败、取消数取有效 result 的终态；result 原子发布早于终态 State，
  因此有有效终态 result 的 running State 也可计入。`jobs` 为这三类之和。
- 缺失、不可读或损坏的 result 跳过并计数，不导致命令失败。JSON `null`、无终态、
  不匹配的非空 jobId、负数 token 或成本也视为损坏。State 缺失、不可读、损坏、id
  不匹配或创建时间缺失时，单独计数。读取限制在单个 job 目录内，不跟随越界链接。
- `skipped.missingResult` / `invalidResult` 只计时间范围内已知 State 的 job；
  `invalidState` 无法确定创建时间，计扫描到的全部坏 State。非目录条目（含符号链接
  job 目录）忽略。不存在的 store 返回空汇总；store 本身不可读取仍报错。

## 成本与不完整用量

只汇总 result 显式提供的数值 `costUSD`，当前正常由 opencode 提供。
缺失或 `null` 不补零；显式 `0` 是有效已报告成本。`costUSD` 在没有已报告美元成本时
为 JSON `null`。codex、kimi、agy 未提供成本时计为订阅，文本显示 `subscription`；其他
harness 缺失成本时显示 `unknown (N)`。混合分组及合计同时显示已报告美元和
`subscription` / `unknown (N)`，例如 `subscription + unknown (2)` 或
`$0.125000 + subscription + unknown (2)`。

CLI 生成的表头、提示、错误、帮助和成本标签统一使用英文。JSON 的分组枚举 `by`
固定为 `harness` / `model` / `repo` / `day`；订阅在 JSON 中以数值字段
`subscriptionJobs` 表示，没有中文枚举或展示标签。来自 job 的模型、仓库和额度原文
作为数据保留，不翻译。

现有 Result 编码使用 `costUSD,omitempty`，可能省略真实零值；历史数据无法区分
未报告与被省略的零，本命令将这种 opencode job 归为 unknown，不推算价格，也不改写结果。

`usagePartialJobs` 单独统计 `usagePartial: true` 的 job，表格 `PARTIAL` 列显示数量；
它们已记录的 token 和成本仍计入总量，不能把汇总理解为完整用量。
`retries` 统计 result.retries 非空的 job 数；同一 job 续接多次仍只计 1，手动 `resumeOf` 不计入 retries。`fallbacks` 仍为 R3 预留，目前始终为 0。

## JSON 结构

顶层字段固定为 `since`、`until`、`by`、`groups`、`total`、`skipped`、`quotaSnapshots`。
时间为 RFC3339。空数组始终为 `[]`，所有 totals 和 skipped 字段始终输出。

```json
{
  "since": "2026-10-10T00:00:00+08:00",
  "until": "2026-10-16T12:00:00+08:00",
  "by": "harness",
  "groups": [
    {
      "key": "codex",
      "jobs": 1,
      "succeeded": 1,
      "failed": 0,
      "canceled": 0,
      "retries": 0,
      "fallbacks": 0,
      "inputTokens": 100,
      "outputTokens": 10,
      "costUSD": null,
      "costReportedJobs": 0,
      "subscriptionJobs": 1,
      "unpricedJobs": 0,
      "usagePartialJobs": 0
    }
  ],
  "total": {
    "jobs": 1,
    "succeeded": 1,
    "failed": 0,
    "canceled": 0,
    "retries": 0,
    "fallbacks": 0,
    "inputTokens": 100,
    "outputTokens": 10,
    "costUSD": null,
    "costReportedJobs": 0,
    "subscriptionJobs": 1,
    "unpricedJobs": 0,
    "usagePartialJobs": 0
  },
  "skipped": {"missingResult": 0, "invalidResult": 0, "invalidState": 0},
  "quotaSnapshots": [
    {
      "harness": "codex",
      "jobId": "example-job",
      "observedAt": "2026-10-16T11:00:00+08:00",
      "rateLimit": "7d 30% (reset 2026-10-20T00:00:00Z)"
    }
  ]
}
```

`groups` 的每项为 `key` 加 totals 字段；`total` 为全部有效 job 的合计。
`costReportedJobs`、`subscriptionJobs`、`unpricedJobs` 互斥，三者之和等于 `jobs`。
美元使用 result 的原始数值相加，文本保留六位小数，不做定价估算。

额度快照不受 `--since` 过滤：每个 harness 从全部有效历史结果中取最新一条非空
`rateLimit`，按 harness 排序。观测时间优先使用 State 的 `finishedAt`，没有时取
`updatedAt`，再取 `createdAt`；同时间取字典序较大的 job id。忽略未来创建或未来观测
时间。后来的空 `rateLimit` 不覆盖旧快照。保留来源 job 和时间，快照不代表实时查询，
也不解析尚未合入的 R1 `quota.json`。

## 与已有查询的关系

`sub status` / `/jobs` / panel 的摘要与错误处理保持原样；特别是 status 文本用量
汇总仍对终态 result 缺失或损坏报错，本命令独立采用跳过并计数的策略。
Result 的 `float64` / `omitempty` 编码保持不变，worker、result 和日志恢复调用方不受影响。
