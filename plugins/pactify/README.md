# Pactify harness 插件

在 Claude Code 中把大任务交给本机官方 codex / kimi CLI，完成后收回结果摘要并审查工作区改动。

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
/codex 为 src/cache/ 增加过期策略和回归测试，只修改该目录，运行 npm test -- cache
/kimi 只读调查 internal/sub/ 的取消流程，列出风险和证据，不修改任何文件
```

也可始终使用完整入口 `/pactify:codex`、`/pactify:kimi`，避免与其他同名 skill 冲突。
适合涉及 3 个以上文件、需要探索和测试、或预计超过约 10 分钟的任务；小改动直接由 Claude 完成。

Claude 主线程先写包含目标、路径、边界、权限、验收命令的完整 brief，再调用
`pactify sub run <harness> --write --wait 0s --json`。只读任务改用 `--read-only`。
退出码 3 表示仍在执行，stdout 包含 `jobId`；主线程以 `run_in_background: true` 执行
`pactify sub wait <jobId>`，完成后读取摘要并检查 diff。退出码 0 / 1 / 2 分别表示成功、失败、参数错误。
可用 `pactify sub result <jobId>` 重新读取结果。

如需指定模型，在任务中明确写出模型名；CLI 无法兑现时应拒绝执行并给出可选模型，不静默降级。
续接同一仓库中该 harness 的上一次成功会话时明确要求 `--resume`。
愿意等待约 9 分钟以内的任务，也可交给 `pactify:codex` / `pactify:kimi` 转发 subagent：
它使用 `model: haiku`、仅有 Bash 工具，只调用一次 Bash，原样返回 stdout；超时后由主线程继续 wait。
callee 不提交代码，改动留给 host 审查。

## 结果摘要

- 首行：完成状态或模型、工具调用上限、空闲超时等警告。
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

## 已知限制

- 第一版不提供 kimi 只读档（`sub run kimi --read-only` 退出 2）：Phase 0 实测 kimi 的 plan 模式在只读任务上不收敛。只读任务用 codex。
- job 在启动阶段就失败时（例如请求的模型不可用），callee 可能已经建了会话但还没把 id 交回，这个会话不会被隐藏。
- 每次委派在 callee 侧约有 2–5 万 token 固定开销，不适合零碎小任务。
- 只有本机已安装、已登录的官方 CLI 可用。模型独立核实失败时，以摘要中的警告为准。
- 自动测试不涵盖真实 Claude Code 安装与委派；发布验收还需要在 Claude Code 中完成冒烟。
