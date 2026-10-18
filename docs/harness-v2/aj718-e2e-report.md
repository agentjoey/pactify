# AJ-718：harness 端到端集成测试

日期：2026-10-17。**有效结果是下面「沙箱外实跑」一节**：由 host 在两轮返工、独立复审通过后，于沙箱外完成。后面各节是 codex 首轮（在外层沙箱内）和两轮返工的历史记录，只作为过程留档。

## opencode 2.x（AJ-720，host，2026-10-18）

opencode 2.0.32，模型 `opencode/deepseek-v4.1-flash-free`，持久的 `PACTIFY_HOME=~/.pactify-e2e/opencode2`，`PACTIFY_E2E_HOST=1`。**13/14 PASS**：唯一的失败是 `readonly_long`，在第 41 次调用时被 40 次的只读上限截断，与其他各家相同。1.x 是 12/14。
- **完整首轮**：10 PASS。
- **失败的 3 项**：都在定位原因并修复后重跑通过。
  - `long_tool`：SSE 心跳没有重置不活动计时；
  - `instructions`：`OPENCODE_DISABLE_PROJECT_CONFIG` 连带关掉了 AGENTS.md；
  - `user_dirs_clean`：监测范围过宽，用户自己常驻的 opencode 1.19 server 会刷新 cache 和 state 目录。
- **调整后重跑**：`model` 和 `failure_surface` 的判定口径收紧后也重跑通过。
- **`readonly_denied` 在 2.x 上 PASS**：只读会话里写入工具被移除，并经权限求值确认。
- 原始汇总：`aj718-e2e-results/host-opencode2*.json`。细节见 [`aj720-opencode2.md`](aj720-opencode2.md)。

## 沙箱外实跑（host，2026-10-17 15:52–16:18，有效结果）

> 源码：AJ-718 worktree（基线 `53a9d02`，加两轮返工和 host 的两处套件修正，见下）· 每家使用持久的 `PACTIFY_HOME=~/.pactify-e2e/<harness>`，被替换的凭据隔离区因此不会落入临时目录 · `PACTIFY_E2E_HOST=1` · 模型：codex 用默认（实际 `gpt-6.1-sol`），opencode 用 `minimax-coding-plan/MiniMax-M3`，agy 用 `gemini-3.8-flash`，host 用 Claude Code haiku · 原始汇总见 `aj718-e2e-results/host-*.json`。

| # | Scenario | codex | opencode | agy | kimi |
|---|---|---|---|---|---|
| 1 | `write_worktree` | PASS | PASS | PASS | PASS |
| 2 | `readonly_denied` | **部分**：文件写入被沙箱拒绝有证据，仓库不变；shell 写入也被拦下（模型答复中的工具输出为 `operation not permitted`），但 codex 上游没有为这次调用发出任何条目，pactify 拿不到证据 | **FAIL（模型未尝试）**：plan 模式的系统提醒让 MiniMax-M3 直接口头拒绝，没有发出工具调用；deny 规则本身由 AJ-713 的 `opencode-readonly` golden 证明 | PASS（shell 和文件工具都有被拒记录） | N/A：kimi ACP 不能强制只读，CLI 以退出码 2 明确拒绝 |
| 3 | `readonly_long` | **FAIL**：第 41 次调用被只读上限截断 | **FAIL**：同上 | **FAIL**：同上 | N/A：同上 |
| 4 | `resume` | PASS | PASS | PASS | PASS |
| 5 | `cancel` | PASS（9 个进程身份全部退出） | PASS（5 个） | PASS（4 个） | PASS（5 个） |
| 6 | `long_tool` | PASS | PASS | PASS | PASS（首轮 FAIL 为套件证据解析缺口，修正后重跑通过，见下） |
| 7 | `large_output` | PASS | PASS（原生截断标志已进入 warnings） | PASS | PASS |
| 8 | `parallel` | PASS | PASS | PASS | PASS |
| 9 | `model` | PASS | PASS | PASS | PASS |
| 10 | `failure_surface` | PASS | PASS | PASS | PASS |
| 11 | `records` | PASS | PASS | PASS | PASS（首轮 FAIL 为套件判定缺口，修正后重跑通过，见下） |
| 12 | `instructions` | PASS | PASS | PASS | PASS |
| 13 | `user_dirs_clean` | **不确定**：`~/.codex` 下 Codex 应用的全局状态和日志数据库有变化；套件给 codex 用的是私有 `CODEX_HOME`，同一时段用户的其他 codex 会话也在写这个目录，无法归因于测试 | PASS | PASS（agy 刷新了一次登录令牌，正确写回用户目录，没有进入隔离区） | PASS（kimi 刷新了登录令牌，按目录链接正确写回用户目录，没有进入隔离区） |
| 14 | `host_path` | PASS | PASS | PASS | PASS |

| Harness | PASS | FAIL | 其他 |
|---|---:|---:|---|
| codex | 11 | 1（只读上限） | `readonly_denied` 部分（上游不发条目）、`user_dirs_clean` 不确定 |
| opencode | 12 | 2（只读上限；模型未尝试写入） | — |
| agy | 13 | 1（只读上限） | — |
| kimi | 12 | 0 | `readonly_denied`、`readonly_long` 为 N/A（kimi 不提供只读，CLI 明确拒绝） |

### 结论

> **kimi 补跑（2026-10-18 00:02–00:16，Owner 重新登录后）**：kimi 2.2.0，模型 `kimi-code/k3-256k`，同样使用持久的 `PACTIFY_HOME=~/.pactify-e2e/kimi` 和 `PACTIFY_E2E_HOST=1`。首轮 10 PASS、2 N/A、2 FAIL。两项 FAIL 都是套件自己的判定缺口，pactify 本身没有问题。修正套件后单独重跑 `long_tool,records,user_dirs_clean`，全部通过，最终 12 PASS、2 N/A：
> - `long_tool`：kimi 2.2.0 的 `tool_call` 只带标题，命令参数以 in-progress 文本出现在 `tool_call_update` 里，结构化证据解析没有读这部分。通用的工具记录检查已经找到了 `sleep 240`。回归测试为 `TestLongToolKimiStreamedArgumentsAreCommandEvidence`。
> - `records`：kimi 的用量在回合结束后从本地 `wire.jsonl` 读取，事件日志里本来就没有 usage 行。套件原来把这种情况当成"用量捏造为零"。现在对有本地用量记录的 harness 改为要求结果里有真实 token。
> - kimi 在测试期间刷新了一次登录令牌，按目录链接写回了用户目录，没有进入隔离区，之后登录仍然有效。原始汇总见 `aj718-e2e-results/host-kimi*.json`。

