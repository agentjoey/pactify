# pactify 清理盘点：云端 / linx 共享架构 + gemini-cli 集成

> 归档路径注记（2026-10-16，AJ-665）：下文四份云端文档已移至 `docs/archive/`；其余内容保留原审查时的事实与建议。

> 只读审查，基于 `main` @ `182afa9`（2026-10-16）。主仓库里没有改动任何文件。
> 证据全部来自 `go list`、`git grep`、`go mod why`、`go build`/`go vet`，以及两次在 scratchpad 里做的**删除模拟**（用 `git archive HEAD` 导出，不是 worktree）：
> - **simA**（Go 侧删云）：删掉 A 组的 Go 文件，再按下文改 5 个保留文件 → `go build ./...` 通过、`go vet ./...` 通过、`go mod tidy -diff` 只去掉 `gorilla/websocket` 和 `golang.org/x/crypto`；`go test ./internal/serve ./internal/orchestrate ./internal/gitx ./internal/acp ./internal/cockpit` 全绿，只有 `TestFixtureExport` 失败（因为 simA 没导出 `cloud/`，见 A5）。
> - **simB**（Go 侧删 gemini-cli）：按 B 组改 12 个文件 → build 通过；vet 只报 `runner_test.go` 里的 `geminiKey`；`go test ./internal/...` 红了 26 个测试（另有 `TestGeminiEnv` 因编译不过被直接删除），全部在下文 B 表里，没有别的。
> 两次模拟都**没有**跑 `cmd/pactify` 测试（按要求）。web（tsc/vitest/playwright）和 bats 没有模拟，下面关于它们的结论只来自静态分析。

---

## 1. A 组：云端 / 与 linx 的共享架构

### 1.0 「跟 linx 的共享架构」具体指什么（有证据）
- `cloud/` 下的 `@pactify-apps/{wire,crypto,relay,pact-project,relay-client}` 都是 0.1.0。CI 工作流 `publish-cloud.yml` 把它们发布到 npm（`relay` 是 restricted，其余 public）。
- **linx 从 npm 消费这些包**（只读查看了 `~/AgentWorks/CodeSpace/pactify-apps/linx`）：`package.json:49` `@pactify-apps/relay ^0.1.0`；`packages/core/package.json:22-23` crypto+wire；`packages/linx-web/package.json:31,41`；`packages/linx-cli/package.json:24,29`。lockfile 里是 registry 解析（`pnpm-lock.yaml:947` 带 integrity）。linx 最近一次提交写着 “remove stale relay deploy workflow (relay lives in pactify repo)”。
- 生产环境的 relay 与 linx 共用：`docs/deployment.md:22`（“Prod relay is **shared with linx**”）；`docs/archive/release-process.md:30-31`（两个 relay 共用一个 Neon 项目 `pactify-cloud`，“linx shares the same relays”）；`cloud/relay/prisma/schema.prisma:1`（“Linx relay data model”）。
- Go 二进制会**读取 linx 的密钥**：`cmd/pactify/cmd_account.go:18-19` 写明 master secret 依次从 `$PACTIFY_MASTER_SECRET`、`$LINX_MASTER_SECRET`、`~/.config/pactify/master-secret`、`~/.config/linx/master-secret` 读取（实现在 `internal/cloudauth/secret.go`）；配对流程让用户去 “linx web/CLI” 批准（`cmd_account.go:33,50`）。

### 1.1 盘点表

