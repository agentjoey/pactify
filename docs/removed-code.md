# 已删除代码记录

> 只追加，最新在最上。每条写明：删了什么、为什么删、在哪个提交删、怎么找回。
> 本文件只记**删除**。变更的完整说明和验证门见 [`CHANGELOG.md`](CHANGELOG.md)。

## 怎么找回

- **单个文件或目录**：用删除它的合并提交的第一个父提交。

  ```bash
  git checkout <合并提交>^1 -- <路径>
  ```

- **只看不恢复**：`git show <合并提交>^1:<路径>`。
- **整套编排器方向**：从 tag `v0.12.0`（2026-10-15）检出。它是编排器方向的回滚锚点，tag 不删。
- 下文的行数是 `git diff --shortstat <合并提交>^1 <合并提交>` 的结果，含测试。

2026-10-16 之前的清理（云端、linx 共享架构、gemini-cli，AJ-664 等）见 [`cleanup-review-2026-10-16.md`](cleanup-review-2026-10-16.md)，本文件不重复。

---

## 2026-10-18

当天按"清理已经不符合 pactify 产品方向的代码"、"清理编排器代码"和"整体退役治理模式"共合入 6 个删除类 PR，约删 8 万行。之后：

- pactify 只剩 harness 路由器：CLI 只有 `serve`、`sub`、`version`；
- main 只剩一个前端（`/api/sub` 驱动的 Dashboard、Jobs、Settings）；
- 编排器和治理模式（pact 协议、账本、MCP、agent 接线）整体移除；
- `internal` 顶层包从 37 个减到 11 个。

### PR #163 · AJ-744 · 治理模式

- **合并提交**：`ab52423`（找回用 `ab52423^1`；整套也可从 tag `v0.12.0` 恢复）。
- **规模**：383 个文件，+339 / −31,600（含移入 `docs/archive/` 的 10 份文档）。
- **删除内容**
  - 12 个包：`internal/pact`、`ledger`、`event`、`projection`、`mcp`、`agent`、`agentreg`、`agentmanifest`、`registry`、`wizard`、`doctor`、`paths`。
  - CLI 命令：`init`、`join`、`assign`、`checkpoint`、`accept`、`changes`、`merge`、`cancel`、`withdraw`、`config`、`status`、`log`、`validate`、`seat`、`agent`、`setup`、`doctor`、`mcp`、`register`、`unregister`、`list`。
  - serve 的 vendor 预检和全部隐藏兼容参数（`--relay-url`、`--relay-token`、`--remote-control`、`--seat`、`--project`）。
  - 仓库文件：`.pact/`（本仓库自己的协议账本和任务记录）、`schemas/`（pact 事件 / 座席 / 任务 schema）、`.mcp.json`、`opencode.json`、`.gitattributes`、`.claude/skills/pact/`、`plugins/pact/` 及 marketplace 条目；`gitx` 中只给协议用的分支 / 合并 / 推送辅助函数；对应的 Go 测试和 bats（bats 从 136 个减到 39 个）。
  - 入口文件里的 pact protocol 段和 `pact:kinds` 行。
- **移到 `docs/archive/`**：pact-protocol、seat-identity、coordination-authority、single-canonical-ledger、review-runtime-deepening、driver-modernization、execution-tiering、onboarding、agent-onboarding、plugin-publishing。
- **原因**：编排器删除后，治理模式只剩手动协作，本仓库也不再使用（开发走 Notion、Linear、`pactify sub`）。Owner 于 2026-10-18 推翻了 10-15"保留为可选治理模式"的决定。
- **本机配套**（不在 git 里）：launchd plist 去掉 `--seat claude` 和 `PACTIFY_LEDGER_REF`（备份 `com.pactify.serve.plist.bak-20261018-*`）；主副本残留的本地 `.pact/` 运行数据移到 `~/.pactify/archive/pact-local-20261018/`。

### PR #158 · AJ-735 · 编排器

- **合并提交**：`adf7db4`（找回用 `adf7db4^1`；整套编排器也可从 tag `v0.12.0` 恢复）。
- **规模**：218 个文件，+330 / −30,313。
- **删除内容**
  - 13 个包：`internal/orchestrate`、`planner`、`schedule`、`audit`、`sarif`、`sessions`、`tokens`、`secret`、`recipe`、`finish`、`runguard`、`agentcfg`、`roles`。
  - CLI 命令：`orchestrate`、`plan`、`run`、`schedule`、`audit`（含 SARIF）、`sessions`、`recipe`、`finish`、`role`、`agent config`、`config gate`、`config critic`、`checkpoint --force`（对应 `cmd/pactify/cmd_{orchestrate,plan,run,schedule,audit,audit_sarif,sessions,recipe,finish,role}.go`）。
  - CLI 和 MCP checkpoint 上的 run guard（编排进行中拒绝 checkpoint）。
  - serve 的定时任务（`internal/serve/scheduler.go`）及项目发现；`tests/role_fallback.bats`。
