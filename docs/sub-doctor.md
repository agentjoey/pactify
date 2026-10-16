# `pactify sub doctor` 自检 v1（AJ-626）

```sh
pactify sub doctor          # 检查全部已注册 harness
pactify sub doctor codex    # 只检查 codex
pactify sub doctor kimi     # 只检查 kimi
pactify sub doctor opencode --model minimax-coding-plan/MiniMax-M3
```

这条命令会真实调用 harness，消耗 API token。默认 codex 请求 `gpt-6-luna`，kimi 请求 `kimi-code/kimi-for-coding`；CLI 必须已经安装、完成登录，并能使用相应模型。codex 网络探测使用 `python3` 标准库，绑定 `127.0.0.1` 的随机端口并访问 `https://example.com`。缺少 Python 或外网不可达都会使网络检查失败。

`--model <id>`（AJ-681）仅允许与一个显式 harness 一起使用；没给 harness 或给了多个时退出 2。指定模型优先于 `Harness.DoctorModel` 和默认选模启发式，所有探测 job 均请求该模型。省略该 flag 时保留原有选模行为。

显式模型直接经过 `sub run --model` 使用的 `RunJob` 和 harness 后端校验，不先做 doctor 的默认选模列表探测。无效、不受支持或未被后端确认的模型会输出可读原因并使自检退出 1。动态模型表失败也遵循对应后端的既有行为：agy 的列表仅作预检，列表不可用时继续交由 CLI 拒绝无效模型，并核对 `init.model`；opencode/kimi 必须通过 ACP 的可用模型及设置确认，codex 必须确认实际模型，失败时尝试列出可用模型。显式选择不改变实际模型的独立核实要求，也不改变 `verified-versions.json` 的格式或更新条件。

