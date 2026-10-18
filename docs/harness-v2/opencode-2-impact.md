# opencode 2.0 对 pactify 接入的影响（2026-10-17，AJ-719）

> **现状（2026-10-18，AJ-720）**：pactify 已经支持 opencode 2.x，并会按本机安装的大版本选择 1.x 或 2.x 后端。2.x 使用 pactify 自己的私有 home 和登录（`pactify sub login opencode`）。实跑结果：`sub doctor` 全部 PASS，AJ-718 端到端 13/14。下文是当时的影响评估，保留作记录。

> **当前状态（AJ-720，2026-10-18）：正在加入与 1.x 并存的 2.x 后端。** 以下 AJ-719 结论是改造前的历史记录，不代表本次 worktree 的实现。版本选择、私有登录库、并发租约、验证结果与 host 录制步骤见 [AJ-720 implementation](aj720-opencode2.md)。1.x 后端继续保留，≥ 3 快速失败。

> 方法：把 `@opencode/cli@2.0.32` 装到临时目录里。所有探测都在隔离的 `HOME` 和 `XDG_*` 目录下运行，不登录、不发推理请求，凭据只用合成的测试值。本机的 opencode 1.19.0 没有改动。
> 流程见 [`../harness-updates.md`](../harness-updates.md)。

## 结论

- **本机现在不受影响。**
  - 本机装的是 1.19.0。
  - `opencode-ai` 包的 latest 仍是 1.19.3。
  - pactify 启动 opencode 时设置了 `OPENCODE_DISABLE_AUTOUPDATE=1`，不会自动升级。
- **一旦升级到 2.x，pactify 的 opencode 就全部不能用：** 所有 job、`sub doctor` 和模型发现都会失败。升级方式包括：
  - 运行 `opencode upgrade --major`；
  - 用 v2 的 curl 安装器，它会直接替换 v1 的二进制。
- **这次修改之后，pactify 遇到 2.x 会立即报错**，报错里给出回退方法，不再等 30 秒超时。
- **v2 适配器暂不开始做**（建议，待 Owner 决定）。理由：
  - 2.x 的服务端 API 在自己的 OpenAPI 里写的是 "Experimental HttpApi"，版本号 0.0.1；
  - 官方说新 SDK 仍在开发中；
  - 1.x 仍是 npm 上的 latest，并且还在发补丁版本。

## 实测：pactify 依赖的契约

| 契约 | 1.19（现状） | 2.0.32 | 影响 |
|---|---|---|---|
| 发布渠道 | npm `opencode-ai` | npm `@opencode/cli`（2.0.0 于 2026-09-11 发布，2.0.32 于 2026-10-16 发布），以及 curl 安装器 | 原先的周检查只看 `opencode-ai@latest`，看不到 2.x |
| `opencode serve --hostname 127.0.0.1 --port 0` | 支持 | 仍支持，另外新增了 `--service`、`--stdio` | 无 |
| 启动时的输出 | `opencode server listening on http://…` | `server listening on http://…` | **阻断。** `startOpencodeServer` 按前缀匹配，匹配不到，30 秒后报 `opencode serve startup timed out` |
| Basic auth（`OPENCODE_SERVER_USERNAME/PASSWORD`） | 有效 | 仍有效：`/api/*` 不带认证返回 401 | 无 |
| HTTP API | v1：`/session`、`/session/{id}/prompt_async`、`/event`、`/permission`、`/config/providers` 等 11 个操作 | 全部换成 `/api/*`：`/api/session/{id}/prompt`、`/interrupt`、`/api/event`、`/api/session/{id}/permission/{rid}/reply`、`/api/provider`、`/api/model`……；OpenAPI 在 `/openapi.json` | **阻断。** 适配器需要重写。旧路径返回 200 和网页 HTML，而不是 404，所以报错会很难看懂 |
| 权限配置（`OPENCODE_CONFIG_CONTENT` 里的 v1 `permission`） | 原样生效 | 会被自动换算成 v2 的 `permissions` 数组（`bash` 换成 `shell`，`write` 并入 `edit`，规则顺序保留）；`experimental.continue_loop_on_deny` 被丢弃 | 只读规则大概还能用，但换算后的规则在实际执行时是否按原意生效还没有验证，需要跑 AJ-718 的 `readonly_*` 场景 |
| 会话级权限（`PATCH /session` 的 `permission`） | 支持 | 改为 `POST /api/session/{id}/permission` | 需要迁移 |
| 内置 `plan` agent | 禁止编辑 | 仍然存在：禁止编辑，但 `shell` 允许 | 只读档要继续依靠 pactify 自己的规则 |
| 凭据 | `XDG_DATA_HOME/opencode/auth.json`，pactify 把这个文件按凭据链接进私有目录 | 放一个合成的 `auth.json`，启动后 `/api/credential` 为空。v2 的凭据改由 `/api/credential` 管理（存储位置没有核实） | managed home 的凭据链接要重新设计 |
| 数据库 | `XDG_DATA_HOME/opencode/opencode.db` | 路径相同；启动时会执行 `/api/experimental/migration/v1` | 升级会迁移用户的会话数据。会话隐藏的做法要重新核实 |
| 模型 ID | `provider/model`，variant 单独传 | `provider/model#variant` | 模型目录和 effort 映射要改 |
| 指令文件 | 读 `AGENTS.md` 和 `CLAUDE.md` | 只读 `AGENTS.md`，不再读 `CLAUDE.md` | 对 brief 没有影响 |
| 后台服务 | 无 | TUI、`run` 默认连接共享的后台服务；`serve` 和 `run --standalone` 是独立进程（据发布说明，未实测） | pactify 用的是 `serve`，不会碰到共享服务 |