| # | 路径 | 是什么 | 组外引用（file:line） | 删除时要改哪些保留代码 | 风险 |
|---|---|---|---|---|---|
| A1 | `cloud/`（131 个文件，不算 lockfile 约 15.5k 行）：`wire/`、`crypto/`、`relay/`（Prisma、12 个 migration、`Dockerfile`、`bin/pactify-relay.mjs`）、`pact-project/`、`relay-client/`、`fly.toml`、`fly.staging.toml`、`pnpm-workspace.yaml`、`pnpm-lock.yaml`、`package.json`、`.npmrc`、`tsconfig.base.json` | relay 服务、zod 线协议、E2E 加密、TS 版投影、浏览器 relay 客户端，以及 fly 部署配置 | web：`web/vite.config.ts:27-28,47-58`、`web/tsconfig.app.json:23-26`、`web/vercel.json`（installCommand 里 `cd ../cloud && pnpm install`），以及 A7 列出的 TS import。Go 测试：`internal/serve/cockpit_decision_conformance_test.go:22` 和 `internal/serve/remotechannel_test.go:68` 读 `cloud/wire/src/rpc.ts`；`internal/serve/fixture_export_test.go:26` 读 `cloud/pact-project/testdata/`。CI：`.github/workflows/ci.yml:33-42,72-103,115-123`；`.github/workflows/publish-cloud.yml`。`internal/doctor/bridge_test.go:21` 只是在 TempDir 里用了 `cloud/relay` 这个目录名，不依赖真实目录 | 必须先完成 A2/A3/A5（Go）和 A7/A8（web），否则 web 构建和 Go 测试会断。`TestFixtureExport` 的 golden 要搬家，见 A5 | **高（跨仓库）**：这里是 linx 依赖包的唯一源码。删之前 Owner 要决定移交还是归档（§5 Q1） |
| A2 | Go 包 `internal/cloudauth`、`internal/cloudclient`、`internal/relaysock`、`internal/remoteexec`、`internal/remotemachine`、`internal/machineid`（合计 3,440 行） | 账户密钥派生和加解密、relay HTTP 客户端和配对、websocket、远程 pact.* rpc 执行器、机器通道、机器 ID | `cmd/pactify/cmd_account.go:8-9`、`cmd/pactify/remote_dispatch.go:7-10`、`internal/serve/relay.go:17-18`、`internal/serve/cockpit_remote.go:11,13`、`internal/serve/provision.go:12`、`internal/serve/remotechannel.go:12,14,15`、`internal/serve/stint.go:15`（以及对应的 `_test.go`）。除此之外没有任何导入者（`go list -f '{{.Imports}}'`） | 无，只要和 A3/A5 一起删 | 低（编译期就能发现问题；simA 已验证） |
| A3 | `cmd/pactify/cmd_account.go`、`cmd_account_test.go`、`remote_dispatch.go`；`commands.go:279` 里的 `newAccountCmd()`；`cmd_orchestrate.go` 的 `--seat-host`（`:58` 变量、`:123-135` 解析、`:150-162` RemoteRunner 接线、`:245` flag）；`cmd_serve.go` 的 `--relay-url`、`--relay-token`、`--remote-control`（`:19-21`、`:51` `SetRelay`、`:54-58` `StartMachineChannel`、`:63-70` 打印、`:76-78` flags） | CLI 子命令 `pactify account {pair,login,whoami,token}`、多机编排、serve 上报 relay 和远程控制 | bats 和 `cli_test.go` 都没有引用（`git grep account/relay/seat-host tests/` 无结果）；`docs/operations.md:49` 有一条示例命令 | 要编辑 `commands.go`、`cmd_orchestrate.go`、`cmd_serve.go`（simA 已改通，包括删掉多余的 `seatHosts` 声明） | **中**：本机 `~/Library/LaunchAgents/com.pactify.serve.plist` 正在传 `--relay-url https://pactify-relay-staging.fly.dev --remote-control`。删掉 flag 后 cobra 会报 unknown flag，launchd 会反复拉起失败。合并前必须先改 plist，或者保留一版隐藏的 no-op flag（§5 Q3） |
| A4 | `internal/orchestrate/remote.go`、`remote_test.go` | `RemoteRunner`、`StintDispatch`、`StintRPC`（M3 分布式编排） | 只有 `cmd/pactify/cmd_orchestrate.go:156` 和 `remote_dispatch.go:17,36` 用到 | **`remote_test.go` 里定义了别的测试要用的 helper**：`gitT`（`remote_test.go:14`，被 `checkout_feature_test.go:87` 等 9 处使用）和 `runnerFunc`（`:163`，被 `heartbeat_lifecycle_test.go:25` 使用）。要把这两个挪到新的 `helpers_test.go` 里，`setupDriverWorker` 和 `workerCheckpoint` 可以一起删。`gitx.HasRemote/Push/Fetch` 仍被 `commands.go:163`、`pact/engine.go:785-786` 使用，保留 | 低（simA 已验证） |
| A5 | `internal/serve/`：`relay.go`（599 行）及 `relay_test.go`、`relay_integration_test.go`、`relay_nonblock_test.go`；`cockpit_remote.go` 及其 `_test`；`cockpit_decision_conformance_test.go`；`provision.go` 及其 `_test`；`stint.go` 及其 `_test`；`remotechannel.go` 及其 `_test` | 账本事件 E2E 上传、relay 下行通道（远程 cockpit 镜像、远程 stint/orchestrate/plan、远程 clone）、机器注册，以及 `.pact/remote.json` 策略 | 只被 `cmd_serve.go:51,58` 和 serve 内部这几处调用：`api.go:62`（`relay *relay` 字段）、`:67-70`（`cockpitEmit`）、`:81-105`（`SetRelay`、`set/getCockpitEmitter`）；`watch.go:39-70`（relay 回放的 seed，以及包住 watchLoop 的 goroutine）、`:135`、`:182`、`:200-202`；`watch_test.go:86-137`（`TestDrainNewEnqueuesToRelay`） | 编辑 `api.go`（删字段和方法，`fmt`、`os`、`sync/atomic` 三个 import 会变成未使用）、`watch.go`（简化成 `go s.watchLoop()`）、`watch_test.go`（删一个测试和 4 个 import）。**`fixture_export_test.go`** 是 serve DTO 的 golden 回归测试，数据在 `cloud/pact-project/testdata/{events,state}-golden.jsonl/json`，同一份数据也被 `cloud/pact-project/test/parity.test.ts` 使用。删 `cloud/` 时要把它搬到 `internal/serve/testdata/` 并改 `goldenTestdataDir`，或者整个删掉 | 中：`watch.go` 的启动顺序（RELAY-2 注释）要保持“watchLoop 在后台跑”。simA 中 serve 测试除 `TestFixtureExport` 外全绿 |
| A6 | `go.mod`/`go.sum`：`github.com/gorilla/websocket v1.5.3`、`golang.org/x/crypto v0.53.0` | 只被云端包使用 | `go mod why`：x/crypto 只来自 `internal/cloudauth`，websocket 只来自 `internal/relaysock` | 跑 `go mod tidy`（CI 有 `go mod tidy -diff` 门） | 低（simA 的 tidy diff 已确认） |
| A7 | web 托管模式（约 3.7k 行，含测试）。整文件删除：`web/src/lib/relaysource.ts`(+test)、`lib/identity.ts`(+test)、`lib/cryptoLink.test.ts`、`lib/pactProjectLink.test.ts`、`components/IdentityGate.tsx`(+test)、`UnlockPanel.tsx`、`AccountPanel.tsx`(+test)、`Machines.tsx`(+test)、**`TaskDetail.tsx`(+test)**（只在 `App.tsx:634-635` 的 `multiMachine` 分支里渲染）。部分删除：`lib/source.ts`（`RELAY_BY_HOST:17-20` 写死了 `orx.agentjoey.ai`/`orx.pactify.dev` → `*.fly.dev`，还有 `isHostedMode`、`relayUrl`、`hex*`、`connectRelaySource`、`connectSessionSource`）、`lib/source.test.ts` | 托管看板（Vercel）、SSO/账户/身份绑定、主密钥解锁、机器列表、zero-knowledge relay 数据源 | `App.tsx:6-10,62,157-170,206-213,488,504-505,579,634-636,646-653`；`components/NoProjects.tsx:4,21,25-`（`NoProjectsHosted`）；`components/shell/SettingsModal.tsx:8-9,25,30,51,58,65-66,131-133,159-163,217-220,435,449,474`（Account/Machines 两个分区和 `hosted` 分支）；`lib/datasource.tsx:19,24,30-31,43-46,113`（`multiMachine`、`locked`、`getMachines`、`Machine`）；`lib/types.ts:147-155`（`Machine`）；`components/shell/DispatchPanel.tsx:29-34,74-76,213`（`hosted = getPlanGenStatus == null` 的一次性分支）；`board/RunRail.tsx:60-76,142,249`（托管注释和降级逻辑）；`Board.tsx:45`、`RightRail.tsx:286,333,343,363,392`（文案 “Remote control needs U3”）；`setupTests.ts:35-45`（WebCrypto polyfill 只给 `@pactify-apps/crypto` 用，`git grep randomUUID\|getRandomValues web/src` 没有其他使用者）。涉及的测试：`App.test.tsx:319,330`、`Board.test.tsx:161`、`RightRail.test.tsx:221`、`RunRail.test.tsx:317,351,375`、`NoProjects.test.tsx:9-17,75`、`SettingsModal.test.tsx:103`、`DispatchPanel.test`、`FallbackCard.test`、`fallbackContract.test`、`api.test`（这几个有 hosted/relay 用例，删前逐个看） | 改动面大，涉及上面所有文件；`internal/serve/dist` 要重建（CI 有 “embedded dist in sync” 门，`ci.yml:61-65`） | **中**：UI 改动要过 vitest + Playwright 两道门，再按 CLAUDE.md 做截图视觉门。`DataSource` 抽象本身（`LocalServeSource` 与可选方法）可以留到第二步再收敛（§3 C3） |
| A8 | web 构建配置：`web/vite.config.ts:26-33`（`crypto-relay` 分块）、`:40-58`（4 个 alias）；`web/tsconfig.app.json:17-27`（`paths`）；`web/vercel.json`（整文件，Vercel 托管构建） | 把 `cloud/` 的源码直接链接进 web 构建 | A7 的 import 全部删掉之后就没有使用者了 | 和 A7 同一个 PR | 低 |
| A9 | CI：`.github/workflows/publish-cloud.yml`（81 行，整文件）；`ci.yml` 的 `cloud:` job（`:72-103`），以及 `test`、`e2e` 两个 job 里的 “cache pnpm store” 和 “install cloud workspace deps” 步骤（`:33-42`、`:115-123`）；注释 `:14-18`、`:66-71` | 云端 workspace 的测试和发布 | 依赖 `cloud/` 存在（`cd cloud && pnpm install`） | 必须和删 `cloud/` 放在**同一个 PR**，否则 CI 立刻失败 | 低。另外仓库 secret `NPM_TOKEN` 会变成无用（§5） |
| A10 | 文档：整篇只讲云端的有 `docs/archive/release-process.md`（87 行，三分支加 fly/Vercel/Neon）、`docs/archive/linx-coordination.md`（55 行）、`docs/archive/agentworks-wire.md`（359 行）、`docs/archive/account-sso.md`（211 行）。只有部分涉及的：`docs/deployment.md`（`:5-62` 的环境/拓扑/CI 讲云端；`:63-115` 的安装、开发环境、pactify.dev 段**保留**）、`docs/operations.md`（头部 `:5-15` 告警、`:41-70` “relay 配置与运维”、`:76-77` 链接）、`docs/architecture.md`（`:31-` 通讯架构、`:50` “M3.4 relay 接口”、状态行）、`docs/specs/coordination-authority.md`（7 处 linx）、`docs/specs/single-canonical-ledger.md:192`、`README.md:15,141` | 退役云端的设计和运维文档 | CLAUDE/AGENTS/GEMINI.md `:24` 链到 `docs/deployment.md`；`operations.md:76-77` 链到 release-process/deployment；cloud 源码注释里的链接会随源码一起消失 | 改完要同步三份 entry 文件 | 低。建议移到 `docs/archive/` 而不是直接删（这些 spec 记录了与 linx 冻结的协议 v1） |
| A11 | 三份 entry 文件（CLAUDE.md、AGENTS.md、GEMINI.md 同步）：`:19`（“云端已退役…随后单独 PR 删除”）、`:24`（Deployment 链接）、`:29`、`:44`（“37 包”，实际已经是 42 个 internal 包，删除后是 36 个）、`:32`（Tech Stack 的 Cloud 行）、`:49`（`cd cloud && pnpm -r build`） | 项目说明 | — | 按 “Release 后必做” 第 3 条三份一起改，不要动托管块 | 低 |
| A12 | 残留的防御性代码（**建议保留**）：`internal/acp/acp.go:209`、`internal/cockpit/jsonrpc.go:294`、`internal/orchestrate/env.go:18` 过滤 `PACT_RELAY_TOKEN` 和 `PACTIFY_*` 环境变量；`internal/orchestrate/sec_c2_env_leak_test.go` | 防止 relay token 泄漏给子进程 | — | 不用改。`PACTIFY_*` 前缀过滤仍然有用，`PACT_RELAY_TOKEN` 这个字面量可以以后顺手删 | — |
| A13 | 本机和 repo 运行时残留（不在 git 里）：`.pact/relay-uploaded.json`（被 `.git/info/exclude:25` 排除，由 `relay.go:445` 写入）；`~/.config/pactify/{session.json,machine-id}`；`~/.config/linx/master-secret`（**linx 的，不能删**）；launchd plist 的旧备份 `com.pactify.serve.plist.pre-relay.bak` | — | — | 由 Owner 手动清理 | — |

