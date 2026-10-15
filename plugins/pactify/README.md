# Pactify harness 插件

在 Claude Code 中把大任务交给本机官方 codex / kimi CLI，完成后收回结果摘要并审查工作区改动。
默认 harness 为 codex；kimi 为实验性 harness（Phase 0：大任务一次完成 6/10，只读不收敛）。

## 安装

在 Claude Code 中依次执行：

```text
/plugin marketplace add agentjoey/pactify
/plugin install pactify@pactify
```

前提：`pactify` 在 PATH 上，且 `pactify sub --help` 可用；要调用的官方 codex / kimi CLI 已安装并登录。
SessionStart hook 只检查 pactify 与 `sub` 命令是否可用，缺失时提示安装或升级，不阻断会话。
插件不代理模型、不管理登录，也不读取或复制凭据。

## 使用

```text
/delegate 为 src/cache/ 增加过期策略和回归测试，运行项目全量测试
/codex 为 src/cache/ 增加过期策略和回归测试，只修改该目录，运行 npm test 全量测试
/kimi 为 src/cache/ 增加过期策略和回归测试，允许写入，运行项目全量测试
```

也可始终使用完整入口 `/pactify:delegate`、`/pactify:codex`、`/pactify:kimi`，避免与其他同名 skill 冲突。
`/delegate` 使用默认 codex；`/codex`、`/kimi` 显式选择 harness。显式调用以上任一入口时一定外派，无论任务大小。
适合涉及 3 个以上文件、需要探索和测试、或预计超过约 10 分钟的任务；小改动直接由 Claude 完成。
自主判断时也不要外派依赖大量口头上下文、无法写成自足说明的任务，或需要与用户频繁交互的任务。
显式调用仍一定外派，但 kimi 的只读限制不变，不得自行升级为写权限。

Claude 主线程先写完整 brief，固定包含：背景、编号目标、已知约束与特例、通用约束、验收命令。
写清路径、范围、权限与已知例外，callee 无法推断当前对话中的约束；验收必须包含项目全量测试命令。
要求改规则前检查调用方、特例及文档例外，每个行为改动先写回归测试并看到失败；不许为迁就新行为修改已有测试，
只有 brief 明确判定旧行为错误才可改对应测试，并在最终回复点名。禁止提交及读取凭据；可选禁止派子代理、写报告文件。
三份 skill 都内嵌同一份模板、完整示例及评审流程，共享标记区由 bats 检查逐字一致。

主线程调用 `pactify sub run [harness] --write --worktree --wait 0s --json`。
省略 harness 时使用 codex，例如 `/delegate` 调用 `pactify sub run --write --worktree --wait 0s --json`。
只读任务用 codex，改用 `--read-only`；kimi 仍拒绝只读任务（退出 2）。
退出码 3 表示仍在执行，stdout 包含 `jobId`；主线程以 `run_in_background: true` 执行
`pactify sub wait <jobId>`，同时设置 `timeout: 7200000`，完成后进入下面的评审流程。
若后台 wait 因时限停止，先查 `pactify sub status` 再继续同一个 wait；不要重复启动任务。
退出码 0 / 1 / 2 分别表示成功、失败、参数错误。
可用 `pactify sub result <jobId>` 重新读取结果。

如需指定模型，在任务中明确写出模型名；CLI 无法兑现时应拒绝执行并给出可选模型，不静默降级。
返工时明确要求 `--resume --worktree`，保持原始 cwd 和 harness。
愿意等待约 9 分钟以内的任务，也可交给 `pactify:codex` / `pactify:kimi` 转发 subagent：
它使用 `model: haiku`、仅有 Bash 工具，只调用一次 Bash，原样返回 stdout；超时后由主线程继续 wait。
callee 不提交代码，改动留给 host 审查。

## Reviewing a delegation

1. 读取摘要及首行警告，先看完整 diff 和新增文件。worktree 从原仓库 HEAD 创建，不带入原工作区未提交的改动。
   从结果取得 `worktreePath`、`baseCommit`，运行 `git -C <worktreePath> diff <baseCommit> --` 及
   `git -C <worktreePath> ls-files --others --exclude-standard`，并打开列出的新文件。
2. 主线程亲自在 worktree 重跑所有验收命令，包括全量测试；Go 测试使用 `PACTIFY_HOME="$(mktemp -d)"`。
   callee 自报通过不能代替主线程验收。
3. 能在真实环境验证的，主线程亲自试用一次，观察真实用户流程、日志可读性和模型实际行为等相关结果。
   记录做过的验证及无法验证的缺口，不把自动测试当成真机验证。
