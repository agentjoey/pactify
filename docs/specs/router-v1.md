# 路由 v1：失败分类、临时错误续接、额度回退链与成本汇总

> 状态：设计稿（2026-10-16）。Owner 已同意按「先做 1-2-3」推进，本文档对应其中第 3 项。对应 [`harness-subagents.md`](harness-subagents.md) §7 M2 剩余部分里的「router v1」和「跨委派成本汇总」两项；「接入 claude-code」不在本文范围内。
> 背景：
> - agy 评估中，10 个大任务有 3 个被后端的临时错误打断（[`agy-eval.md`](../agy-eval.md)）；
> - 2026-10-16 用 Gemini 4 Argon 并发跑评估，约 6 分钟就耗尽了一周的个人额度，10 个 job 全部失败；
> - 目前 pactify 无法区分「临时错误」和「额度耗尽」，也不会记住哪个 harness / 模型的额度已经用完。

## 1. 目标与非目标

**目标**
1. **失败分类**：每个失败的 job 都给出机器可读的失败类别。
2. **临时错误自动续接**：后端出现 5xx、EOF、stream 中断这类临时错误时，在同一会话内退避后续接，不必让用户手动重跑。
3. **额度记忆与回退链**：某个 harness / 模型的额度耗尽时，记住它什么时候重置；按配置的回退链，自动改由下一个 harness 来跑。
4. **别名**：用一个短名同时指定 harness、模型和 effort，例如 `@cheap`、`@fast`。
5. **成本汇总**：按时间、harness、模型或仓库统计 token 和美元成本。

**非目标（v1 不做）**
- 根据任务内容自动判断该用哪个 harness，比如用模型给任务分类。v1 只按访问档和别名选路；按任务类型选择 harness 由 host（Claude）在 skill 里按规则决定。
- 换 harness 后接着前一个 harness 的进度往下做。不同 harness 的会话不能互相续接，所以 v1 只在「还没留下任何改动」时自动回退（见 §4）。
- 预测额度、按价格选择最便宜的路线。

## 2. 失败分类（R1）

`result.json` 与 `sub status --json` 新增字段 `failureClass`：

| 类别 | 含义 | 已观测到的例子 |
|---|---|---|
| `transient` | 后端临时故障，稍后重试大概率会成功 | agy：`API error ... EOF`、`UNAVAILABLE (code 503)`、`The stream was interrupted. Please continue...` |
| `quota` | 额度或限流已耗尽，在重置之前重试也没用 | agy：`Individual quota reached ... Resets in 166h58m`；codex：额度窗口用量达到 100%；DeepSeek：`Insufficient Balance` |
| `auth` | 未登录、凭据过期 | — |
| `model` | 请求的模型不可用 | `requested model is not offered by this agent` |
| `idle_timeout` | 长时间没有任何进展，被判为空闲超时 | agy T04 |
| `canceled` | 被用户取消 | — |
| `other` | 无法归入以上任何一类 | — |

- 每个 harness 维护一张「错误文本或结构化字段 → 类别」的对照表，放在 adapter 里。能从结构化字段判断的（例如 codex 的 rateLimits、HTTP 状态码），优先使用结构化字段，不靠匹配文本。
- **重置时间**：类别为 `quota` 时，尽量解析出重置时间，写入 `quotaResetAt`。例如 agy 的「Resets in 166h58m」；codex 的 rateLimits 自带 `resets_at`。
- **额度记忆**：额度耗尽时，把这条记录写进 `$PACTIFY_HOME/quota.json`，键为 harness + 模型（模型不确定时只按 harness），内容为重置时间、来源 job 和记录时间。到了重置时间，这条记录自动失效。`sub status` 和 `/jobs` 里显示目前有哪些路线处于额度耗尽状态。

## 3. 临时错误自动续接（R2）

- 当一轮对话以 `transient` 结束、并且 job 还没有被取消时，worker 在**同一个会话**里退避后续接。
  - 续接时发送：「The previous turn was interrupted by a transient backend error. Continue the task from where you left off; do not redo completed steps.」
  - 退避节奏：30 秒、2 分钟，默认最多续接 2 次。用 `sub run --retries N` 调整，`0` 表示关闭。