**不属于云端、要保留的：** `bridge/claude-host/`（本地 Claude Agent SDK 桥，由 `internal/cockpit/claude.go` 和 doctor `--setup-bridge` 使用）；`internal/serve/cockpit.go`（本地深度集成 cockpit）；`internal/gitx` 的 remote 系列函数；以及 `internal/pact/merge_empty_test.go:14-38`、`sec_m9_merge_recovery_test.go:54`、`orchestrate/parallel.go:280`、`loop_test.go:420`、`schedule/*_test.go` 里出现的 “linx”。这些地方的 linx 是“一个使用 pact 协议的项目 / 历史 bug 来源”，不是共享架构。

---

## 2. B 组：gemini-cli 集成

### 2.1 盘点表（要删的）

| # | 位置 | 内容 | 组外引用 / 连带 | 测试（simB 实测会红的） | 风险 |
|---|---|---|---|---|---|
| B1 | `internal/agent/agent.go:119` | kind 注册 `"gemini-cli"`（entry 是 `GEMINI.md`，MCP 配置是项目级 `.gemini/settings.json`，命令 `gemini`）；`:45` 有一处注释 | `agent.Kinds()` 被 `cmd_agent.go:128`、`cmd_setup.go:55-88`、`commands.go:35`、`agentmanifest/manifest.go:178`、`doctor/relevance.go:97`、`doctor/vendor.go:94`、`serve/agents.go:70` 使用，它们会自动少一个 kind | `agent_test.go:10` `TestKindsSortedAndComplete`、`TestRunnerCLIKinds`；`entrymark_test.go:62-191` 中 `TestWireGeminiCliDoesNotWireAntigravity`、`TestWiredConfigKindsStillReportWired`、`TestLegacyBlockCreditsNeitherGeminiKind`（**这 3 个其实是在保护 antigravity 与 gemini-cli 共用 GEMINI.md 时互不误判，删除时要改写成只验证 antigravity，不能直接删**）；`probe_test.go:159-161`；**`briefing_test.go:136` `TestDocFileInSync`：`docs/agent-onboarding.md` 是生成文件，要用 `pactify agent docs` 重新生成（`:301-309` 的 gemini-cli 段会消失）** | 中 |
| B2 | `internal/agent/launch.go:81-97` | `runnerProfiles["gemini-cli"]`（`-p`、`--approval-mode yolo`、`--skip-trust`，模型 `gemini-3.1-pro-preview`/`gemini-3-flash-preview`）；`:40` 有注释 | — | `launch_test.go:10-66,105,205`（`TestLaunchProfile_DefaultsMatchRunner`、`_ModelOverride`） | 低 |
| B3 | `internal/orchestrate/runner.go:252-268`、`:610-626` | `geminiEnv` 从 Keychain（`pactify/gemini`）注入 `GEMINI_API_KEY`；`GEMINI_CLI_TRUST_WORKSPACE=true`；`geminiKey` | **`internal/secret` 包不能跟着删**：它还被 `runner.go:30,37` 的 GLM token 使用 | `runner_test.go:111-130` `TestGeminiEnv` | 低 |
| B4 | `internal/orchestrate/env.go:55` | `vendorOwnKeys["gemini-cli"]` | `allVendorAPIKeys`（`:33-47`）里的 `GEMINI_API_KEY`、`GOOGLE_API_KEY`、`GOOGLE_APPLICATION_CREDENTIALS` **必须保留**：agy 靠把它们清空来强制走 OAuth 文件（`env.go:56-80`），`sub` 通过 `orchestrate.CrossVendorStrip` 使用（`sub/harness.go:72`、`sub/worker.go:140`、`sub/doctor.go:163`） | `sec_m11_vendor_key_isolation_test.go:12-39,104` `TestSEC_M11_CrossVendorStrip`（改断言，不删测试） | **中**：误删 key 列表会让 agy 委派继承用户的 Google key |
| B5 | `internal/orchestrate/acprunner.go:446-450`（以及 `:432-433` 注释） | ACP 传输 `gemini --acp` | — | `acprunner_test.go:353,360` `TestAcpCommandNativeBinariesForAcpTier` | 低 |
| B6 | `internal/cockpit/acp.go:101-102`（以及 `:15` 注释） | cockpit ACP backend 支持 gemini-cli | 由 `internal/serve/cockpit.go:102` 调用；`sub` 不走这条路（`sub/harness.go` 只用 Agy、Codex、kimi 的 backend） | — | 低 |
| B7 | `internal/serve/cockpit.go:76,95,102,105` | `cockpitCapableKind` 和 `backendForKey` 里的 gemini-cli 分支 | web 端 `CockpitPanel.tsx:41-47` 有一份同步的列表 | `serve/seatkind_channel_test.go:282-283`、`orchestrate_test.go:161-164`、`agents_test_endpoint_test.go:80`（fixture） | 低 |
| B8 | `internal/serve/remotechannel.go:160` | `pactToWireKind["gemini-cli"]` | 会随 A5 一起删，如果 A 先合并就不用单独处理 | `remotechannel_test.go:36` `TestMachineAgentKindsAdvertisesAntigravity` | — |
| B9 | `internal/sessions/sessions.go:61`，以及只服务 gemini 的机制 `IndexDelete` 字段（`:24-34,99,107,128-160,176`）和 `geminiIndexRe`（`:67-68,147`） | gemini 会话的 list 和按序号删除 | `serve/sessions.go` 和 `cmd_sessions.go` 走通用 API，删了 kind 之后 `IndexDelete` 分支就没有使用者了（C2） | `sessions_test.go`（`TestSupported`、`TestCanPrune`、`TestList`、`TestPrune`，17 处）；`serve/sessions_test.go:17-19,60` `TestHandleSessionsList`；`sessions/antigravity_test.go:99` 只是把 gemini-cli 当作“非 agy kind”的样例，删掉那一项就行 | 低 |
| B10 | `internal/doctor/vendor.go:19`（`acpKinds`）、`:48-50`（`vendorAuth`，查 `~/.gemini/oauth_creds.json`、`google_accounts.json`）；`relevance.go:33` 有注释 | vendor 预检 | `cmd_serve.go` 启动时会调 `doctor.VendorChecks` | `vendor_test.go`（`TestVendorChecks_Binary`、`_Auth`、`_GeminiAuthAlternatives`、`_TransportClassification`）；`relevance_test.go`（`TestRelevantKinds_SeatNameHeuristic`、`TestRun_IrrelevantVendorFailureIsAdvisoryButStillShown`）；`cmd/pactify/cmd_doctor_relevance_test.go`（2 处，simB 没跑，需要在 worktree 里验证） | 低 |
| B11 | 审计 hook 的 `"gemini"` kind：`internal/audit/install.go:76-80,160-161,179,183`（往 `.gemini/settings.json` 写 `BeforeTool` hook）；`cmd/pactify/cmd_audit.go:27,29,48,57-58,68,74,82,84,91,104,236`（`--gemini` flag 和 help 文案） | `pactify audit install --gemini` | `audit/hook.go:35` 的 `antigravityInput` **保留**（只是注释提到 gemini-lineage） | `install_test.go`（`TestInstallGeminiIdempotentPreservesKeys`、`TestDetectGemini`）；`hook_test.go:43-75`（共 8 处，需要逐个看是不是只把 “gemini” 当 kind 字符串用）；`cmd_audit_test.go:81-100`（simB 没跑） | 低。用户脚本里写了 `--gemini` 的会报 unknown flag |
| B12 | `cmd/pactify/cmd_orchestrate.go:79` | help 文案里的 kind 列表 | — | — | 无 |
| B13 | `internal/agentreg`、`internal/planner`、`internal/wizard` 的测试 | 用 gemini-cli 当 fixture | — | `agentreg/config_test.go:53-67` `TestConfigSurvivesRoundtrip`；`wizard/wizard_test.go:9-30` `TestSuggest_ClaudeLeadsOthersWork`；`planner/dynamic_seat_test.go:34,64-65,89`（simB 下没红，可以保留或顺手改）；`wizard.go:25` 是注释 | 低 |
| B14 | web：`components/CockpitPanel.tsx:45`、`lib/agentLogos.tsx:63,76,87`、`lib/icons.tsx:51`、`components/ElementsGallery.tsx:144,303`、`components/ops/AgentsPage.tsx:212`（提示文案）、`lib/api.ts:278`（注释）、`components/shell/AddProjectWizard.tsx:40`、`web/scripts/gallery-shot.mjs:15` | 前端的 kind 列表、logo、图标、gallery | **`AddProjectWizard.tsx:38-42` 的 `entryFor`：`kind.startsWith("gemini") → GEMINI.md`。删掉这一行后要加 `antigravity → GEMINI.md`**，否则 GEMINI.md 的映射就没了。另外它现在就把 antigravity 映射成 AGENTS.md，这是现存 bug，见 C1 | `ops/AgentConfig.test.tsx:37,44`、`shell/AddProjectWizard.test.tsx:123-140` 用了 gemini-cli 这个 kind；`Dashboard.test`、`dashboard.test`、`Board.test`、`Agents.test`、`Machines.test`、`relaysource.test` 里的 “gemini” 只是座席名或二进制名，不用改 | 低。要重建 dist |
| B15 | 仓库里被 git 跟踪的 `.gemini/settings.json` | gemini-cli 的**项目级** MCP 配置（`pact` MCP server） | agy 的 MCP 配置是全局 `~/.gemini/config/mcp_config.json`（`agent.go:131`）；`audit/install.go:51-52` 实测 agy 不读 `.gemini/settings.json` 的 hooks。**agy 是否读项目级 `.gemini/settings.json` 的 MCP 段，没有在代码或文档里找到证据，不确定** | — | 低到中，建议删前用 agy 实测一次 |
| B16 | 文档：`docs/agent-onboarding.md:301-309`（生成的）、`docs/operations.md:103`（kind 列表）、`:236`（Gemini 免费档降级）、`docs/architecture.md:311,330,358`、`docs/specs/driver-modernization.md:67,103,126-127`、`docs/specs/seat-identity.md:27,91`、`README.md:5,48`、`docs/onboarding.md:3`、三份 entry 文件 `:21`（`[ACP-MODEL]` 那一行里的 gemini-cli）、`dev/showcase/README.md:7`，以及 `dev/showcase/.pact/*` 里的 gemini 座席（演示 fixture，可以不动） | — | — | — | 低 |
| B17 | 本机和本仓库的 roster 数据：`~/.pactify/agents.json` 注册了 `gemini-cli`（model `gemini-3.1-pro-preview`）；本仓库 `.pact/PROJECT.md:19` 和 `.pact/STATE.yml:6` 有座席 `gemini-worker`（entry `GEMINI.md`，治理模式的历史座席） | — | `agentreg.Load` 遇到未知 kind 不报错（`agentreg.go:35-47`），`Register` 才报错（`:62-63`）；`commands.go:35` 对 seat spec 里的未知 kind 会报 `unknown agent kind` | — | 低，§5 Q6 |