- **集成机制在四家上都成立。** 写入与 worktree、续接、取消与进程清理、4 分钟长工具、5 MB 大输出、三路并发、模型兑现、失败分类、记录一致性、AGENTS.md、经 Claude Code 插件的 host 路径：codex、opencode、agy、kimi 全部通过（kimi 不提供只读，两项只读场景为 N/A）。
- **唯一共同的失败是只读调用上限。** 三家读 60 个文件都在第 41 次调用时被截断，codex 也不例外。这是 pactify 自己的限制（`cmd/pactify/cmd_sub.go` 的 `ToolCallLimit: 40`），调整方案待 Owner 决定。
- **codex 上游的可观测性缺口。** codex 0.162.1 的 app-server 对模型使用的统一执行工具（输出带 `chunk_id`、`wall_time_seconds`）不发任何 item。host 直接驱动 app-server 复现过：一次被沙箱拒绝的 shell 写入前后，只有两条 agentMessage。后果：这类调用不进 job 日志，也不计入工具调用次数，codex 的只读上限对它们不生效。写入仍由 OS 沙箱拦截，所以不是安全问题。
- **opencode 的只读拒写判不出集成好坏。** opencode 的只读靠 plan agent 加 deny 规则；plan 模式的提醒让模型根本不去尝试写入。deny 规则生效由 AJ-713 的 golden 回放证明。建议这一项对 opencode 以 golden 为准，不再要求实跑中出现被拒的调用。

### host 对套件的两处修正（本次实跑前）

1. **host 路径在 macOS 上保留用户真实 HOME。** macOS 按 HOME 查找钥匙串，私有 HOME 会让 Claude Code 找不到登录（用不计费的 `claude auth status --json` 对比过：私有 HOME 时 `loggedIn:false`，真实 HOME 加私有 `CLAUDE_CONFIG_DIR` 时为 `true`）。配置仍由 `CLAUDE_CONFIG_DIR` 隔离；明文凭据的检查路径在切换前计算，仍指向私有 HOME，不会碰用户真实的 `~/.claude`。回归测试 `TestHostEnvironmentKeepsUserHomeOnDarwin`。
2. **codex 的文件拒写证据也从 harness stderr 读取。** codex 的工具路由器在任何 item 开始之前就拒绝 `apply_patch`，拒绝只写在 stderr（`patch rejected: writing is blocked by read-only sandbox`）。新增 `stderrLines` 只给拒写判定使用；其他判定仍只读 JSON 事件。回归测试 `TestCodexRouterPatchRejectionCountsAsDeniedFile`（含一条无关路由错误的反例）。

## 环境和隔离（codex 首轮，历史）

- 基线 commit：`53a9d0266512b7296dde8dbd5ab543e1bf262eb8`，加本次未提交的 AJ-718 改动。各轮均重新构建二进制；未使用用户安装的 pactify。
- macOS 26.6.2 / arm64；Go `go1.26.1 darwin/arm64`。Claude Code `2.1.301`（host 请求 `haiku`，隔离配置未找到登录，未取得实际模型回报），插件版本 `0.2.6`，插件文件未修改。
- codex `0.162.1`：省略模型参数，实际回报 `gpt-6.1-sol`；opencode `1.19.0`：`minimax-coding-plan/MiniMax-M3`；agy `1.4.0`：`gemini-3.8-flash`。Kimi `2.2.0` 使用订阅默认配置，回报配置模型 `kimi-code/k3-256k`；授权失败，未验证实际模型执行。
- 每个场景使用新 git 仓库、`PACTIFY_HOME`、HOME、CODEX_HOME、KIMI_CODE_HOME 和 XDG 目录。续接两轮共用该场景的私有 session store。登录只用 managed symlink；未读取、打印或复制用户凭据内容。初轮使用私有 kimi credentials 目录逐文件链接；独立评审确认这会丢失轮换后的刷新令牌，现已修正，见“凭据刷新缺陷”。
- 按 codex → opencode → agy → kimi 串行；同一家内只有 `parallel` 同时启动三个 job。相关失败另行串行重跑，表格采用最近一次结果；原始汇总保留，不隐藏首次失败。
- 当前外层沙箱禁止 codex 再创建执行沙箱：实际工具报 `sandbox-exec: sandbox_apply: Operation not permitted`。没有关闭 codex 的沙箱来取得通过结果。
- 默认系统 TMPDIR 不可写，因此所有 Go 测试显式设置 `TMPDIR=/private/tmp`，并另设独立 `PACTIFY_HOME`。

## 初轮场景结果（历史，非返工后的验证结果）