- **原因**：产品方向是 harness 路由器，编排器自 2026-10-15 冻结。AJ-722/723/724 之后委派路径已不依赖它。
- **保留**
  - pact 协议、账本、投影、MCP server、agent 接线和项目注册表。
  - pact 写 `start`、`escalate`、`config_gate`、`config_critic` 事件的函数作为协议面保留，但 CLI 已无调用方；投影照常折叠旧日志里的这些事件。
  - `serve --seat`、`--project` 变成隐藏的空操作参数，启动时提示已忽略。

### PR #157 · AJ-734 · opencode 1.x 只读的 bash 白名单

- **合并提交**：`d3f37fc`（找回用 `d3f37fc^1`）。
- **删除内容**：1.x 只读的 15 条精确命令 allow、危险字符 deny 模式和 `orderedBashPolicy`（`internal/cockpit/opencode_permissions.go`）；doctor 里解析 opencode "被拒写入"日志的分支。只读改为整体禁用 bash。
- **原因**：白名单依赖 opencode 的 shell 解析，2.x 已出现解析不出命令就跳过权限检查的绕过（AJ-720 B1）。

### PR #154 · AJ-723 · serve 里的旧治理 API

- **合并提交**：`6e10e73`（找回用 `6e10e73^1`）。
- **规模**：101 个文件，+524 / −14,424。
- **删除内容**
  - serve 的旧路由及其 handler、DTO、SSE hub、watcher、testdata：projects、state、worktrees、events、编排审批、cockpit、registry 和 author verbs、agents、sessions、plan、setup、文件系统浏览、wiring、seats、stats、recipes、manifests、audit。
  - 三个 bats：`tests/serve.bats`、`tests/serve_ops.bats`、`tests/serve_author.bats`。
  - 包 `internal/stats`、`internal/diffstat`。
  - cockpit 的 Manager、Claude 后端，`bridge/claude-host/`（Claude Agent SDK 桥），`pactify doctor --setup-bridge`。
- **原因**：Web v2（AJ-704/705/706）只请求 `/api/sub/*`。审查确认这些路由除了 serve 自己的测试没有任何使用方。
- **保留**：`/api/sub`、CSRF 写保护、静态资源嵌入、serve 定时任务（`scheduler.go`）。`--seat`、`--project` 继续配置定时任务，因为本机 launchd plist 传 `--seat claude`。

### PR #153 · AJ-722 · 死代码与过期产物

- **合并提交**：`94693de`（找回用 `94693de^1`）。
- **规模**：100 个文件，+79 / −3,814。
- **删除内容**
  - web：17 个截图脚本（`web/scripts/*shot*.mjs`、`live-test-run.sh`，包括入口文件里还提到的 `live-gate-shot.mjs`、`shot-dispatch-review.mjs`）；ElementsGallery 及只给它用的组件和工具（`web/src/components` 18 个文件、`web/src/lib` 7 个文件）；只被测试引用的组件；`@phosphor-icons/react`；`web/public/*.svg`。两份 dist 重建。
  - Go：`gitx.Merge`、`sub.Store.FinalJobSnapshot`、`LastSessionFor`、`ResumeJob`、`prepareKimiHome`。
  - 其他：`dev/showcase/`（演示数据）、误入库的 `.pyc`（`internal/cockpit/opencodeapi/__pycache__/`）、`.claude/settings.json` 里过期的发布 hook、`.gitignore` 的 Rust `target/` 规则。
- **原因**：Web v2 合入后旧视图不再挂载；Go 函数经引用搜索和 `deadcode`（带 `record,harness_e2e,agye2e,acpsmoke` tag）确认不可达。
- **保留**：`Store.Actions`、`Store.Summarize`，当时仍被 serve 测试调用。

### PR #152 · AJ-724 · 委派路径与冻结包解耦

- **合并提交**：`bcb7ef2`（找回用 `bcb7ef2^1`）。
- **规模**：15 个文件，+262 / −182。以搬移为主，删除很少。
- **删除内容**：`agent.RemovedKindError` 的用法改为 cockpit 内联报错；厂商 API key 剥离逻辑从 `internal/orchestrate/env.go` 搬到新包 `internal/vendorenv`。
- **原因**：让 sub、subpanel、cockpit 不再导入任何冻结包，为之后整体删除编排器做准备。`internal/sub/deps_test.go` 会拦住新的反向依赖。

### 非代码清理（同日）

不在 git 里，列在这里备查：

- `/private/tmp` 下 1218 个 pactify 临时目录，以及 46 个评估 worktree。
- `~/.pactify` 从约 4.2G 降到 587M：清掉了 go-build 缓存，保留 go-mod 和 npm 缓存供离线任务使用。
- 已取消的 AJ-689 LAN 补丁归档到 `~/.pactify/archive/aj689-lan-panel-canceled-20261016.patch`；backlog 的 `.bak` 文件移到 `~/.pactify/archive/backlog-backups/`。
- 删除本地分支 `feat/aj720-opencode-v2`、`feat/aj720-opencode-v2-impl`、`staging`。
- 主副本里残留的未跟踪 `bridge/claude-host` 安装（约 273M 的 `node_modules`）。