### 2.2 附近必须**保留**的 agy / antigravity 代码（不要误删）
- `internal/sub/agy_home.go`（`agyTokenPath = ".gemini/antigravity-cli/antigravity-oauth-token"`，镜像 HOME 时排除 `.gemini`）、`internal/sub/managed_home*.go`、`internal/sub/harness.go:71-85`（`"agy"` harness）、`agy_test.go`、`agy_readonly_test.go`、`managed_home_test.go:18-114`（里面的 `.gemini` 是 agy 的数据目录）。
- `internal/cockpit/agy.go`、`agy_process.go`、`agy_*_test.go`（`AgyModels` 是动态模型表，`gemini-3.8-*` 这些名字是**经 agy 调用的 Gemini 模型名**）。
- `internal/agent/agent.go:124-131`（`antigravity` kind，**entry 就是 `GEMINI.md`**）；`launch.go:150-197`（antigravity 的 profile，模型名是 `gemini-3.7-*`）。
- `internal/sessions/antigravity.go:11-42`（`~/.gemini/antigravity-cli`）、`internal/orchestrate/runner.go:196,348-384,431,538`、`orchestrate/cleanup_antigravity_test.go`、`agy_e2e_test.go`（带 `agye2e` build tag）。
- `internal/orchestrate/env.go:33-47` 的 Google key 列表，以及 `:56-80` 的 antigravity 空切片（B4）。
- `internal/doctor/vendor.go:55-66` 的 antigravity 认证探针；`internal/audit/hook.go:34-70`（`antigravityInput`）和 `install.go` 的 antigravity 分支（`.agents/hooks.json`）；`cmd_audit.go` 的 `--antigravity`。
- 文档里的 `gemini-3.7-*` 和 agy profile 示例（`operations.md:166-194`）、`docs/sub-doctor.md:76-77`、`plugins/pactify/README.md:149-150`、`README.md:106`，以及插件 `plugins/pactify/{agents,skills}/agy*`。

