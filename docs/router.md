# Harness 路由

`pactify sub run` 可通过 `$PACTIFY_HOME/router.json`（默认 `~/.pactify/router.json`）配置别名与额度回退。文件不存在时，保持原行为：默认 codex，不做额度选路和自动回退。配置错误会指出文件与字段，退出码为 2，不创建 job。

```json
{
  "defaults": {
    "codex": {"model": "gpt-6.1-sol", "effort": "high"}
  },
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

四个字段均可省略。`defaults` 按 harness 指定默认 model / effort，两个字段也都可省略；harness 必须已知。有固定 effort 取值约束的 harness 会在保存时校验；动态选项仍由后端在 session 设置时确认。别名名只用字母、数字、下划线和连字符，首字符为字母或数字，配置中的名字不带 `@`。别名必须指定已支持的 harness，model、effort 可省略；模型与 effort 的可用性仍由 harness 检查。不要直接复制不支持 effort 的模型设置。

```sh
printf '%s\n' '更新安装说明，检查链接' | pactify sub run @cheap --worktree
printf '%s\n' '实现任务' | pactify sub run @mm --model another-model --effort high
pactify sub router
pactify sub router --json
```

模型和 effort 分别按 `--model` / `--effort` 显式参数 > `@alias` > `defaults[harness]` > harness 自身默认选取。显式参数也可覆盖为空值；别名省略的字段继承 defaults。不存在的别名会列出可用名字。Claude Code 支持 `/delegate @cheap <任务>`；适合小型文档等低风险写任务，复杂代码和只读任务优先 codex。别名必须先在本机配置。

## 启动选路

1. 显式 harness / `@alias` 是首选，省略时用 codex。
2. fallback 的键是 harness，值是有序路线列表，每项为 harness 名或 `@alias`。它是该首选 harness 的完整链，不递归拼接其他 harness 的 fallback；相同路线去重。
3. 每条路线依次检查 `quota.json` 中的 harness+model、整个 harness、所属 quotaGroup。只要有一项尚未重置，就跳过该路线。CLI 的 model/effort 覆盖只作用于首选路线；回退路线使用自身别名与 defaults 设置。
4. quotaGroup 中任意 model 耗尽，整个组都耗尽，避免在共用额度的模型档位间反复失败。组限定在同一个 harness 内；过期记录不起作用。无重置时间的额度记录按 R1 的 24 小时失效规则处理。
5. 全部耗尽则退出 2，显示最早可恢复的时间，不创建 job。一个路线同时受多个耗尽条件约束时，要等所有条件重置。
6. 只读任务仅保留 codex 路线；显式指定不支持只读的 harness 仍报原来的访问档错误。

改选时输出和最终 result.warnings 会保留`quota exhausted for X; using Y`，已知重置时间时附加 `(resets in 6d 18h)`。`sub router` 默认按 Config path、Defaults、Aliases、Fallback、Quota groups、Exhausted routes 分段展示；已知重置时间同时显示倒计时和 UTC 时间，未知时标明记忆过期时间。`--json` 保持原有字段，并提供展开后的 `resolvedFallback` 列表。运行中的 job 保存剩余路线与 quotaGroups；修改配置只影响后续启动。

`--resume` 继续原 harness 的原会话，继承所选原 job 的 model 和 effort；只有显式 `--model` / `--effort`（包括显式空值）才能分别覆盖，别名和 router defaults 不参与续接模型选择。不按额度静默切换会话。换 harness 请使用下面的 `--worktree-from`。

## 运行中回退

只有 `quota` 失败才考虑自动回退，transient 仍使用同会话 retries，其他错误与取消不回退。回退前先结束旧 harness 并记录额度，再过滤已耗尽和不支持只读的路线。

- 相对原基线没有改动：创建新 job，用同一份 brief、访问档、用户配置选择与 retries 设置，开启下一条路线的新会话。模型与 effort 使用新路线自己的值。工作区用现有 stash 快照基线，无 HEAD 时用临时 index tree；worktree 用原 base commit。无法读取基线或 diff 时不自动回退。
- 已有改动：保留失败结果，提示 `review the diff, then: pactify sub run <next> --worktree-from <job>`。先评审完整 diff 和未跟踪文件，再把完整任务说明通过 stdin 传入新会话。没有 worktree 的旧式工作区 job 不能使用此参数，需要评审后自行启动工作区任务。
- 无可用后继：停在最后的失败结果，给出耗尽或访问档提示。

旧 job 记录 `fallbackTo`，新 job 记录 `fallbackFrom`。`sub wait <第一个 jobId>` 和 `sub result <第一个 jobId>` 沿链读取最终 job；返回结果的 `jobId` 是最终 id。`sub cancel` 也沿链取消当前尝试。status、日志和历史保留每次尝试，不把失败伪装成成功。`/jobs`、status 显示两个方向的链接；Jobs 看板将同链合并为一张卡片，详情抽屉可切换各轮；`sub cost` 的 FALLBACKS 统计带 `fallbackFrom` 的 job。

## 在原 worktree 开新会话

```sh
printf '%s\n' '已评审上次的 diff。保留已有实现，完成剩余验收：…' |
  pactify sub run @mm --worktree-from 20261016-120000-abcdef --wait 0s --json