| # | Scenario | codex | opencode | agy | kimi |
|---|---|---|---|---|---|
| 1 | `write_worktree` | 无效（外层沙箱）：FAIL：外层 sandbox 阻止工具，文件未落地 | PASS：隔离写入、原仓 clean、apply 和 discard 均通过 | PASS：隔离、两处写入、apply、discard 均通过 | 作废（凭据链接缺陷）：FAIL：授权 grant 失效，模型工具写入前失败 |
| 2 | `readonly_denied` | 无效（外层沙箱）：FAIL：未取得实际 shell / 文件工具拒写记录，不能只凭答复通过 | FAIL：仅口头解释只读策略，未实际尝试被拒写入 | PASS：实际 shell 和文件工具拒写记录均存在，job 成功 | 作废（凭据链接缺陷）：N/A：上游不提供可强制执行的只读能力，CLI 明确拒绝 |
| 3 | `readonly_long` | 无效（外层沙箱）：FAIL：工具调用为 0，未能读文件或给出正确答案 | FAIL：第 41 次调用触发原有 40 次上限 | FAIL：第 41 次调用触发原有 40 次上限 | 作废（凭据链接缺陷）：N/A：上游不提供可强制执行的只读能力，CLI 明确拒绝 |
| 4 | `resume` | 无效（外层沙箱）：FAIL：session / worktree 和记忆核对通过；两处文件写入受阻 | PASS：相同 session / worktree，正确回忆词并写入两文件 | PASS：session / worktree、记忆词和两文件均通过 | 作废（凭据链接缺陷）：FAIL：第一轮即 auth 失败，未进入续接验证 |
| 5 | `cancel` | 无效（外层沙箱）：FAIL：shell 未启动，未验证运行中取消 | PASS：OS 确认 sleep 后取消；6 条进程身份记录均已退出 | PASS：OS 确认 sleep 后取消；4 条进程身份记录均已退出 | 作废（凭据链接缺陷）：FAIL：auth 失败，sleep 未启动，未验证运行中取消 |
| 6 | `long_tool` | 无效（外层沙箱）：FAIL：长命令未执行，未取得完成输出 | PASS：240s 命令完成，无 transient 重试或空闲超时误判 | PASS：240s 命令完成，无 transient 重试或空闲超时误判 | 作废（凭据链接缺陷）：FAIL：auth 失败，未进入 240s 工具执行 |
| 7 | `large_output` | 无效（外层沙箱）：FAIL：大输出命令未执行，完成文件不存在 | PASS：5 MB 后正常结束；原生截断已写入 warnings | PASS：5 MB 压力命令后正常结束、有最终答复 | 作废（凭据链接缺陷）：FAIL：auth 失败，未进入大输出工具执行 |
| 8 | `parallel` | 无效（外层沙箱）：FAIL：worktree 独立，但目标文件写入受阻 | PASS：三个 worktree 独立，同名文件各自内容正确 | PASS：三个隔离 worktree 和各自内容均通过 | 作废（凭据链接缺陷）：FAIL：模型授权失败，未验证并行文件落地 |
| 9 | `model` | 无效（外层沙箱）：PASS：默认回报 gpt-6.1-sol；无效模型正确归为 model | PASS：请求 / 回报 MiniMax-M3 一致，无效模型正确归类 | PASS：回报 gemini-3.8-flash；无效模型现正确归为 model | 作废（凭据链接缺陷）：FAIL：默认及无效模型探针均先被 auth 阻塞，未核验模型 |
| 10 | `failure_surface` | 无效（外层沙箱）：PASS：无登录与缺少可执行文件两个负例均通过 | PASS：缺登录已归 auth，缺可执行文件快速失败 | PASS：无登录与缺可执行文件两个负例均通过 | 作废（凭据链接缺陷）：PASS：无登录与缺可执行文件两个负例均通过 |
| 11 | `records` | 无效（外层沙箱）：FAIL：可见账本字段一致；前置文件写入未落地 | PASS：status / result / logs / cost 的可见字段一致 | PASS：status / result / logs / cost 的可见字段一致 | 作废（凭据链接缺陷）：FAIL：授权阻塞成功写入与账本核对，失败 usage 标为 partial |
| 12 | `instructions` | 无效（外层沙箱）：PASS：最终答复含 AGENTS_MD_SEEN | PASS：最终答复含 AGENTS_MD_SEEN | PASS：AGENTS.md 与 GEMINI.md 同置；最终标记存在 | 作废（凭据链接缺陷）：FAIL：授权失败，无最终指令标记验证 |
| 13 | `user_dirs_clean` | 无效（外层沙箱）：FAIL：后轮有 4 处元数据变化，含主 agent 当前会话文件 | PASS：完整轮及重跑均无元数据变化 | PASS：完整轮及重跑均无元数据变化 | 作废（凭据链接缺陷）：PASS：未观察到全局目录元数据变化 |
| 14 | `host_path` | 无效（外层沙箱）：FAIL：Claude 返回 Not logged in，未进入 callee | FAIL：Claude 返回 Not logged in，未进入 callee | 未运行：未设置 PACTIFY_E2E_HOST=1 | 作废（凭据链接缺陷）：未运行：未设置 PACTIFY_E2E_HOST=1 |

| Harness | PASS | FAIL | N/A | 未运行 |
|---|---:|---:|---:|---:|
| codex（历史结果无效） | — | — | — | 待 host 沙箱外重跑 |
| opencode | 11 | 3 | 0 | 0 |
| agy | 12 | 1 | 0 | 1 |
| kimi（历史结果作废） | — | — | — | 待 Owner 登录后重跑 |

`failure_surface` 中额度耗尽是每家额外的 N/A 子项（covered by golden replay），不计入上面的整场景 N/A 数量。初轮把 Kimi 两个只读场景计为整项 N/A，但当时未执行拒绝探针；该成绩同列其他结果一起作废。修复后必须先验证 CLI 退出码 2 与明确的拒绝提示。

agy 在完整轮开始 / 结束时，Gemini 非 Argon 周额度回报 **92.33% / 90.18%**；model 重跑前后为 **90.67% / 90.18%**，均高于 20%，因此没有跳过。接口快照存在短时回升，不据此推算精确消耗。完整轮和重跑的 quota JSON 均保留。

最近一次 records 核对中，opencode 记录 input **40601** / output **338**，agy 为 **78574** / **949**，两者 usagePartial 均为 false；各 CLI 可见字段一致。Codex 账本可见字段核对未出现不一致，但场景因文件写入缺失仍失败。Kimi 成功任务用量未验证。

历史记录采用 [codex 完整轮](aj718-e2e-results/codex-full.json) + [重跑](aj718-e2e-results/codex-recheck.json)、[opencode 完整轮](aj718-e2e-results/opencode-full.json) + [重跑](aj718-e2e-results/opencode-recheck.json)、[agy 完整轮](aj718-e2e-results/agy-full.json) + [重跑](aj718-e2e-results/agy-recheck.json)，大输出补查采用 [opencode 修复后重跑](aj718-e2e-results/opencode-output-verified.json)、[agy 输出标记检查](aj718-e2e-results/agy-output.json)，并保留 [opencode 截断遗漏探针](aj718-e2e-results/opencode-output-probe.json)。另有 [opencode OS 进程核验重跑](aj718-e2e-results/opencode-cancel.json)、[agy OS 进程核验重跑](aj718-e2e-results/agy-cancel.json)，以及 [kimi 完整轮](aj718-e2e-results/kimi-full.json)。另保留 [codex 早期探针](aj718-e2e-results/codex-initial-probe.json) 和 [kimi 授权问题探针](aj718-e2e-results/kimi-records-probe.json)，不把探索轮当最终成绩。

