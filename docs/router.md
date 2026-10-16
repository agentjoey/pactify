# Harness 路由

`pactify sub run` 可通过 `$PACTIFY_HOME/router.json`（默认 `~/.pactify/router.json`）配置别名与额度回退。文件不存在时，保持原行为：默认 codex，不做额度选路和自动回退。配置错误会指出文件与字段，退出码为 2，不创建 job。

```json
{
  "aliases": {
    "cheap": {"harness": "opencode", "model": "opencode/deepseek-v4.1-flash-free", "effort": "low"},
    "mm": {"harness": "opencode", "model": "minimax-coding-plan/MiniMax-M3"}
  },
  "fallback": {
    "codex": ["@mm", "kimi"],
    "agy": ["@mm"]
  },
  "quotaGroups": {
    "agy-argon": {
      "harness": "agy",
      "models": ["gemini-4-argon-low", "gemini-4-argon-medium", "gemini-4-argon-high"]
    }
  }
}
```

三个字段均可省略。别名名只用字母、数字、下划线和连字符，首字符为字母或数字，配置中的名字不带 `@`。别名必须指定已支持的 harness，model、effort 可省略；模型与 effort 的可用性仍由 harness 检查。不要直接复制不支持 effort 的模型设置。

```sh
printf '%s\n' '更新安装说明，检查链接' | pactify sub run @cheap --worktree
printf '%s\n' '实现任务' | pactify sub run @mm --model another-model --effort high
pactify sub router
pactify sub router --json
```

`--model`、`--effort` 显式参数分别覆盖别名字段，包括显式空值。不存在的别名会列出可用名字。Claude Code 支持 `/delegate @cheap <任务>`；适合小型文档等低风险写任务，复杂代码和只读任务优先 codex。别名必须先在本机配置。

## 启动选路

1. 显式 harness / `@alias` 是首选，省略时用 codex。
2. fallback 的键是 harness，值是有序路线列表，每项为 harness 名或 `@alias`。它是该首选 harness 的完整链，不递归拼接其他 harness 的 fallback；相同路线去重。
3. 每条路线依次检查 `quota.json` 中的 harness+model、整个 harness、所属 quotaGroup。只要有一项尚未重置，就跳过该路线。CLI 的 model/effort 覆盖只作用于首选路线；回退路线使用自身设置。
4. quotaGroup 中任意 model 耗尽，整个组都耗尽，避免在共用额度的模型档位间反复失败。组限定在同一个 harness 内；过期记录不起作用。无重置时间的额度记录按 R1 的 24 小时失效规则处理。
5. 全部耗尽则退出 2，显示最早可恢复的时间，不创建 job。一个路线同时受多个耗尽条件约束时，要等所有条件重置。
6. 只读任务仅保留 codex 路线；显式指定不支持只读的 harness 仍报原来的访问档错误。

改选时输出和最终 result.warnings 会保留`quota exhausted for X; using Y`，已知重置时间时附加 `(resets in 6d 18h)`。`sub router` 默认按 Config path、Aliases、Fallback、Quota groups、Exhausted routes 分段展示；已知重置时间同时显示倒计时和 UTC 时间，未知时标明记忆过期时间。`--json` 保持原有字段，并提供展开后的 `resolvedFallback` 列表。运行中的 job 保存剩余路线与 quotaGroups；修改配置只影响后续启动。

`--resume` 继续原 harness 的原会话，保留既有选择规则，不按额度静默切换会话。换 harness 请使用下面的 `--worktree-from`。

## 运行中回退

只有 `quota` 失败才考虑自动回退，transient 仍使用同会话 retries，其他错误与取消不回退。回退前先结束旧 harness 并记录额度，再过滤已耗尽和不支持只读的路线。

- 相对原基线没有改动：创建新 job，用同一份 brief、访问档、用户配置选择与 retries 设置，开启下一条路线的新会话。模型与 effort 使用新路线自己的值。工作区用现有 stash 快照基线，无 HEAD 时用临时 index tree；worktree 用原 base commit。无法读取基线或 diff 时不自动回退。
- 已有改动：保留失败结果，提示 `review the diff, then: pactify sub run <next> --worktree-from <job>`。先评审完整 diff 和未跟踪文件，再把完整任务说明通过 stdin 传入新会话。没有 worktree 的旧式工作区 job 不能使用此参数，需要评审后自行启动工作区任务。
- 无可用后继：停在最后的失败结果，给出耗尽或访问档提示。

旧 job 记录 `fallbackTo`，新 job 记录 `fallbackFrom`。`sub wait <第一个 jobId>` 和 `sub result <第一个 jobId>` 沿链读取最终 job；返回结果的 `jobId` 是最终 id。`sub cancel` 也沿链取消当前尝试。status、日志和历史保留每次尝试，不把失败伪装成成功。`/jobs`、status、Jobs 页显示两个方向的链接；`sub cost` 的 FALLBACKS 统计带 `fallbackFrom` 的 job。

## 在原 worktree 开新会话

```sh
printf '%s\n' '已评审上次的 diff。保留已有实现，完成剩余验收：…' |
  pactify sub run @mm --worktree-from 20261016-120000-abcdef --wait 0s --json
```

源 job 必须已结束，worktree 必须存在、未 discard、没有正在运行的共享会话。此参数与 `--resume` 互斥；可同时带 `--worktree`，无需指定。源 id 支持现有的唯一前缀规则。

新 job 接管现有 worktree，保留路径和原基线，不加载旧 harness 的 session 或 home。旧 id 再次启动、apply 或 discard 会提示当前持有者；后续请用新 id。内部 `worktree.owner` 继续作为共享锁和分组标识，`worktree.holder` 表示转移后的当前持有者。原有同会话 resume 链在未转移前保持原有语义。