```

源 job 必须已结束，worktree 必须存在、未 discard、没有正在运行的共享会话。此参数与 `--resume` 互斥；可同时带 `--worktree`，无需指定。源 id 支持现有的唯一前缀规则。

新 job 接管现有 worktree，保留路径和原基线，不加载旧 harness 的 session 或 home。旧 id 再次启动、apply 或 discard 会提示当前持有者；后续请用新 id。内部 `worktree.owner` 继续作为共享锁和分组标识，`worktree.holder` 表示转移后的当前持有者。原有同会话 resume 链在未转移前保持原有语义。


## Jobs 页管理路由（AJ-700）

Jobs 的二级 **Routing** 页签显示配置路径、别名、按 harness 分组的有序回退链、额度组和当前耗尽记录。页签打开后每 30 秒刷新；编辑期间保留原配置版本，避免自动刷新覆盖草稿。已知 resetAt 显示重置时间与倒计时；未知时间显示 `resets in unknown`，不把 24 小时记忆过期当作供应商重置。

dashboard 中点 **Edit** 可增删改别名、回退路线和额度组，回退路线支持上下调整顺序，组中的模型逐项编辑。**Save** 使用 CLI 相同的解析器与校验规则；422 在对应字段旁显示错误，409 显示 `Config changed elsewhere — reload to see the latest`，点 **Reload** 丢弃草稿并加载最新配置。**Cancel** 丢弃编辑。配置变更只影响后续任务。

耗尽记录旁的 **Clear** 需在页面内确认，仅删除本地 quota.json 中该 key 的记录，不会重置供应商额度。同组其他模型仍有耗尽记录时，组约束仍有效。panel 只读，不显示编辑或 Clear 控件；提示 `Edit routing from the dashboard or with pactify sub router.`。CLI 的 `sub router` 用于查看；也可直接编辑配置文件。

| 操作 | serve | panel |
| --- | --- | --- |
| 读取 | `GET /api/sub/router` | `GET /api/router`（Bearer） |
| 保存 | `PUT /api/sub/router` | 不注册 |
| 清除额度 | `DELETE /api/sub/quota/{key}` | 不注册 |

GET 返回 `{path, exists, config, version, exhaustedRoutes}`。缺失文件返回 `exists:false`、四个空配置对象；version 始终为文件原始内容的 SHA-256（缺失时为空字节的 SHA-256）。耗尽记录保留原有 QuotaRoute 字段并增加实际存储 `key`，过滤过期记录；模型路线 key 可为 `harness/model`，组 key 同样按原值删除。URL 中的 key 要用 `encodeURIComponent` 编码。

PUT 仅接受 `application/json`（可带 charset），请求为 `{version, config}`，请求体上限 64 KiB；非 JSON 返回 415，超限返回 413，请求信封或 JSON 格式错误返回 400，配置错误返回 `422 {errors:[{field,message}]}`，版本冲突返回 409。只写 serve 进程的 `$PACTIFY_HOME/router.json`，不接受请求指定路径。版本比较和写入在 `router.json.lock` 下完成，复用 lockx；临时文件加 rename 原子替换，文件权限 0600。DELETE 与既有额度写入共用 `quota.json.lock`，原子更新；不存在的 key 也成功返回。两个写接口均经过 serve Origin 校验，经反向代理编辑时需按现有规则设置 `PACTIFY_ALLOWED_ORIGINS`。CLI 与 cost API 的既有 JSON 契约不变。


## Web v2 后端接口（AJ-704）

`GET /api/sub/harnesses`（panel 为 `GET /api/harnesses`，需 Bearer）返回
`{harnesses:[{name,installed,version,verifiedVersion,experimental,readOnly,models:{items,error,fetchedAt},default:{model,effort},efforts}]}`。
`default` 优先显示 router 中配置的值；kimi 未配置 router 默认模型时显示受管 config.toml 的 `default_model`，其他缺省字段为空字符串。`efforts` 优先返回 codex `model/list` 广告的各模型 reasoning effort 去重并集；没有这些元数据时仍使用 harness 的固定约束（无固定枚举则为 `[]`）。并集不表示每个模型都支持所有档位，最终由所选模型确认，router 层不硬编码动态枚举。

模型发现复用 `Harness.Models` 及同源的公开元数据发现函数：

- codex 复用最小配置 app-server 启动与 initialize/initialized 握手，分页调用 `model/list` 后关闭进程；不创建或续接 thread，也不发送 prompt / turn，总超时上限 20 秒。
- kimi 优先只读 `$PACTIFY_HOME/harness-homes/kimi/config.toml` 中链接进来的配置（job 子进程的 `KIMI_CODE_HOME`）。链接缺失时读取 serve / CLI 环境中 `KIMI_CODE_HOME/config.toml`，未设置则读取 `~/.kimi-code/config.toml`。使用已有 `go-toml/v2` 白名单解码，只保留 models 表的键名和 default_model；不读取 credentials/device_id，不解码 providers、oauth、key 等字段，不启动 kimi 会话。配置不可读、格式错误或模型表为空时返回英文原因；解析错误不带配置原文。接口不创建受管目录或自动准备认证链接。
- agy 仅执行 `agy models --model gemini-3.8-flash-low`；opencode 启动隔离的 serve，只读取 v1 `/config/providers`，随后回收进程组。

以上路径不执行 doctor 的计费探测。doctor 保留 codex/kimi 的固定探测模型，不因新增发现能力改选模型。installed 使用 LookPath，version 使用现有 Version，verifiedVersion 读取当前 PACTIFY_HOME 的 verified-versions.json。

模型列表在进程内缓存 10 分钟，`?refresh=1` 强制刷新。发现失败返回英文 error 并保留上次成功的 items、efforts 和原生默认模型；失败尝试也缓存 10 分钟，fetchedAt 表示最近发现尝试时间。未安装的 harness 不执行版本或发现命令。

serve 新增以下 POST（panel 不注册）：

| 路径 | 请求与结果 |
| --- | --- |
| `/api/sub/jobs/{id}/cancel` | `{}`；沿 fallback 链取消当前尝试，成功返回 `{ok:true}` |
| `/api/sub/jobs/{id}/resume` | `{brief:"..."}`；后台启动新 job，返回 `{jobId:"..."}` |
| `/api/sub/jobs/{id}/apply` | `{}`；安全应用到原工作区，成功返回 `{ok:true}` |
| `/api/sub/jobs/{id}/discard` | `{}`；清理 worktree，成功返回 `{ok:true}` |

四个接口沿用 Origin 校验，仅接受 `application/json`（可带 charset），请求体上限 64 KiB。非 JSON 为 415，超限为 413，非法 JSON 或空白 brief 为 400，job 不存在为 404，状态不允许及 apply 冲突为 409；错误统一为 `{error:"English message"}`，不会强制写入冲突文件。cancel / apply / discard 可省略请求体。

resume 通过 CLI 共用的 ResumeRun 和 worker 启动逻辑复用 session、worktree 及其所有权检查，并继承原 job 的 harness、访问档、model、effort、配置模式与 retries。原始 cwd、harness、session 决定跨进程锁，锁内检查有无活跃 turn 并创建新 job；无 worktree 的任务也受保护。brief 只存为 job 数据，worker 命令参数只有 job id。HTTP 不等待任务运行完成。serve 使用 StartDashboardWorker 在后台 Wait 回收子进程，CLI 保持 Release 行为。

同一个 store 最多保留 4 个由 dashboard 发起的 queued / running / retrying job；超出返回 429，错误为 `too many jobs started from the dashboard are still running (limit 4)`。CLI 发起的 job 不占此额度；完成、取消或启动失败后释放额度。跨 serve 进程的检查与 queued job 创建使用同一把 store 锁。

summary 新增始终存在的 `actions` 数组；列表和详情都使用同一套只读状态判断。未结束任务可 cancel；只有链上最新已结束且有 session 的 turn 可提供 resume，并且整链不能有 queued / running / retrying job。worktree 未转移、无共享运行任务时可 apply / discard；apply 后符合续接条件时仍可 resume 或 discard，但不能重复 apply；discard 后不再允许复用。列表复用已加载的 state/result 快照，一次建立 session、worktree 与 resumedBy 索引，不为每行扫描 store。worktree 目录被外部删除时保留 discard，以清理 git 注册。旧 CLI 对终态 cancel 的清理能力及显式选择旧已结束 turn 的能力保持不变，但同会话已有活跃 turn 时禁止并发 resume；HTTP 对终态 cancel 返回 409。

### 沙箱能力（AJ-710）

`GET /api/sub/harnesses` 每行总是包含布尔字段 `sandboxed`，codex 为 true，其余为 false。
opencode 的 `readOnly` 为 true：使用 plan 并在最终 diff 非空时失败，不代表 OS 沙箱。
模型发现沿用 10 分钟缓存；免费模型仅按 id 的 `-free` 后缀识别，不能依据 cost=0。
Doctor 继续优先选择 free 模型，再按 flash/mini/lite/nano 名称分档。