表中的 N/A 只表示上游不支持。`host_path` 未启用时写“未运行”，不冒充 N/A；初轮该项对 codex、opencode 均实际调用了 Claude，均在登录阶段失败，未进入 callee，未验证到字节转交。

机器汇总位于 [aj718-e2e-results/](aj718-e2e-results/)。每条包含场景、状态、原因、job id 和耗时；agy 另含运行前后 `/usage` 原始结构的脱敏结果。各次生成的 JSON 均保留，历史表按场景合并当时的重跑结果（codex 无效、kimi 作废）。`codex-initial-probe.json` 是早期探针：修正了成功 stderr 混入 stdout、duration 的显示格式比较和失败原因格式后，另跑正式完整轮次；早期探针不参与最终表。环境受限、提示词不服从、调用上限均不归为上游不支持。

## 凭据刷新缺陷

独立评审从 kimi CLI 源码确认：`FileTokenStorage.save` 先写 `*.json.tmp.<pid>.<rand>`，再 rename 覆盖登录文件。逐文件 symlink 会被私有普通文件替换。刷新令牌轮换后，新令牌仅留在私有目录，测试清理再删除它，用户原文件中的旧刷新令牌已被服务端作废。用户全局文件的元数据可能完全不变，所以初轮 `user_dirs_clean` 的 PASS 不能证明凭据安全。

**影响：kimi 用户登录已经失效，需要 Owner 重新登录。AJ-713 录制 README 原先要求逐文件链接并删除私有替换文件，同样有此破坏性缺陷，已删除该指示。** 初轮 kimi 整列结果作废；不能归咎于 kimi 集成。本轮没有尝试登录、读取凭据或发起真实调用。

修复与边界（以第二轮返工后的实现为准）：

- kimi 继续链接整个 `credentials/` 和 `oauth/` 目录，和交互式会话共享刷新锁。`oauth/` 是上游按需创建的目录；源目录缺少时先以 0700 创建再链接，不再把它当作登录失效。E2E 登录继承同样确保共享源目录存在，并把合法的锁目录创建单列为元数据刷新活动。
- 回写资格由 `homeLink.Credential` 显式声明：opencode `auth.json`、agy `antigravity-oauth-token`、kimi `credentials/` 目录为凭据；kimi `config.toml`、`device_id`、`oauth/` 不走回写。配置的私有原子重写在 cleanup 或下一次 exclusive prepare 被丢弃，用户原配置保持原样。合法的配置规范化 warning 归 informational，不再让成功 job 的 State.Error 出现配置丢弃提示。Codex 生产适配器没有 managed-home 文件链接；E2E 的 `auth.json` 文件链接仍接受严格检查。
- 仅凭据文件保存源 canonical path、device/inode、size、mtime、mode 等元数据。源未变时先把私有副本 chmod 为源原权限，再 rename 回源；chmod 失败或源已变化则隔离并 warning，绝不删除刷新副本或放宽用户文件权限。模型发现的临时 home 复用同样的资格与清理规则。
- 生产默认隔离根改为 **`filepath.Dir(store.Root)/credential-quarantine`**，即 **`~/.pactify/credential-quarantine`**；`PACTIFY_HOME` 覆盖时随 store 放置。目录必须由当前用户持有且为 0700。不再把唯一有效的刷新令牌放到共享 `/tmp`，也不再让普通测试写入旧 `/tmp/pactify-credential-quarantine-<uid>`。
- E2E 明确使用外层测试进程的 `$PACTIFY_HOME/credential-quarantine`（未覆盖时同生产默认），位于每个 scenario fixture 之外，日志报告其绝对路径，fixture 不删除它。内层 job store 如产生 quarantine，在删除 fixture 前整目录 rename 到外层隔离根；失败则保留整个 fixture。codex、opencode、agy 文件链接被替换以及 Claude 在私有 HOME/.claude 或 host config 下生成明文 `.credentials.json` 回退文件，都会 FAIL 并保全；不打开其内容。
- 保留已知并发边界：元数据检查与 rename 之间，交互式 agy 仍可能原地刷新源文件。没有用平台 exchange syscall 宣称消除此窗口；它们不提供简单、可移植的 compare-and-replace 契约。代码注释和新增 `docs/backlog.md` 已登记。

隔离失败的恢复步骤：停止该 harness 的 pactify jobs 和交互式会话；保留错误消息指明的文件及完整 fixture，不要删除。修复隔离根的所有权、0700 权限及同卷 rename 条件后重试，prepare 会重新核对源元数据。如果需要人工恢复，由 Owner 决定将保留文件按源原权限 rename 回正确登录源，或 rename 到保留文件所在卷的私有持久 0700 隔离目录；源也发生过刷新时不要盲目覆盖。条件仍不满足则保留整个目录供恢复。上述步骤不需要读取、打印或复制凭据内容。英文错误消息和两份 README 均给出这些指引。

旧 `/tmp/pactify-credential-quarantine-501` 的历史隔离项保持原样；第二轮只做前后元数据比较，不打开或删除未知文件。第一轮留有合成文件的事实继续保留，不将其当作本轮写入。

## 第一轮返工修复与普通回归（历史）

| 项目 | 修复 | 回归与修改前验证 |
|---|---|---|
| B1 | 目录链接、共享刷新锁、元数据判定回写/隔离、fixture 保全、录制说明同步 | `TestManagedCredentialAtomicRefresh`（writeback/conflict）、`TestKimiRefreshLockDirectoryLinked` 修改前实际失败，修复后通过；`TestCredentialMetadataRefreshBoundaries` 修改前暴露临时文件/权限误豁免及锁活动误报。另加共享 lease、未清理刷新恢复和隔离保全测试 |
| S1 | 当时将 retry 改为日志事件，但错误地在终态设置服务端错误，第二轮已撤回终态改动 | `TestCodexRetryNotificationIsNotTerminalError` 保留；仅用 fake session 的 `TestWorkerCodexReconnectDoesNotPoisonQuota` 不能证明适配器过滤，第二轮删除，改由原始协议序列驱动真实适配器的回归覆盖 |
| S2 | `Opencode truncated tool output` 归 informational，保留 warning，不作为 State.Error | `TestWorkerTruncationWarningDoesNotBecomeError` 成功/失败两种修改前均实际失败 |
| S3 | 无凭据文件链接；darwin secure-storage 空值、Linux 指向用户 `.claude`；future fixture 先 auth status | `TestHostSecureStorageAndAuthPreflightFake` 使用自建 shell fake 验证环境、命令与未登录失败；未执行真实命令，也未对旧实跑路径做 red/green |
| S4 | 拒写按 native error 结构；长工具要求命令、工具输出与记录时长 ≥240s；kimi N/A 先实际 CLI 拒绝探针；移除死代码 | `TestDeniedWritesRejectsNullError` 修改前实际失败；新增 structured denial、long-tool 反例与 exit 2 fake 测试通过；后两项未对旧 live 场景做 red/green |
| Nit | TreeFingerprint 忽略失败 git 命令的全部输出，其他调用仍保留错误诊断 | `TestTreeFingerprintIgnoresFailedGitOutput` 修改前实际失败 |