### 2.3 GEMINI.md 是产品集成还是贡献者入口？
**两者都是，而且现在承载它的是 agy，所以必须保留。**
- 它是本仓库的贡献者入口文件之一：CLAUDE.md:58 “三份 entry 文件……必须一致”；GEMINI.md:1 标题是 “GEMINI.md：gemini-cli · antigravity/agy”。
- 它也是产品层面的 entry 契约：`internal/agent/agent.go:124-131` 写明 antigravity（agy）读 `GEMINI.md`、不读 `AGENTS.md`（2026-08-22 实测）。`entrymark.go:12` 记录 GEMINI.md 由 gemini-cli 和 antigravity 共用。删除 gemini-cli 之后，GEMINI.md 就是 agy 唯一的入口。
- 需要改的是 GEMINI.md 托管块里的归属行 `GEMINI.md:68 <!-- pact:kinds: gemini-cli -->`。删除 gemini-cli kind 后，这个归属指向一个不存在的 kind。建议在 B 组 PR 里改成 `antigravity`，方式有两种：一是重新 bake（`pactify agent add antigravity`，**但这个命令会写全局的 `~/.gemini/config/mcp_config.json`，有副作用**）；二是只手改托管块内这一行（CLAUDE.md:58 要求“不要动托管块”，这一步需要 Owner 授权）。§5 Q7。
- 结论：**保留 GEMINI.md**，正文去掉 gemini-cli 字样，标题改成 agy 专用；托管块的处理由 Owner 决定。