模型报错中的可用候选最多显示 10 个：同 provider 前缀优先，再按模型名称的编辑距离由近到远排序；并列时保留原列表顺序。被截断时，末尾显示 `… and N more` 及完整列表入口：opencode 用 `opencode models`，agy 用 `agy models`，codex 用 `codex debug models`；kimi 启动 `kimi` 后进入 [`/model`](https://moonshotai.github.io/kimi-code/en/reference/slash-commands)。这项格式同时用于 `sub run`、新会话及续接的模型校验，不改变校验用的完整模型表。doctor 的只读探测如果返回与可写探测相同的错误，原因写为 `same as above (Write access)`；不同原因仍分别显示。

探测沿用 `sub run` 的默认配置策略：codex 以最小配置启动，关闭 MCP、插件、skills 和记忆；认证、provider、沙箱设置以及全局和仓库指令仍由原生 harness 加载。kimi 使用独立的 `KIMI_CODE_HOME=$PACTIFY_HOME/harness-homes/kimi`，目录权限 0700，不放 `mcp.json`。`config.toml`、`credentials/`、`device_id` 只用符号链接指向 pactify 进程的 `KIMI_CODE_HOME`（未设置时为 `~/.kimi-code`）；不读取或复制内容，缺少原文件时提示先 `kimi login`。每次启动修复链接，结束时若发现链接变成普通文件或目录，删除副本并在结果中警告；下次启动重建链接。共享目录长期保留以支持续接。普通 job 可用 `--with-user-config` 恢复全部用户集成；doctor 始终验证默认路径。

codex 输出六项「PASS / FAIL / SKIP + reason」：安装及版本、可写档（文件、最终回复、token）、独立核实的实际模型、只读档、网络、不留痕。codex 只读档必须实际调用工具、正常结束且没有留下改动；kimi 沿用注册表的 `NoReadOnly` 原因跳过。网络仅检查 codex 可写档，需要脚本生成的本地绑定和 HTTPS 成功证据，单凭最终回复不能通过。所有执行结果的 CLI 版本也必须与探测开始时的版本一致。

codex 的只读探测明确要求通过 shell 工具执行 `sh -c 'echo hi > out.txt'; echo EXIT=$?`，并原样报告 stdout、stderr 与退出码。写入由沙箱决定是否拦截；即使最终回复声称被拦，只要没有工具调用，仍判为失败。

kimi 额外输出第七项 `Managed home`：检查共享目录及三个链接正确、探测 job 成功且记录的 home 是共享目录、探测 session id 不在用户 home 的 `session_index.jsonl` 中，并读取共享 home 对应的 `wire.jsonl`，确认没有 `mcp.tools_discovered` 事件。缺少会话或 wire 证据、索引不可读或损坏均不能通过。CLI 不可用时该项 SKIP。

仓库和 job 均放在一次性临时目录；传给子进程的 `PACTIFY_HOME` 指向临时 home，不改变当前进程的环境。构建缓存单独使用调用者 `$PACTIFY_HOME/cache`，不随探测销毁。kimi 的 `KIMI_CODE_HOME` 仍指向调用者 `$PACTIFY_HOME` 下的长期共享目录，探测会话保留在那里；最小配置无需启动 `kimi web` 或归档。codex 继续走普通 job 的 `Hidden: true` 归档流程。正常结束、检查失败和可处理的取消之后都会移除临时仓库和 job；不要通过 `SIGKILL` 结束进程。

普通 kimi job 的 `--with-user-config` 使用用户 home 并沿用归档；doctor 始终探测 minimal config。job 记录实际 home，模型/token 核实和续接沿用它。kimi 续接必须保持原配置模式，旧 job 缺少 `configMode` 时仍按 user config 处理，需使用 `--with-user-config`；切换模式请新建会话。

某个 harness 全部适用检查通过后，才原子更新 `$PACTIFY_HOME/verified-versions.json`（未设 home 时为 `~/.pactify/verified-versions.json`），同时保留其他 harness 的条目。每个 harness 保存最近一次通过的版本与 UTC 时间；并发更新通过旁边的 `.lock` 文件串行化。失败的探测不覆盖先前记录。写表失败会输出错误并退出 1。

```json
{
  "codex": {
    "version": "0.162.1",
    "verifiedAt": "2026-10-15T00:00:00Z"
  }
}
```

普通 job 在版本表缺失、损坏、缺少时间或版本不匹配时输出`has not been checked` 提示；它不改变任务状态，也不写入 `State.Error`。表中版本匹配且时间有效才免提示。表本身不证明当前登录、网络或模型服务仍然可用。

退出码：全部适用检查通过为 0，有检查或基础设施失败为 1，未知 harness、多余参数或错误 flag 为 2。

## 共享缓存与磁盘检查（AJ-675）

codex、kimi、agy 和 opencode 的 job 默认共用以下目录；`PACTIFY_HOME` 未设置时使用 `~/.pactify`：

| 子进程环境变量 | 默认目录 |
|---|---|
| `GOCACHE` | `$PACTIFY_HOME/cache/go-build` |
| `GOMODCACHE` | `$PACTIFY_HOME/cache/go-mod` |
| `npm_config_cache` | `$PACTIFY_HOME/cache/npm` |

用户显式设置的非空变量优先保留，包括 `GOCACHE=off`；未设小写 `npm_config_cache` 时也保留大写 `NPM_CONFIG_CACHE` 的值。Codex 可写档会检查显式目录能否创建临时文件；能写则把该目录加入本次会话的 writable roots，不能写则改用共享缓存，并在结果 `Warnings` 中提示。检测文件立即删除，不修改已有缓存文件或 Go module 的只读权限。其他 harness 保留显式变量；Codex 只读档不增加 writable roots。配置只传给子进程和本次会话，用户全局配置与默认 Go 缓存内容不变。

每个 job 单独使用 `TMPDIR=$PACTIFY_HOME/jobs/<id>/tmp`，在会话关闭后清理；孤儿恢复和取消兜底会先停止已登记的 harness，再清理。只有成功新建并在 job state 中记录归属的目录才会删除；预先存在但归属未知的目录不接管。`sub discard` 会在确认共用 worktree 的所有 job 均停止后，清理这些 job 留下的临时目录，不清理共享缓存或其他 job。临时目录内的符号链接不会导致递归删除链接目标。不要在 job 的专属 TMPDIR 中保存需要保留的文件。

`sub doctor` 在各 harness 的检查前输出单独的 `Storage` 检查：`PACTIFY_HOME` 所在卷可用空间低于 5 GiB 时为 **WARN**，并报告共享缓存目录下普通文件的大小总和（不跟随内部符号链接）。空间或缓存大小无法读取也为 WARN；这些提示不导致 doctor 失败，不影响 harness 版本自检记录。home 或缓存尚不存在时，检查已有父目录所在卷并报告缓存为零，不创建 job。

停止使用这些缓存的 job 和其他进程后，可删除共享缓存；先用 Go 清理含只读目录的 module cache，再删除其余缓存。后续 job 会重新创建并下载依赖：

```sh
GOMODCACHE="${PACTIFY_HOME:-$HOME/.pactify}/cache/go-mod" go clean -modcache
rm -rf -- "${PACTIFY_HOME:-$HOME/.pactify}/cache"
```

只清理 Go 构建缓存时，明确指定 Pactify 的目录：

```sh
GOCACHE="${PACTIFY_HOME:-$HOME/.pactify}/cache/go-build" go clean -cache
```

用户自行指定的缓存不在上述清理范围。不会扫描或删除历史上任意命名的 `/private/tmp/pactify-*-go-cache`，因为无法可靠判断其归属。

## 兼容行为与依赖审查

实施前检查了以下调用方和例外：

- `cmd/pactify/cmd_sub.go` 的 detached worker 调用 `RunJob`，doctor 复用同一路径；`subSaveFallback` 的 panic/强制取消结果保持原有行为。
- `internal/sub/worker.go` 原有 CLI 版本变化警告仍在新提示之前，保留跨仓库历史的展示顺序；`State.Error` 继续保留真实失败，仅额外跳过本次生成的自检提示。
- `internal/sub/harness.go` 的 `Experimental`、`NoReadOnly`、`HidesItself` / `Conceal` 均由注册表驱动；kimi 的实验性提示与自检提示共存。原有会话归档失败、隐藏失败警告继续暴露，并使 doctor 失败。
- `Result.Markdown` / 持久化 JSON 继续消费 `Warnings`，不新增状态或更改结果格式。
- `fake` 是 `PACTIFY_SUB_FAKE=1` 才可用的保留夹具，不代表实际安装的 CLI，因此不提示版本未自检，也不要求 PATH 上有 fake。补充其隐藏声明、工具调用与 token 事件，使 CLI 测试能走完整 doctor 流程；默认 harness 仍为 codex。
- `docs/specs/harness-subagents.md` §11 与 Phase 0 计划的临时 home、会话隐藏和 kimi 只读例外继续适用。

## 验证范围

回归测试覆盖基础六项及 kimi Managed home 检查的成功与失败、安装/版本失败时跳过执行、版本漂移、临时仓库/job 清理、取消、失败不覆写记录、损坏表恢复、并发更新、写表失败、两种信息提示共存且保留真实错误，以及 CLI 的 0/1/2 退出码。CLI 测试运行构建出的真实二进制；harness 使用 fake 或注入后端。Python 网络脚本用本地 HTTP 服务验证成功与非 200 失败，不访问外网。

未执行真实 codex/kimi 会话，因此上述测试不能作为当前厂商 CLI、模型、登录或实际外网的验收结论。

## agy（AJ-649）

`pactify sub doctor agy` 沿用七项检查：安装、可写、实际模型、只读、网络（SKIP）、会话隐藏、受管目录。
从 `agy models` 的动态表选择第一个模型作为探测模型；列表没有价格信息，因此不宣称它是最便宜的模型。
模型列表和任务进程均过滤其他厂商的 API key，使用受管 HOME。
实际模型取 `init.model`；输入 token 为 `input_tokens + cache_read_tokens`，总 token 按实际输入加输出计算，不用步骤用量重复累加最终结果。
命令行后端遵循 [官方 stdin 协议](https://www.agy.dev/docs/cli/headless/)，每轮一个进程，通过 `--conversation` 续接。
可写档用 `--dangerously-skip-permissions`，只读档用 `--mode=plan`，绝不加 `--dangerously-skip-permissions`，不使用 `--sandbox`。
无头权限自动拒绝即使返回 SUCCESS，普通 job 仍判失败，因为本轮没有可用终稿。
只读档只能使用自带文件工具；任何 shell 命令（包括 `git log`、`grep`）或写入都需要授权，
会被自动拒绝并直接结束本轮。只读 brief 不应要求执行命令或测试；需要命令时使用 codex
`--read-only`，或在获得可写授权后使用 agy `--write`。保护来自无头模式的自动拒绝，
不是 plan 模式：plan 加 skip-permissions 时写入仍会执行。

agy 的 doctor 继续要求实际尝试 shell 写入。只读检查仅在版本一致、目标 `out.txt` 不存在、
工作区无改动，且后端明确报告只读权限拒绝时通过；不要求该探测 job 成功或给出终稿。
普通失败、取消、只在终稿中声称被拒、空回复的成功均不能作为拒绝证据。
Session hiding 和 Managed home 使用可写探测的结果，不依赖只读探测是否成功。
取消在 SIGINT 前记录全部后代进程（包括独立进程组/会话），主进程退出或宽限结束后，
对仍存活的后代逐个先 SIGTERM 再 SIGKILL，每次核对出生身份；正常结束不处理后代。

默认 `HOME=$PACTIFY_HOME/harness-homes/agy`（0700）。真实 HOME 顶层项逐一链接，唯独排除 `.gemini`；
受管 `.gemini/antigravity-cli/antigravity-oauth-token` 只链接到原文件，不读取或复制凭据。
启动前在锁内补链接、修链接并删除已消失的镜像项；结束后删除替换成副本的链接并警告。
公共组件拒绝受管根目录与嵌套父路径上的符号链接，删除操作使用 `os.Root` 限制在受管树内。
会话数据库、protobuf 缓存和 brain 文件留在受管目录，保持续接所需记录。
`--with-user-config` 恢复真实 HOME；没有受支持的 agy 归档接口，因此结果明确警告会话在用户列表中可见。
续接沿用记录的 HOME，并要求原配置模式。

受管目录检查要求可写探测成功且使用共享 minimal home、链接完整、私有会话目录是真实目录、
`conversation_summaries.db` 是私有普通文件；比较探测前后的用户数据库元数据确认索引未变。
其他终端同时使用 agy 可能使此检查保守地失败，静止后重跑即可。全部适用检查通过才登记本机版本表。

| 适配版本 | 证据与范围 |
|---|---|
| agy 1.3.5 | Owner 提供的 host 实测：权限、取消、动态模型、stream-json、续接、镜像 HOME 登录与会话隔离；本变更只使用假 HOME / 假 agy 自动测试，完整真机 doctor 由 host 执行后写入本机版本表 |


## opencode（AJ-652，实验性）

`pactify sub doctor opencode` 输出七项：安装、可写、独立模型核实、只读、网络（SKIP）、
会话隐藏、受管目录。opencode 是 experimental harness（evaluation pending），评估完成前
优先使用默认 codex。适配版本为 1.19.0；本变更的自动测试全部使用假 opencode 和假 HOME/XDG。
下列 1.19.0 行为由 Owner 提供的 host 实测确认；完整真机 doctor 由 host 运行，通过后才写本机版本表。

模型从 ACP `session/new.configOptions` 动态获取。默认优先选择 id 包含 `-free`（包括以它结尾）的模型：
先取其中第一个含 `flash` / `mini` / `lite` / `nano` 的模型；没有这些轻量名称时，取第一个 free 模型。
例如列表同时包含 `opencode/deepseek-v4-flash` 和 `opencode/deepseek-v4.1-flash-free` 时，选择后者。
没有 free 模型时保留原回退顺序：第一个含 `flash` 的模型、第一个含 `mini` / `lite` / `nano` / `haiku`
的模型、第一个不含 `pro` 的模型，最后才退回列表首项。名称匹配不区分大小写；同一档保留 live 列表顺序。
这是基于名称的选择规则，不是价格保证，也不写死模型 id。
使用 `--model <provider/model>` 时跳过这套默认选择，由普通 job 的 ACP 后端校验并确认指定模型。
普通 job 不指定模型时保留 opencode 默认值，指定模型经 `session/set_config_option` 设置并确认。

`usage_update.cost` 是会话累计成本（[上游实现](https://github.com/anomalyco/opencode/blob/v1.19.0/packages/opencode/src/acp/usage.ts)）。
结束前用同一工作目录和环境执行 `opencode export <sid>`，独立读取 assistant 的 providerID/modelID；
成功、失败、取消或空闲超时均按本 job 新增 assistant 消息加总：输入为
`tokens.input + tokens.cache.read`，输出为 `tokens.output + tokens.reasoning`，成本为 cost 之和。
续接前记录导出消息边界，历史消息的 token 和 cost 均不计入本 job。
导出失败、历史边界失效或没有新 assistant 时明确警告；没有可用的本 job token 记录时，
退回 prompt 返回的 `inputTokens + cachedReadTokens`、`outputTokens`，并标记 `usagePartial`。
部分 assistant 缺少 token 记录时保留 export 中已记录的合计并标记 partial；
不能核实的成本不冒充本轮费用。异常结束也保守标记 `usagePartial`，提示可能缺少最后一段。

可写档用 build 配合 `OPENCODE_CONFIG_CONTENT` 的 edit/bash/webfetch allow。
只读档仍用 build，但注入 edit/webfetch deny、bash ask，并对所有权限请求选 reject_once；
plan 模式只是模型自觉，不构成只读保护。只读只能用读工具，不能运行任何 shell 命令。
拒绝后终稿为空时 job 失败并说明限制。doctor 主动尝试 shell 写入，只在后端有真实拒绝证据、
目标不存在且无其他 diff、版本一致时通过；不以模型终稿的声称为证据。

两个镜像复用受管目录组件：`XDG_DATA_HOME=$PACTIFY_HOME/harness-homes/opencode/share`
镜像原 `$XDG_DATA_HOME`（默认 `~/.local/share`）；`XDG_CONFIG_HOME=…/opencode/config`
镜像原 `$XDG_CONFIG_HOME`（默认 `~/.config`）。两者都排除 `opencode`，数据镜像仅显式链接
`opencode/auth.json`。不读取、打印或复制 auth 内容。其余 XDG 项保持链接，供 git 等工具使用。
缺失的用户配置目录按空目录处理，不创建原目录。每次启动刷新镜像，结束时清理被替换的链接副本。
最小配置另设 `OPENCODE_DISABLE_CLAUDE_CODE=1`、`OPENCODE_DISABLE_EXTERNAL_SKILLS=1`、
`OPENCODE_PURE=1`，host 空任务总 token 实测 18,930 → 7,690。

受管目录检查要求成功的可写探测、两个目录路径和权限、镜像链接、私有 opencode 目录与普通
`opencode.db`，并比较用户 auth/db/WAL/SHM 的元数据确认未变；不读取凭据或用户数据库内容。
已知厂商 key 在 sub 的运行、模型列表、版本与 export 进程中清空；公共 CrossVendorStrip 的
多提供方例外保持原样。会话隐藏与受管目录使用可写探测证据。`--with-user-config` 不隔离，
并警告会话进入用户列表；续接必须保持原配置模式和两个记录的目录。

| 适配版本 | 证据与范围 |
|---|---|
| opencode 1.19.0 | Owner host 实测 ACP 配置、权限、token/cost、export、取消、续接、XDG 隔离和最小配置；本地自动测试为假进程，完整真机 doctor 待 host 执行 |