这些测试只使用合成日志、临时假凭据文件和 fake 可执行文件。实际登录、计费任务、host 插件路径和修复后的各 harness 结果都待 host 验证，不能由普通回归替代。

## 已修复的 pactify 缺陷

1. **codex 不存在的订阅模型误归为 `other`，状态只显示 `Turn failed`。** 首次真实 job `20261017-132858-1827c8` 返回 HTTP 400，明确说明模型不支持 ChatGPT 账户，却丢了分类和状态提示。增加精确的模型错误规则，并保留已经显式收到的 error 通知；用独立 `Collector.ErrorMessage` 区分显式错误与终态 raw payload，重试时清空，保留原有 quota 终态的兼容契约。`TestCodexUnsupportedSubscriptionModelFailure` 修复前因 `other` 失败，修复后通过；真实 `model`、`failure_surface` 重跑通过。
2. **成功 git 命令的 stderr 污染 idle watchdog。** 普通全仓测试首次在 `TestHarnessUsageOnAbnormalExit/agy/idle` 超过 10 分钟。此环境的 xcrun 缓存警告每次带不同临时路径，`TreeFingerprint` 把成功 stderr 当成工作树变化，持续刷新 idle 时间。`gitx.run` 现在成功时仅返回 stdout，失败时保留 stdout 和 stderr 诊断。`TestTreeFingerprintIgnoresSuccessfulGitStderr` 修复前失败，修复后连同错误诊断保留测试、原 agy idle 用量测试通过。
3. **opencode 缺少 managed 登录文件时误归为 `other`。** 首次真实负例 `20261017-134553-5f088e` 已提示 `run opencode login first`，分类仍不正确。补充该明确登录提示的 auth 映射。普通回归 `TestManagedMissingLoginFailureIsAuth` 在修复前复现 opencode 的 `other`；修复后 opencode、kimi 均通过，真实 opencode 错误面重跑也通过。Kimi 原有该分类没有修改。
4. **agy 无效模型的原生 CLI 报错误归为 `other`。** 完整实跑收到了明确的 `invalid model selection` 和可用模型列表，终态清晰失败但类别不正确。`TestFailureRegression` 新增该真实错误格式，在修复前复现 `other`，修复后通过；增加精确的 `model` 规则，不把它当成 transient 重试。真实 model 定向重跑结果见上表。
5. **Kimi 订阅授权 grant 失效误归为 `other`。** 真实 wire 报错 `internal: The provided authorization grant is invalid` 没有被现有 auth 规则识别。补充该精确 OAuth 提示，普通回归同时覆盖原始 wire envelope 和持久化结果，修复前均复现 `other`。此分类测试仍有效，但初轮把登录失效归为环境问题的结论已撤回：套件的逐文件链接会丢失轮换后的凭据，需 Owner 重新登录。该失败路径的 usage 为 partial。

6. **opencode 原生工具截断没有进入结果 warnings。** 追加真实 5 MB 探针发现 raw 记录 `metadata.truncated=true`，但结果 warnings 缺少提示，最终答复的 `truncated=false` 又只表示最终文本。现将 SSE 和最终消息里的原生截断标志转为 `Opencode truncated tool output`，同轮重复完成帧只发一次 warning。普通 `TestOpencodeServeV2/truncated` 修复前为 0 条 warning 而失败，修复后通过；扩展的普通回归还验证 opaque metadata 不丢工具事件。真实输出重跑已回报该 warning，job 仍正常成功。

## 未修复项和验证边界

- **只读 40 次调用上限未修改。** opencode、agy 的 `readonly_long` 都实际在第 41 次工具调用触发 `Tool-call limit exceeded (41 calls)`。等待 Owner 决定，不用临时提高上限的构建替代本次结果。
- **codex 的外层环境限制。** 写入、读文件、长 shell 和取消启动等未走通。部分 job 正常给出最终说明，但文件或工具证据不存在，场景仍为 FAIL。`user_dirs_clean` 观察到全局 `.codex` 的数据库、缓存及当前会话文件变化；当前主 agent 同时使用该目录，无法把变化单独归因于测试，也不声称隔离检查通过。
- **opencode 的口头拒写。** 即便采用 AJ-713 的 plan diagnostic prompt override，MiniMax-M3 仍只解释只读策略，未发生实际被拒的 shell / 文件工具调用，按要求 FAIL，没有放宽权限或改成 N/A。
- **Kimi 的授权阻塞（初轮归因已撤回）。** Kimi `2.2.0` 的订阅默认配置回报 `kimi-code/k3-256k`，但实际模型调用前即遇到失效的 authorization grant，不能据此宣称实际模型已核验。需要真实模型的成功路径、工具压力、续接和指令遵守等尚未验证；这是套件自身凭据链接缺陷造成的失效，不能作为 kimi 集成或能力的证据；需要 Owner 重新登录。探针还记录了 Node 文件监听的 `EMFILE` 警告，未确认其独立影响。
- **host 登录未继承成功（S3 已修，待实跑）。** 初轮的 `.credentials.json` 链接被 Claude no-follow 拒绝，且 macOS 私有配置目录使钥匙串条目名带哈希。现移除文件链接，通过独立 secure-storage 环境变量选择现有登录；只用 fake 验证，仍未取得真实 host 摘要或 callee job。
- **模型动作与格式的不稳定。** opencode 初轮续接把记忆词放在中间消息而非最终答复，部分文件内容也未满足要求；并行与 records 初轮有内容不一致。明确续接的 shell `printf` 和最终答复格式，修正取消探针的观测方式后，相关场景重跑通过。其余文件验收保持原要求，未改成仅检查文件存在。这里没有统计大任务完成率。
- **初次 free 模型不可用。** 从实时 `/config/providers` 选中的 `opencode/ling-3.0-flash-fin-free` 在首个 write job `20261017-133340-ee3338` 返回 `Model is unavailable`，没有工具落地。取消这轮未完成的 free 探测后，用允许的 MiniMax-M3 完整重跑；未使用其他付费通道，也未调用 deepseek-v4-pro。
- `modelVerified` 只沿用现有 adapter 定义，不代表独立验证提供方的模型权重。`records` 对命令实际暴露的字段做交叉核对：cost 有 token 和模型分组，没有逐 job 的 duration / worktree 字段；duration 比较按现有显示格式解析，容许截秒与四舍五入造成的一秒差异。
- `truncated` 是最终答复的截断字段，不能据其为 false 宣称上游保存了完整 5 MB 工具输出。`large_output` 验证真实命令、执行完成的文件证据和最终答复；发现原生截断 flag 时要求 warnings 明示。opencode 实测原生 flag=true，修复后有 warning；agy 未观察到该 boolean flag，不将其等同于未发生任何上游裁剪。额度耗尽不能按需制造，`failure_surface` 将该子项注明为 N/A / `covered by golden replay`；其他负例仍须通过。
- 本次没有执行 §4 额外的三个耐力大任务，未完成任何 harness 的新转正流程。