---

## 3. C 组：其他死代码或冲突（只列有证据的）

| # | 项 | 证据 | 建议 |
|---|---|---|---|
| C1 | **现存 bug**：web 新建项目向导把 antigravity 座席的 entry 设成 `AGENTS.md` | `web/src/components/shell/AddProjectWizard.tsx:38-42`（只有 claude 和 gemini 前缀映射到非 AGENTS.md），`:48` 把它写进 `SetupApplySeat.entry`，`internal/serve/setup.go:109` 拼成 seat spec。但 `internal/agent/agent.go:124-131` 写明 agy 看不到 AGENTS.md | 在 B14 里一起修：`antigravity → GEMINI.md`（最好直接用后端 `agent.Get(kind).EntryFile`，不要在前端再维护一份映射） |
| C2 | 删除 gemini-cli 后 `sessions.Spec.IndexDelete` 和 `geminiIndexRe` 没有使用者 | `sessions.go:61` 是唯一一个 `IndexDelete: true` | 和 B9 一起删 |
| C3 | 删除托管模式后，`DataSource` 的托管专用能力和可选方法没有实现者 | `datasource.tsx:24`（`multiMachine` 只有 RelaySource 是 true）、`:31`（`locked`）、`:44-46`（`getEvents?`/`getMachines?` 的注释写着 “Hosted-mode sources may…”）；`DispatchPanel.tsx:34` 和 `RunRail.tsx:63-76` 的“方法不存在就降级”分支只为 RelaySource 存在 | A7 先删字段和分支；把 DataSource 收敛成 LocalServeSource 是可选的第二步 |
| C4 | 编排器（冻结中）的 agy 静态模型表过期 | `internal/agent/launch.go:184-187` 写的是 `gemini-3.7-*`/`gemini-3.1-pro-*`；`docs/specs/harness-subagents.md:268-269` 已实测“不被识别”；`sub` 走的是动态表（`cockpit.AgyModels`） | 不在本次范围，随降级冻结，只记一笔 |
| C5 | 文案残留 “Remote control needs U3” 在本地模式下也会显示 | `Board.tsx:45`、`RightRail.tsx:333,343,363,392` | 放进 A7，改成“需要 acting seat”之类的本地文案 |
| C6 | `internal/serve/stint.go:177-225` 的 `serveOrchestrator`/`servePlanner` 只是远程 rpc 的适配层 | 只被 `remoteexec` 的接口引用 | 已经包含在 A5 |
| C7 | 未跟踪的本地文件 | `git status`：`docs/backlog.md.bak-20260913-010149`、`.bak-20261015`、`.bak-20261015b`（不在 git 里） | 不属于仓库内容，Owner 自行处理 |

没有发现其他零导入者的 Go 包：`go list` 统计结果里，入度为 0 的只有 `internal/cockpit/codexschema` 和 `internal/testenv`，两个都只在测试里用，而且有用途（`codex-schema.yml` 和 `testmain_test.go`）。

---

## 4. 依赖关系与删除顺序

