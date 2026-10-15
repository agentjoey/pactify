# `pactify sub doctor` 自检 v1（AJ-626）

```sh
pactify sub doctor          # 检查全部已注册 harness
pactify sub doctor codex    # 只检查 codex
pactify sub doctor kimi     # 只检查 kimi
```

这条命令会真实调用 harness，消耗 API token。codex 请求 `gpt-6-luna`，kimi 请求 `kimi-code/kimi-for-coding`；CLI 必须已经安装、完成登录，并能使用相应模型。codex 网络探测使用 `python3` 标准库，绑定 `127.0.0.1` 的随机端口并访问 `https://example.com`。缺少 Python 或外网不可达都会使网络检查失败。

探测沿用 `sub run` 的默认配置策略：codex 以最小配置启动，关闭 MCP、插件、skills 和记忆；认证、provider、沙箱设置以及全局和仓库指令仍由原生 harness 加载。kimi 使用独立的 `KIMI_CODE_HOME=$PACTIFY_HOME/harness-homes/kimi`，目录权限 0700，不放 `mcp.json`。`config.toml`、`credentials/`、`device_id` 只用符号链接指向 pactify 进程的 `KIMI_CODE_HOME`（未设置时为 `~/.kimi-code`）；不读取或复制内容，缺少原文件时提示先 `kimi login`。每次启动修复链接，结束时若发现链接变成普通文件或目录，删除副本并在结果中警告；下次启动重建链接。共享目录长期保留以支持续接。普通 job 可用 `--with-user-config` 恢复全部用户集成；doctor 始终验证默认路径。

codex 输出六项「PASS / FAIL / SKIP + reason」：安装及版本、可写档（文件、最终回复、token）、独立核实的实际模型、只读档、网络、不留痕。codex 只读档必须实际调用工具、正常结束且没有留下改动；kimi 沿用注册表的 `NoReadOnly` 原因跳过。网络仅检查 codex 可写档，需要脚本生成的本地绑定和 HTTPS 成功证据，单凭最终回复不能通过。所有执行结果的 CLI 版本也必须与探测开始时的版本一致。

只读探测明确要求通过 shell 工具执行 `sh -c 'echo hi > out.txt'; echo EXIT=$?`，并原样报告 stdout、stderr 与退出码。写入由沙箱决定是否拦截；即使最终回复声称被拦，只要没有工具调用，仍判为失败。

kimi 额外输出第七项 `Managed home`：检查共享目录及三个链接正确、探测 job 成功且记录的 home 是共享目录、探测 session id 不在用户 home 的 `session_index.jsonl` 中，并读取共享 home 对应的 `wire.jsonl`，确认没有 `mcp.tools_discovered` 事件。缺少会话或 wire 证据、索引不可读或损坏均不能通过。CLI 不可用时该项 SKIP。

仓库和 job 均放在一次性临时目录；传给子进程的 `PACTIFY_HOME` 指向临时 home，不改变当前进程的环境。kimi 的 `KIMI_CODE_HOME` 仍指向调用者 `$PACTIFY_HOME` 下的长期共享目录，探测会话保留在那里；最小配置无需启动 `kimi web` 或归档。codex 继续走普通 job 的 `Hidden: true` 归档流程。正常结束、检查失败和可处理的取消之后都会移除临时仓库和 job；不要通过 `SIGKILL` 结束进程。

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