## 文件清单

- 套件：`internal/sub/e2e/suite_test.go`、`isolation_test.go`、`catalog_test.go`、`scenarios_test.go`、`process_darwin_test.go`、`process_linux_test.go`，以及该目录的 `README.md`。真实调用测试仍有 `harness_e2e` build tag；`evidence_helpers_test.go`、`metadata_test.go` 与 `checks_test.go` 无 tag，供普通测试用合成记录与 fake 验证，不发起真实调用。
- 运行时修复：`internal/cockpit/agy.go`、`codex.go`、`opencode.go`、`kimi_failure.go`、`opencode_serve.go`；`internal/gitx/gitx.go`；`internal/sub/collect.go`、`worker.go`。
- 本轮补充：`internal/credentiallinks/refresh.go` 与 `refresh_test.go`，`internal/sub/managed_home.go`、`kimi_home.go`、`kimi_models.go`、`credential_refresh_test.go` 及相应既有测试；`internal/cockpit/golden_record_test.go` 与 AJ-713 README；e2e 的无 tag helper / fake 回归。
- 普通回归：`internal/sub/failure_test.go`、`internal/cockpit/failure_test.go`、`internal/cockpit/opencode_serve_test.go`、`internal/gitx/fingerprint_stderr_test.go`；复用并扩展 `internal/testenv/opencode.go` 假服务。
- 文档：`docs/specs/harness-v2.md` §4、`docs/CHANGELOG.md`、本报告和 `aj718-e2e-results/` 下的 14 份脱敏 JSON。
- 未修改 `CLAUDE.md`、`AGENTS.md`、`GEMINI.md` 或 `plugins/pactify`，未修改只读调用上限。

## 上一轮检查和运行命令（历史记录）

所有 `go test` 都使用独立 PACTIFY_HOME。下列环境前缀中的临时目录只是示例名，不是用户 HOME。

```sh
export TMPDIR=/private/tmp

go build ./...
gofmt -l $(git diff --name-only HEAD -- '*.go'; git ls-files --others --exclude-standard -- '*.go')
PACTIFY_HOME="$(mktemp -d)" go test ./...
PACTIFY_HOME="$(mktemp -d)" go vet -tags harness_e2e ./internal/sub/e2e/...
test -f docs/harness-v2/aj718-e2e-report.md
```

普通全仓测试最终通过（cmd/pactify 253.265s，internal/cockpit 58.057s，internal/sub 235.909s）；修复前的 watchdog 超时没有计作通过。中间一轮还暴露了旧 quota reason 兼容断言与一次 checkout hook 5 秒等待失败；前者用显式 error 与 raw 终态分离解决，后者单项及最终全仓均通过，未修改等待阈值。构建、格式、tagged vet 和报告检查的最终结果见收尾验证记录。

完整实跑使用以下形式，四家串行执行，分别输出独立 JSON：

```sh
PACTIFY_HOME="$(mktemp -d)" PACTIFY_E2E_HARNESS=codex \
  PACTIFY_E2E_HOST=1 PACTIFY_E2E_REPORT=/private/tmp/aj718-codex-final.json \
  go test -tags harness_e2e ./internal/sub/e2e -run '^TestHarnessE2E$' -count=1 -v -timeout 90m

PACTIFY_HOME="$(mktemp -d)" PACTIFY_E2E_HARNESS=opencode \
  PACTIFY_E2E_MODEL=minimax-coding-plan/MiniMax-M3 PACTIFY_E2E_HOST=1 \
  PACTIFY_E2E_REPORT=/private/tmp/aj718-opencode-final.json \
  go test -tags harness_e2e ./internal/sub/e2e -run '^TestHarnessE2E$' -count=1 -v -timeout 90m

PACTIFY_HOME="$(mktemp -d)" PACTIFY_E2E_HARNESS=agy \
  PACTIFY_E2E_REPORT=/private/tmp/aj718-agy.json \
  go test -tags harness_e2e ./internal/sub/e2e -run '^TestHarnessE2E$' -count=1 -v -timeout 90m

PACTIFY_HOME="$(mktemp -d)" PACTIFY_E2E_HARNESS=kimi \
  PACTIFY_E2E_REPORT=/private/tmp/aj718-kimi.json \
  go test -tags harness_e2e ./internal/sub/e2e -run '^TestHarnessE2E$' -count=1 -v -timeout 90m
```