```
A-Go（PR1） ─┐
             ├──> 删 cloud/ + CI（PR3） ──> 文档与 entry 文件（PR4）
A-web（PR2）─┘
B-Go（PR5） ──> B-web 与仓库清理（PR6）     （B 线和 A 线互不依赖；PR2 和 PR6 都要重建 internal/serve/dist，必须串行，不能并行）
```
硬约束：
1. `cloud/` 不能先删：Go 测试读 `cloud/wire/src/rpc.ts`（随 PR1 删掉）和 `cloud/pact-project/testdata`（PR3 里搬走）；web 用 alias 直接编译 `cloud/` 的源码（PR2 去掉）；CI 会 `cd cloud`（PR3 改）。
2. 删 `remote_test.go` 时要先把 `gitT` 和 `runnerFunc` 挪出来，否则 orchestrate 的测试编译不过。
3. web 改动必须带上重建后的 `internal/serve/dist`（`ci.yml:61-65`）。
4. PR1 合并之前，Owner 的 launchd plist 必须去掉 `--relay-url` 和 `--remote-control`（或者 PR1 先保留隐藏 flag）。
5. `cmd/pactify` 的测试只能在 worktree 里跑（会把主副本当真实仓库污染，见 memory）。

### 提议的 PR 序列（每个都是最小的安全单元，各自单独提、单独确认，与 spec §8 一致）

**PR0（Owner 的仓库外动作，没有代码）**：决定 `cloud/` 的去向（移交 linx，或者打 tag 归档，例如 `git tag cloud-final`）；改 launchd plist；把下文 §5 的外部资源决定下来。

**PR1 · Go 侧删云**（A2、A3、A4、A5 中除 fixture 外的部分、A6）
- 删除：A2 的 6 个包；A3 的 3 个文件和对应 flag；A4 的 `remote.go`（`remote_test.go` 里的 helper 挪到 `helpers_test.go`）；A5 的 13 个 serve 文件；`watch_test.go` 里的 `TestDrainNewEnqueuesToRelay`。
- 编辑：`commands.go:279`、`cmd_orchestrate.go`、`cmd_serve.go`、`serve/api.go`、`serve/watch.go`；跑 `go mod tidy`。
- 验证：
  ```bash
  go build ./... && go vet ./... && go mod tidy -diff
  git grep -n -E 'cloudauth|cloudclient|relaysock|remoteexec|remotemachine|machineid|seat-host|relay-url|remote-control' -- '*.go'   # 期望没有结果
  # 在 worktree 里：
  go test ./...
  go test -race ./internal/{pact,orchestrate,serve}/...
  bats tests/
  go build -o /tmp/p ./cmd/pactify && /tmp/p --help | grep -c account   # 期望 0
  ```

**PR2 · web 删除托管模式**（A7、A8、C3 的第一步、C5）
- 删除 A7 列出的整文件；按 A7 的行号改 App.tsx、NoProjects.tsx、SettingsModal.tsx、datasource.tsx、types.ts、DispatchPanel.tsx、RunRail.tsx；删 `vite.config.ts` 的 alias 和 crypto-relay 分块、`tsconfig.app.json` 的 `paths`、`web/vercel.json`、`setupTests.ts:35-45`；重建 dist。
- 验证：
  ```bash
  cd web && npx tsc -b --noEmit && npx vitest run && npm run build && npx playwright test
  git grep -n -E '@pactify-apps|\.\./cloud/|isHostedMode|RelaySource|multiMachine' -- web/src web/*.ts web/*.json   # 期望没有结果
  git diff --exit-code -- internal/serve/dist   # 提交 dist 之后
  node web/scripts/shots.mjs settings   # 视觉门：Settings 去掉 Account/Machines 之后的截图
  go build ./...
  ```

**PR3 · 删 `cloud/`、CI 和发布流程**（A1、A9、A5 的 fixture）
- `git rm -r cloud/ .github/workflows/publish-cloud.yml`；删 `ci.yml` 的 `cloud` job，以及 test、e2e 两个 job 里的 pnpm cache 和 cloud install 步骤；把 `cloud/pact-project/testdata/{events-golden.jsonl,state-golden.json}` 移到 `internal/serve/testdata/` 并改 `fixture_export_test.go:18-34`（或者经 Owner 同意后直接删这个测试）。
- 验证：
  ```bash
  go test ./internal/serve/ -run TestFixtureExport
  git grep -n -E "cloud/|pnpm" -- .github web '*.go'   # 期望没有结果
  # 推到草稿 PR，确认 CI 的 test 和 e2e 两个 job 全绿（按 memory：单独查返回码，不要串在 watch 后面直接 merge）
  ```

**PR4 · 文档与 entry 文件**（A10、A11）
- 归档四份云端文档（AJ-665 已决定保留，现位于 `docs/archive/release-process.md`、`docs/archive/linx-coordination.md`、`docs/archive/agentworks-wire.md`、`docs/archive/account-sso.md`）；`deployment.md` 只保留安装、开发、官网三段；删掉 `operations.md` 的 relay 段；`architecture.md` 的通讯架构和 M3.4 两段改成“已删除，见 CHANGELOG”；README 去掉托管说明；三份 entry 文件同步改 `:19,24,29,32,44,49`；`harness-subagents.md §8` 标注完成；CHANGELOG 加一节。
- 验证：
  ```bash
  git grep -n -E 'deployment\.md|release-process\.md|linx-coordination|agentworks-wire|account-sso' -- ':!docs/CHANGELOG.md' ':!docs/archive' ':!.pact'   # 期望没有失效链接
  diff <(sed -n '1,60p' CLAUDE.md) <(sed -n '1,60p' AGENTS.md)   # 只允许标题行不同
  bats tests/spec_doc.bats tests/f3_entry_files.bats
  ```