## 修改前后的本机对照

两次都在完全隔离的 HOME 和 XDG 目录下运行，数据目录里只有合成的 `auth.json`，PATH 最前面是 2.0.32 的二进制。

- **当前部署的 pactify：** `pactify sub doctor opencode` 报告 `PASS Installed: CLI version v2.0.32`。随后 Write access 和 Read-only access 两项各自等满 30 秒，都报 `opencode serve startup timed out`，总共 61 秒，最后全部 FAIL。
- **本分支：** 1 秒内给出结果：`FAIL Installed: opencode v2.0.32 is not supported yet: pactify drives the opencode 1.x server API; reinstall opencode 1.x (for example npm install -g opencode-ai@1)`，并且不启动 `opencode serve`。job 和模型发现也走同一个检查。

## 选项（待 Owner 决定）

- **A（推荐）：先留在 1.x。** 保留快速失败，2.x 记在 `releases.py` 的 `TRACKED` 表里。满足下面任意一条时重新评估：
  - `opencode-ai` 被标为 deprecated，或者它的 latest 不再更新；
  - 2.x 的 API 不再标 Experimental，`@opencode/client` 发布了稳定版；
  - Owner 需要 2.x 的新功能，例如后台子代理、Code Mode。
- **B：现在就写 v2 适配器。** 工作量与 AJ-710（opencode 改用 `serve`）相当或更大：
  - 重写 `opencode_server.go`、`opencode_serve.go`、`opencode_sessions.go`；
  - 权限改用 v2 结构，凭据和 managed home 重新设计，处理模型 variant；
  - 重录 golden，在 CI 里加 2.x 的 OpenAPI 投影；
  - 重跑 AJ-718 的 14 个场景。

  做完后，1.x 和 2.x 要么同时支持，要么切换到 2.x 并放弃 1.x。

## 本机注意

在没有决定之前，**不要**在本机运行 `opencode upgrade --major`，也不要运行 v2 的 curl 安装器。如果已经升级了，回退方法是：先卸载 v2，再用 1.x 的安装方式重装（例如 `npm install -g opencode-ai@1`），然后运行 `pactify sub doctor opencode`。

## 来源

- [OpenCode 2.0 发布说明](https://anoma.ly/notes/opencode-2-0/)
- [Migrate from V1](https://opencode.ai/v2/docs/migrate-v1/)
- npm registry：`opencode-ai`、`@opencode/cli` 的 dist-tags 和发布时间，查询于 2026-10-17