4. 并发、权限、删数据等高风险改动另请独立评审；处理发现的问题后重跑验收。
5. 评审通过 → `pactify sub apply <jobId>`，成功后 `pactify sub discard <jobId>` 清理 worktree。
   apply 不提交；相关路径存在用户改动或冲突时整次拒绝，先查原因，不强行覆盖或盲目重试。
   返工 → 不 apply、不 discard，用原始 `--cwd` 和同一 harness 执行 `sub run ... --resume --worktree`，
   保持原权限、指定模型和 effort，在 stdin 提供返工说明，再按同一后台 wait 流程收回结果，重复步骤 1–4。
   放弃 → 不 apply，直接 `pactify sub discard <jobId>`；运行中的 job 必须先 `pactify sub cancel <jobId>`。

返工消息模板：

```text
在同一个 worktree 继续原任务，原范围与约束保持不变。
评审发现：<文件/行号、实际与预期行为、复现步骤或失败输出>
需要修改：<逐条修复要求及必须保留的约束>
补充测试：<回归场景，先见失败；不弱化已有测试>
重跑验收：<所有原验收命令，包含全量测试>
最终回复贴出改动、命令输出及剩余缺口；不提交、不写报告文件。
```

`--resume` 按原始 cwd 与 harness 选最近有 session id 的已结束 worktree 会话，包括失败或取消的轮次；
续接前核对目标会话，续接后核对仍是同一个 worktree 和 base commit，不在同一 worktree 并发开轮次。
不带 `--worktree` 续接也会沿用原隔离；已 discard 或丢失的 worktree 不能续接。
旧的非隔离会话仍须成功才能续接，不能中途加隔离，需去掉 `--resume` 新建 worktree 任务。
apply / discard 可使用续接链上任一 job id，作用于共享 worktree；discard 会标记整条链。

## 结果摘要

- 首行：完成状态或模型、工具调用上限、空闲超时等警告。
- kimi job 的首行包含实验性提示；提示保存在 `Result.Warnings` 中，不改变 job 状态。
- Harness 与 CLI 版本：实际调用的工具及其版本。
- 模型与权限：请求模型、实际模型及独立核实状态，`read-only` 或 `write`。
- 用量：耗时、输入 / 输出 token、工具调用次数；token 用量不代表精确账单金额。
- 改动：文件列表及增删行数，供 host 检查 diff。
- 最终回复：最多 4096 字节；会话 id 与完整日志路径供追查。

job 存在 `$PACTIFY_HOME/jobs/<jobId>/`，默认 `~/.pactify/jobs/<jobId>/`。

**不留痕**：委派出去的会话在 job 结束时自动隐藏，不会出现在你自己用这些工具时的会话 / 项目列表里。每个接入的 harness 都必须声明隐藏方式（有测试强制）：

| harness | 怎么隐藏 | 想看原始会话 |
|---|---|---|
| codex | 结束时归档线程（Codex 桌面与手机端都不显示） | `codex unarchive <会话 id>` |
| kimi | 结束时调用 kimi 自己的归档接口（与 kimi web 界面「归档」同一个调用：pactify 临时起一个只监听本机的 `kimi web --no-open`，用完即关），命令行、web 与 Remote Control 都不再显示；接口不可用时退回写会话文件里的归档字段（只对命令行列表生效，摘要首行会提示）；最后用 `kimi session list` 核对 | `kimi session list --archived --cwd <目录>`，或 kimi web 的「已归档会话」里恢复 |

`--resume` 会先取消隐藏、续接，结束后再隐藏。会话 id 在摘要的「会话」一行。隐藏失败时摘要首行会提示（例如 kimi 自动更新后改了会话格式）。kimi 的 workspace 条目由它自己从会话记录派生，归档后仍会保留——与你在 kimi 里手动归档一个会话的效果相同。

## 权限档

| | codex | kimi |
|---|---|---|
| `--write` | 沙箱 workspace-write，**可联网**（能跑起本地服务的测试、能拉依赖） | kimi 默认模式（直接写，无沙箱） |
| `--read-only` | 沙箱 read-only，不联网 | 第一版不提供 |

## 已知限制

- 第一版不提供 kimi 只读档（`sub run kimi --read-only` 退出 2）：Phase 0 实测 kimi 的 plan 模式在只读任务上不收敛。只读任务用 codex。
- job 在启动阶段就失败时（例如请求的模型不可用），callee 可能已经建了会话但还没把 id 交回，这个会话不会被隐藏。
- 每次委派在 callee 侧约有 2–5 万 token 固定开销，不适合零碎小任务。
- 只有本机已安装、已登录的官方 CLI 可用。模型独立核实失败时，以摘要中的警告为准。
- 自动测试不涵盖真实 Claude Code 安装与委派；发布验收还需要在 Claude Code 中完成冒烟。