- 不续接的情况：`quota`、`auth`、`model`、`idle_timeout`、`canceled`、`other`。
- 结果里新增 `retries: [{class, error, at}]`，用量（token、成本）在多次续接之间累加。终稿取最后一轮的回复。
- harness 不支持在同一会话内续接时（需要确认各 adapter 的情况），这个 harness 不做自动续接，在结果里注明。

## 4. 回退链与别名（R3）

**配置文件** `$PACTIFY_HOME/router.json`：没有这个文件时，行为与现在完全相同。

```json
{
  "aliases": {
    "cheap": {"harness": "opencode", "model": "opencode/deepseek-v4.1-flash-free"},
    "mm":    {"harness": "opencode", "model": "minimax-coding-plan/MiniMax-M3"}
  },
  "fallback": {
    "codex": ["@mm", "kimi"],
    "agy":   ["@mm"]
  }
}
```

- **别名**：`sub run @cheap`，插件里写作 `/delegate @cheap <任务>`。别名会展开成 harness、模型和 effort；命令行上显式给出的参数优先于别名里的设置。
- **启动时选路**：
  1. 用户显式指定的 harness 或别名，就是首选路线；
  2. 首选路线在 `quota.json` 里处于额度耗尽状态时，按回退链往下找，跳过同样处于耗尽状态的路线；
  3. 链上的路线全部耗尽时，直接报错，并给出最早的重置时间，不创建 job。
- **运行中回退**：job 以 `quota` 失败时，按以下条件决定是否自动回退：
  - **还没有留下任何改动**（worktree 或工作区相对基线没有变化）：自动用下一条路线、同一份任务说明创建一个新 job。新 job 记录 `fallbackFrom: <旧 job id>`，旧 job 记录 `fallbackTo`。
  - **已经有改动**：不自动回退，避免另一个 harness 在半成品上重做。结果里给出建议：先评审 diff，再用 `sub run <下一条路线> --worktree-from <job>` 在原 worktree 上继续。`--worktree-from` 也属于本项的实现范围。
- **只读档**：目前只有 codex 支持只读，所以只读任务的回退链只能指向 codex；其他路线在选路时直接跳过。
- **成本标注**：回退产生的新 job 在 `/jobs` 和 Jobs 页里显示为「由 X 回退」。

## 5. 成本汇总（R4）

`pactify sub cost [--since 7d|2026-10-01] [--by harness|model|repo|day] [--json]` 读取全部 job 的 `result.json`，按维度汇总：

- 汇总的数据项：job 数（成功 / 失败 / 回退 / 续接）、输入 token、输出 token、美元成本。美元成本只来自能报出价格的 harness，目前只有 opencode。
- 订阅制的 harness（codex、kimi、agy）不显示美元成本，只显示 token；同时附上最近一次看到的额度快照，例如 codex 的 7 天窗口用量，以及 `quota.json` 里耗尽的路线。
- 用量标记为不完整（`usagePartial`）的 job 要单独计数，并在输出里注明。
- 这份数据 `/jobs all` 不显示；dashboard 的 Jobs 页底部显示最近 7 天的汇总（依赖 AJ-689 合并）。

## 6. 退出条件

- 回退链有一次**真实触发**的记录。现成的场景是：agy 的 Gemini 4 Argon 额度要到约 2026-10-23 才重置，可以用 `agy`（Argon）回退到 `@mm`。
- 临时错误续接至少有一次真实触发的记录，或者用可复现的故障注入验证。
- 现有 harness 的 `sub doctor` 全部通过。
- 文档与插件 skill 同步更新：`/delegate` 支持别名，skill 指导 host 何时选用 `@cheap` 一类的低成本路线。

## 7. 拆分与顺序

| 项 | 内容 | 依赖 |
|---|---|---|
| R1 | 失败分类、`quotaResetAt`、`quota.json` | — |
| R2 | 临时错误自动续接 | R1 |
| R3 | `router.json`、别名、启动选路、运行中回退、`--worktree-from` | R1 |
| R4 | `sub cost` 与 Jobs 页汇总 | 只依赖已有字段，和 R1 并行；Jobs 页部分依赖 AJ-689 |