codex 定向重跑使用 `PACTIFY_E2E_SCENARIOS=resume,model,failure_surface,records,user_dirs_clean`；agy 使用 `model,user_dirs_clean`。Kimi 先用 `records,user_dirs_clean` 小范围复现授权分类，再跑完整轮。opencode 定向重跑另设 `PACTIFY_E2E_SCENARIOS=resume,cancel,parallel,records,failure_surface,user_dirs_clean`，不再重复四分钟长工具。取消场景增强 OS `sleep` 身份确认后，两家又分别使用 `cancel,user_dirs_clean` 串行重跑并通过；大输出追加检查使用 `large_output,user_dirs_clean`，opencode 修复 warning 后再跑一次，均通过。其他有效定向重跑及汇总以结果章节的记录为准。普通回归另跑了 `go test ./internal/cockpit -run '^TestOpencodeServeV2$' -count=1`（独立 PACTIFY_HOME，含原生截断、重复帧和 opaque metadata），通过。其他普通回归命令包括：

```sh
PACTIFY_HOME="$(mktemp -d)" go test ./internal/sub ./internal/cockpit \
  -run 'TestManagedMissingLoginFailureIsAuth|TestCodexUnsupportedSubscriptionModelFailure|TestFailureRegression|TestStructuredFailuresTakePrecedence' -count=1

PACTIFY_HOME="$(mktemp -d)" go test ./internal/gitx ./internal/sub \
  -run 'TestTreeFingerprintIgnoresSuccessfulGitStderr|TestGitRunRetainsFailureDiagnostic|TestHarnessUsageOnAbnormalExit/agy/idle|TestCodexUnsupportedSubscriptionModelFailure' -count=1
```

启动检查的 `git pull` 因 detached HEAD 失败，`.agent/CURRENT.md` 不存在；未切换分支或合并。所有改动留在本 worktree，未 commit、push 或合并。

## 上一轮收尾验证记录（历史记录）

- `go build ./...`：通过。
- 验收给定的 `gofmt -l` 命令：通过，输出为空。
- 独立 PACTIFY_HOME 的最终 `go test ./...`：通过（cmd/pactify 253.265s，internal/cockpit 58.057s，internal/sub 235.909s）。
- `go vet -tags harness_e2e ./internal/sub/e2e/...`：通过。
- `test -f docs/harness-v2/aj718-e2e-report.md`、`git diff --check`：通过。
- `GOOS=linux GOARCH=amd64` 下以独立 PACTIFY_HOME 执行 `go test -tags harness_e2e -c -o /private/tmp/aj718-e2e-linux.test ./internal/sub/e2e`：交叉编译通过；没有在 Linux 上执行真实 harness。
- 本报告和 14 份 JSON 经过 AJ-713 `golden.Scan` 独立扫描，全部通过；没有修改受保护的 entry 文件或插件。

## 第一轮返工验收记录（历史，无真实调用）

以下实际命令已执行。每个 `go test` 和 tagged vet 都使用新建的独立 PACTIFY_HOME，TMPDIR 为 `/private/tmp`。完整普通测试通过：`cmd/pactify` 263.883s、`internal/cockpit` 67.972s、`internal/sub` 234.326s；其余包也通过。后续只对调整过的纯 helper 和镜像清理分支做定向复核。

```sh
go build ./...
go vet ./...
gofmt -l $(git diff --name-only HEAD -- '*.go'; git ls-files --others --exclude-standard -- '*.go')
TMPDIR=/private/tmp PACTIFY_HOME="$(mktemp -d /private/tmp/pactify-test.XXXXXX)" \
  go vet -tags harness_e2e ./internal/sub/e2e/...
TMPDIR=/private/tmp PACTIFY_HOME="$(mktemp -d /private/tmp/pactify-test.XXXXXX)" \
  go vet -tags record ./internal/cockpit
TMPDIR=/private/tmp PACTIFY_HOME="$(mktemp -d /private/tmp/pactify-test.XXXXXX)" \
  go test ./... -count=1
TMPDIR=/private/tmp PACTIFY_HOME="$(mktemp -d /private/tmp/pactify-test.XXXXXX)" \
  go test ./internal/sub/e2e ./internal/credentiallinks -count=1
```

初次 red 验证分别运行 `go test ./internal/sub -run 'TestManagedCredentialAtomicRefresh|TestKimiRefreshLockDirectoryLinked' -count=1`、`go test ./internal/cockpit ./internal/sub ./internal/gitx -run 'TestCodexRetryNotificationIsNotTerminalError|TestWorkerTruncationWarningDoesNotBecomeError|TestTreeFingerprintIgnoresFailedGitOutput' -count=1` 及 `go test ./internal/sub/e2e -count=1`，均有独立 PACTIFY_HOME，确认上述指定回归失败。修复后这些回归与新增 fake 检查均通过。中间一次 Kimi/managed-home 定向检查仍因旧测试要求删除刷新副本而失败；已改为验证回写/隔离，并使用测试专属 quarantine，随后该组与全仓普通测试通过。

S3 的变量支持来自本机 `~/.local/share/claude/versions/2.1.301` 的嵌入源码检查：`Fx` 选择安全存储目录，`v1` 在显式空值时省略钥匙串哈希后缀，同时存在 `auth` / `status` 命令注册；没有执行该二进制。

S3、long_tool 和 kimi N/A 的新 fake 测试没有在旧 live 场景上执行 red/green，不能宣称重新验证了真实 host 登录或各 harness 的长工具能力。最终静态检查、格式检查与差异检查通过；本轮未运行带 harness_e2e tag 的测试，也未运行 sub doctor、sub run、claude auth status。实际 rerun 由 host 完成，kimi 须先由 Owner 重新登录。没有 commit、push 或合并；受保护 entry 文件、plugins/pactify、40 次上限未改。

本轮本报告、14 份历史 JSON 和两份 README 共 17 个工件通过 AJ-713 `golden.Scan` 检查；扫描仅处理这些仓库文档与历史脱敏结果。临时扫描 helper 已删除。

## 第二轮返工修复与回归（无真实调用）