**PR5 · Go 侧删 gemini-cli**（B1 到 B13 中的 Go 部分，以及 C2）
- 按 B 表编辑 12 个 Go 文件，并修 simB 中红的 26 个测试（外加删除 `TestGeminiEnv`）：kind 列表类断言直接删 gemini-cli；`entrymark_test` 里的 3 个改写成只验证 antigravity；`TestSEC_M11` 改断言。用新二进制跑 `pactify agent docs` 重新生成 `docs/agent-onboarding.md`。
- 验证：
  ```bash
  go build ./... && go vet ./...
  git grep -n -E '"gemini-cli"|"gemini"|geminiEnv|geminiKey|IndexDelete|--acp' -- '*.go'   # 期望没有结果，或只剩注释
  git grep -n -i gemini -- '*.go' | grep -v -i -E 'antigravity|agy|\.gemini/antigravity|gemini-3\.|GEMINI\.md|GEMINI_API_KEY|GOOGLE_'   # 人工复核剩下的
  # 在 worktree 里：
  go test ./... && go test -race ./internal/{pact,orchestrate,serve}/... && bats tests/
  go run ./cmd/pactify agent docs && git diff --exit-code docs/agent-onboarding.md
  ```

**PR6 · web 侧与仓库清理**（B14 到 B16、C1）
- 改 CockpitPanel、agentLogos、icons、ElementsGallery、AgentsPage、AddProjectWizard（`antigravity → GEMINI.md`）和相关测试 fixture；重建 dist；删除 `.gemini/settings.json`（前提是 Q5 确认 agy 不需要）；GEMINI.md 正文和标题改掉，`pact:kinds` 行按 Q7 的决定处理；更新文档。
- 验证：同 PR2 的 web 门，再加 `go build ./...` 和 `bats tests/f3_entry_files.bats tests/agent_add.bats`。

---

## 5. 需要 Owner 确认的开放问题

1. **`cloud/` 的去向（最关键）**：linx 通过 npm `^0.1.0` 依赖 `@pactify-apps/relay`、`wire`、`crypto`（证据见 §1.0），linx 自己也写着 “relay lives in pactify repo”。删掉之后 linx 没有上游源码。选项：(a) 先用 `git subtree split -P cloud` 把带历史的子树移交给 linx 仓库，再删；(b) 只打 tag 归档，linx 冻结在 0.1.0；(c) 同时对 npm 包做 deprecate。这一步还决定 `publish-cloud.yml` 和 `NPM_TOKEN` secret 怎么处理。
2. **仓库外资源**（不在本仓库里，删代码不会自动处理）：
   - fly app `pactify-relay`、`pactify-relay-staging`（0 machines，但 app 和 secrets `DATABASE_URL`、`RELAY_SECRET` 还在）。
   - Vercel 项目 `pactify`：`orx.pactify.dev`、`orx.agentjoey.ai` 仍在提供指向已关闭 relay 的托管页面（`operations.md:9-10`）。
   - GitHub OAuth app 和对应 secrets（SSO）。
   - 远端分支 `origin/staging`、`origin/production`（停在 2026-07-04）。
   - **与 linx 共用的 Neon 项目 `pactify-cloud`**：pactify 独有的表是 `Project`、`PactEvent`（migration 9、10）以及 `User`、`Identity`、`WebSession`、`AccountMember`、`Subscription`、`MagicLink`、`LinkChallenge`（migration 11）。**migration 11 还 ALTER 了 linx 的 `Account` 表**，所以是否删表或回滚，需要和 linx 一起决定。代码删除本身不碰数据库。
3. **本机 serve**：`~/Library/LaunchAgents/com.pactify.serve.plist` 带着 `--relay-url ... --remote-control`。PR1 合并、二进制更新后 serve 会起不来。是先改 plist，还是 PR1 保留一个版本的隐藏 no-op flag？
4. **治理模式是否用到这些将被删除的能力**：多机编排 `--seat-host`、远程控制、`.pact/remote.json`、`provision`（远程 clone 并注册）。代码里没有找到本地路径对它们的依赖（§1 A4、A5），但需要 Owner 确认没有在用的流程。
5. **`.gemini/settings.json`**：agy 是否读取项目级 `.gemini/settings.json` 里的 MCP 配置，没有证据，删除前建议实测一次。
6. **gemini-cli 的兼容处理**：已有 roster 里有 gemini-cli（`~/.pactify/agents.json`；本仓库的 `gemini-worker` 座席）。是直接让 `--seat-kind x=gemini-cli` 报 `unknown agent kind`，还是加一个友好提示（“已移除，请改用 antigravity/agy”）？`gemini-worker` 只是历史座席 id，建议保留。
7. **GEMINI.md 托管块**：`pact:kinds: gemini-cli` 要改成 `antigravity`。用 `pactify agent add antigravity` 重新 bake 会顺带写全局 `~/.gemini/config/mcp_config.json`；手改则违反 CLAUDE.md:58 “托管块不要动”。选哪一种？
8. **web 和 serve 的去留**：spec §8 说看板冻结、去留在 M3 单独决定。本盘点只删其中托管专用的部分。如果 Owner 打算整体删除 web 和 serve，PR2 和 PR6 可以跳过、直接合并进那个 PR，以免白做一遍。
9. **“Release 后必做”里与云端相关的内容**：五条步骤本身不提云端，但第 3 条要求三份 entry 文件同步，其中 Tech Stack 的 Cloud 行、Dev Commands 的 `cd cloud && pnpm -r build`、Technical docs 里的 Deployment 链接、“37 包”这个数字，都要在 PR4 里一起改。CLAUDE.md:19 的“历史约束：先部署 relay 再放二进制”也应删除。是否要在 CHANGELOG 里单独发一个版本（例如 v0.13.0“移除云端与 gemini-cli”）？
10. **文档处理方式**：`docs/archive/agentworks-wire.md`、`docs/archive/account-sso.md`、`docs/archive/linx-coordination.md` 是移到 `docs/archive/` 保留，还是直接删除（git 历史里仍可找回）？