| 项目 | 修复与测试 | 修改前验证 |
|---|---|---|
| N-B1 | `TestKimiCreatesMissingOAuthDirectory` 验证 prepare 成功、共享目录及 0700 权限 | 实际 red，源缺少 oauth 被误报 auth；修复后定向通过 |
| S1 | `TestDefaultQuarantineUsesPactifyHome`、`TestDefaultQuarantineWithoutOverrideUsesPersistentHome`；worker/doctor/catalog 按 store 传隔离根；`TestNestedQuarantineSurvivesFixtureRemoval` 验证 fixture 的内层隔离项保全 | 默认路径测试实际 red；其余为修复后的新增覆盖 |
| S2 | `TestKimiPrivateConfigRewriteDoesNotReachUser` 覆盖 cleanup 与下一次 prepare；`TestHarnessCredentialLinkDeclarations` 审计各家声明；模型发现 fake 同步要求源配置不变 | 配置、device_id 在两条路径上均实际 red；修复后通过。另 `TestKimiConfigRewriteWarningDoesNotBecomeJobError` 实际 red，确认配置丢弃提示曾盖进成功 job 的错误字段，修正为 informational 后通过 |
| S3 | `TestRestorePreservesSourcePermissions`；`TestRestoreChmodFailureQuarantines` 验证失败时源不变且副本保留；`TestManagedCredentialRetentionExplainsRecovery` 核对阻断错误中的路径与恢复指引 | 原权限测试实际 red（0600 被改成 0644）；其余为修复后覆盖。交互式刷新竞态仍未消除，已登记 backlog |
| S4 | 删除旧 `TestWorkerCodexReconnectDoesNotPoisonQuota`。新增 `TestWorkerCodexFailureNotificationsThroughAdapter`：自建假 stdio app-server 经真实 codex 适配器解析原始 reconnect/terminal 序列及 quota 终态 | 两种场景在撤回 Err 改动前实际 red；撤回后通过。另临时移除 willRetry 过滤，reconnect 子测试实际失败为 quota、写入额度记忆；恢复过滤后验证通过 |
| Nit helpers | helpers 改名为无 tag 的 `_test.go`，普通测试可用，生产构建无 e2e GoFiles | 文件名与静态构建检查；无 red/green 声称 |
| Nit doctor | 错误分支把 Cleanup warnings 合并进 Managed home 结果；`TestDoctorReportsCredentialCleanupWarnings` 用损坏的合成 metadata sidecar 触发 retained warning | 实际 red，原结果隐藏 warning；修复后通过 |
| Nit readonly probe | `TestKimiReadonlyProbeCancelsUnexpectedJobFake`；返回 jobId 即跟踪并立即 cancel，然后 FAIL，含 cancel 失败反例 | 实际 red，旧检查没有调用 cancel；修复后通过 |
| Nit host fallback | `TestHostPlaintextFallbackIsQuarantined`；teardown 同时检查私有 HOME/.claude 与 host config，失败时保留 fixture | 新 guard 的 TDD red/green：无保全逻辑时测试失败，加入 rename/FAIL 后通过；未运行旧 live host 场景 |

`TestCodexRetryNotificationIsNotTerminalError` 和 `TestWorkerQuotaMessagePreservesLegacyReason` 都保留。新适配器回归使用测试二进制提供假 JSON-RPC peer，没有启动安装的 codex；不把基线上也能通过的 fake-session 测试当作回归证据。

第二轮实际验证命令（每次 go test 使用新 PACTIFY_HOME，未执行任何真实 harness）：

```sh
go build ./...
gofmt -l $(git diff --name-only HEAD -- '*.go'; git ls-files --others --exclude-standard -- '*.go')
go vet ./...
TMPDIR=/private/tmp PACTIFY_HOME="$(mktemp -d /private/tmp/pactify-test.XXXXXX)" \
  go vet -tags harness_e2e ./internal/sub/e2e/...
TMPDIR=/private/tmp PACTIFY_HOME="$(mktemp -d /private/tmp/pactify-test.XXXXXX)" \
  go vet -tags record ./internal/cockpit/...
TMPDIR=/private/tmp PACTIFY_HOME="$(mktemp -d /private/tmp/pactify-test.XXXXXX)" \
  go test ./... -count=1
go list -f '{{.ImportPath}} production-files={{.GoFiles}} test-files={{.TestGoFiles}}' ./internal/sub/e2e
```

针对回归的初次 red 检查使用 `go test ./internal/credentiallinks ./internal/sub -run 'TestDefaultQuarantineUsesPactifyHome|TestRestorePreservesSourcePermissions|TestKimiCreatesMissingOAuthDirectory|TestKimiPrivateConfigRewriteDoesNotReachUser|TestDoctorReportsCredentialCleanupWarnings' -count=1`，及 `go test ./internal/sub -run '^TestWorkerCodexFailureNotificationsThroughAdapter$' -count=1`，都有独立 PACTIFY_HOME。Nit readonly 探针的新增 fake 回归也实际 red/green。host fallback 只验证合成文件的保全逻辑，未验证真实钥匙串故障。后续配置规范化测试第一次受缺失的合成 wire 记录干扰；补齐假记录后，实际复现了成功 job 错误字段中的配置丢弃提示，再修为 informational 并通过。

一次中间全仓普通测试通过后，因新增配置 warning 修复重新执行最终全仓验收；最终结果以下方收尾记录为准。无真实模型、doctor/run CLI 或 Claude 登录状态检查，所有真实场景仍交由 host 在审查后重跑。kimi 需 Owner 重新登录；codex 的历史沙箱结果与 kimi 的历史成绩继续标为不可作为集成证据。

第二轮最终收尾结果：

- `go build ./...`、指定 `gofmt -l`（输出为空）、`go vet ./...`、harness_e2e tagged vet、record tagged vet、`git diff --check` 均通过。
- 最终独立 PACTIFY_HOME 的 `go test ./... -count=1` 通过：cmd/pactify **259.321s**，internal/cockpit **62.007s**，internal/sub **237.303s**，internal/sub/e2e **4.098s**，其他包也通过。该轮包含最后的配置 warning 修复。
- `go list` 实际回报 e2e 的 `production-files=[]`，无 tag 的 test-files 为 checks_test.go、evidence_helpers_test.go、metadata_test.go；真实调用场景仍需 build tag。
- 验收最后一条 `test ! -e /tmp/pactify-credential-quarantine-$(id -u) || echo ...` 因历史目录已存在而打印 check。额外元数据快照比较：**前后均 71 项，device/inode/mode/size/mtime/symlink target 完全一致**。没有打开其中任何文件；这轮测试没有留下新增项或元数据变化。未删除历史隔离项。
- 本报告、两份 README、backlog 和 14 份历史 JSON 共 18 个仓库工件通过 AJ-713 sanitizer；扫描 helper 随后删除。
- 受保护 entry 文件、插件、只读调用上限未改；未 commit、push、合并。真实 harness/host 场景没有重跑。
