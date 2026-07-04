# Spec — Driver Modernization(驱动层现代化大包)

> Status: APPROVED-FOR-BUILD · 2026-07-05
> 来源:竞品分析(OpenHands/mco/Hive,Obsidian P027 research)→ backlog C 项 review。
> 本包 = **C-1(ACP transport)+ C-20(session 续接)+ C-6(知识层)+ C-2(doctor 预检)+ C-12(可靠度统计)**。
> 交付方式:**pactify 自驱长跑 dogfood**——本 spec 拆成任务图,由 orchestrate 驱动多 agent 实现,claude 座席任 orchestrator+reviewer。

## 0. 包目标与不变量

**目标**:把 orchestrate 的「驱动层」从 一次性 headless spawn + stdout 解析 升级为 **结构化、可续接、带知识注入、可自检、可度量** 的现代驱动——同时保持:

- **I-1 零破坏**:`CmdRunner`(现有 spawn 路径)保持为默认;ACP 是 **opt-in 增强**(per-kind 自动探测 + 显式开关)。任何 ACP 失败必须干净回退或软失败,绝不比现状差。
- **I-2 协议不动**:pact 两不变量、账本事件语义、门控全不变。本包只动「驱动一棒」的方式与质量。
- **I-3 文件协议优先**:知识层是 `.pact/` 下的纯文件;不引入任何服务依赖。

## 1. Workstream A — ACP transport(C-1,核心)

### A.1 `internal/acp` — Go ACP 客户端(新包)

对 stdio 子进程说 **Agent Client Protocol**(JSON-RPC 2.0 over stdio,Zed 规范)。参考实现:`~/AgentWorks/Code_Opencode/opencode-remote-control/src/core/agent/`(`acp-connect.ts`/`acp-backend.ts`/`acp-normalizer.ts`,TS → Go 移植思路而非逐行翻译)。

```go
package acp

// Client owns one ACP server subprocess + the JSON-RPC connection over its stdio.
type Client struct { /* cmd, stdin enc, stdout dec, pending map[id]chan, notif handlers */ }

func Spawn(ctx context.Context, command string, args, env []string, dir string) (*Client, error)
// JSON-RPC: request/response 关联(id→chan)+ notification 分发(method→handler)。
// 进程退出/stdio 断 → 所有 pending 收错误,Client 进入 dead 态。

func (c *Client) Initialize(ctx context.Context) (InitializeResult, error)   // initialize 握手,协商能力
func (c *Client) NewSession(ctx context.Context, cwd string) (SessionID, error)  // session/new
func (c *Client) LoadSession(ctx context.Context, id SessionID) error            // session/load(续接,能力探测后才调)
func (c *Client) Prompt(ctx context.Context, sid SessionID, text string) (StopReason, error) // session/prompt,阻塞到回合结束
func (c *Client) OnSessionUpdate(fn func(SessionUpdate))       // session/update 通知(agent 消息块/工具调用/plan)
func (c *Client) OnPermissionRequest(fn func(PermissionRequest) PermissionOutcome) // session/request_permission(服务端→客户端请求)
func (c *Client) Close() error
```

要点:
- **类型最小化**:只建本包需要的 ACP 类型(initialize/session.new/load/prompt/update/request_permission/stop reason/usage),字段用 `json.RawMessage` 兜底未知部分——ACP 还在演进,宽进严出。
- **usage 捕获**:`session/update` 与 prompt response 里的 usage(token/cost)字段解析出来,经回调暴露(→ A.3)。
- **测试**:hermetic——测试内用 Go 起一个假 ACP server(读 stdin 写 stdout 的进程或 io.Pipe 内嵌),覆盖:握手、prompt 回合、update 流、permission 往返、进程死亡的 pending 清理、malformed 帧。**不依赖任何真 vendor CLI。**

### A.2 `orchestrate.AcpRunner` — 实现 `Runner` 接口

```go
// AcpRunner runs one stint over ACP. Falls back is NOT its job — the routing
// runner (below) decides ACP vs CmdRunner per kind.
type AcpRunner struct { Idle, Total time.Duration; OnUsage func(seat, task string, u Usage); Escalate PermissionPolicy }
func (r *AcpRunner) Run(ctx context.Context, lc LaunchContext) error
```

生命周期:`Spawn(vendor cmd) → Initialize → NewSession(lc.RepoDir) → Prompt(briefing) → 等 StopReason → Close`。
- **PACT_AGENT_ID=lc.Seat** 注入子进程 env(与 CmdRunner 相同——agent 内部照常用 pact MCP/CLI 做 checkpoint,协议路径零改动)。
- **idle watchdog**:`session/update` 即「有输出」;超过 Idle 无 update → kill,软失败(语义同 CmdRunner 的 IdleTimeout)。
- **permission 策略**(与 OpenHands 的差异化——它无脑 auto-approve):
  `PermissionPolicy` 三档:`auto`(默认,选 allow/approve/yes 选项——无人值守棒);`escalate`(写 escalation 文件+通知,拒当次请求;人审门模式);`deny`。per-project 可在 `.pact/config` 配,默认 `auto`(保持现状行为)。**账本可见**:非 auto 的 permission 事件追加为任务级 note 事件(复用现有 start/note 机制,不新增 event_type)。
- **kind→ACP 映射表**(per-vendor,来自 OpenHands docs + OCRC 实测):

| kind | ACP 启动 | 凭据(订阅登录优先) |
|---|---|---|
| kimi-cli | `kimi acp` (原生,最强目标) | kimi 登录态 |
| claude-code | `npx -y @agentclientprotocol/claude-agent-acp` | Keychain / `~/.claude/.credentials.json`;剥离冲突 `ANTHROPIC_API_KEY` |
| codex-cli | `npx -y @zed-industries/codex-acp` | `~/.codex/auth.json` |
| gemini-cli | `npx -y @google/gemini-cli --acp` | `~/.gemini/oauth_creds.json` |
| opencode | 无(保持 CmdRunner) | — |

- **路由**:`RoutedLocalRunner{ acp *AcpRunner, cmd CmdRunner, mode map[kind]Transport }`。mode 来源:`.pact/config` 的 `transport:` 覆盖 > agentcfg per-agent 覆盖 > 默认表(**默认全部 cmd**,ACP 本期 opt-in;kimi 可作为第一个默认翻 acp 的试点,由 e2e 结果决定)。cmd_orchestrate 加 `--transport kind=acp|cmd`(repeatable)调试开关。
- **测试**:AcpRunner vs 假 ACP server(复用 A.1 harness):完整棒、idle kill、permission 三策略、usage 回调、fallback 路由。

### A.3 usage → TOK 指标(附带还旧债)

`OnUsage` 回调把 per-stint token 写到 orchestrate 现有的 per-task token 捕获处(v0.5.1 建立的机制,当前 CLI 路径无数据源)。落点:orchestrate 驱动侧已有 token 记录文件/事件——ACP 棒有真数据,CmdRunner 棒维持现状。serve `/stats` 自动受益。

## 2. Workstream B — session 续接(C-20)

- **记录**:AcpRunner 在 NewSession 后把 `{seat, task, kind, sessionID, updatedAt}` 写 `.pact/orchestrate/sessions.json`(runtime 状态,gitignored 区域;带 file lock,复用 lockx)。CmdRunner 路径本期不记(CLI resume 靠 vendor 各家 flag,后续增量)。
- **续接**:同一 (seat,task) 重试(rework/软失败重跑)时,AcpRunner 先 `LoadSession(旧 id)`(initialize 能力协商声明支持才调);成功 → prompt 里附「续接上次会话,先自查上次进度再继续」;失败(session 过期/不支持)→ 静默 NewSession,行为=现状。
- **清理**:任务到 accepted/cancelled → 删对应记录(挂进现有 session-cleanup 钩子);`sessions.json` 里的孤儿(任务已终态)由 orchestrate 启动时清扫。
- **测试**:假 ACP server 声明/不声明 loadSession 能力两分支;重试路径断言 LoadSession 先行;终态清理。

## 3. Workstream C — git-native 知识层(C-6)

- **skills(策划)**:`.pact/skills/*.md`,frontmatter:
  ```yaml
  ---
  roles: [worker]          # 或 [reviewer] / 两者;空=全部
  keywords: [frontend, ui] # 命中任务 spec/标题 任一词才注入;空=总是
  ---
  正文(注入的知识)
  ```
- **memory(累积)**:单文件 `.pact/memory.md`(自由 markdown,人和 agent 都可追加;orchestrator 在 merge 后可让 reviewer 提炼教训追加——本期不做自动提炼,先把注入管道铺好)。
- **注入点**:`internal/orchestrate/brief.go` 的 `workerBrief`/`reviewerBrief` 尾部追加「## 项目知识」节:memory 全文(有则)+ 命中的 skills 正文。**预算**:合计截断 4KB(先 memory 后 skills,截断处标注`…(truncated)`),防 briefing 爆量。
- **零依赖**:纯文件读取,文件不存在 = 现状。skills 解析用手写 frontmatter 切分(项目已无 yaml 依赖则 keep it dumb:`---` 定界 + 逐行 `key: [a, b]` 解析)。
- **测试**:role 过滤、keyword 命中、截断、malformed frontmatter 跳过该文件不炸、无文件=原 briefing 字节不变。

## 4. Workstream D — doctor vendor 预检(C-2)

- `internal/doctor` 新增 per-kind 检查(对 `agent.Kinds()` 每个有 headless runner 的 kind):
  1. **binary 在 PATH**(用 RunnerSpec.Command LookPath);
  2. **认证态**(便宜静态检查:per-kind 凭据文件存在/非空——claude `~/.claude/.credentials.json` 或 Keychain 条目、codex `~/.codex/auth.json`、gemini `~/.gemini/oauth_creds.json`、kimi/opencode 各自配置;**不发网络请求**);
  3. **ACP 可用性**(kind 在 A.2 映射表 && 对应命令可解析)→ 报告 `transport: acp available / cmd only`。
- 输出并入现有 `pactify doctor` 的 Check 列表;全绿不打扰,红项给一行可操作修复提示(装/登录命令)。
- serve 启动时跑同一检查,结果打到日志(**不阻断**启动);机器 register 的 agentKinds 本期不改语义(仍按 binary 存在),避免多机行为回归。
- **测试**:fake HOME + fake PATH 的表驱动;无一真 CLI 依赖。

## 5. Workstream E — 可靠度统计(C-12)

- `internal/stats` `AgentStat` 加两字段:`Accepted int`(该 seat 拥有的任务达到 accepted 数)、`Reworked int`(其任务收到 `changes` 事件总次数)。fold 时从既有事件推导,零新事件。
- serve `/stats` DTO 透传;web 座席卡显示 `✓N ↻M`(kimi 前端,一个小改动:RosterDock/Board 座席统计条)。
- **测试**:fold 用例(accept 一次/多次 changes 后 accept);web 快照测试更新。

## 6. Dogfood 任务图(pactify 自驱长跑)

Feature:`driver-modernization`,branch `feat-driver-modernization`。**顺序尊重依赖,独立项并行**。

| task | 内容 | owner(seat/kind) | reviewer | deps | verify 门 |
|---|---|---|---|---|---|
| dm-acp-core | A.1 `internal/acp` 包 + 假 server harness | claude-worker (claude-code) | claude | — | `go test ./internal/acp/` |
| dm-acp-runner | A.2 AcpRunner + 路由 + `--transport` flag | claude-worker | claude | dm-acp-core | `go test ./internal/orchestrate/ -run Acp` + `go build ./...` |
| dm-acp-usage | A.3 usage→TOK 接线 | kimi-worker (kimi-cli) | claude | dm-acp-runner | `go test ./internal/orchestrate/` |
| dm-session-resume | B 全部 | kimi-worker | claude | dm-acp-runner | `go test ./internal/orchestrate/ -run Session` |
| dm-knowledge | C 全部(brief 注入) | kimi-worker | claude | — | `go test ./internal/orchestrate/ -run Brief` |
| dm-doctor | D 全部 | gemini-worker (gemini-cli) | claude | — | `go test ./internal/doctor/` |
| dm-stats | E 的 Go 侧 | gemini-worker | claude | — | `go test ./internal/stats/ ./internal/serve/` |
| dm-stats-web | E 的 web 座席卡 | kimi-worker | claude | dm-stats | `cd web && npx vitest run && npx tsc -b --noEmit` |
| dm-e2e-docs | 集成:kimi 座席真 ACP 冒烟(有 kimi 则跑,无则 skip)+ docs/architecture 更新 + CLAUDE.md Version 行 | claude-worker | claude | dm-acp-runner, dm-session-resume | 全仓 `go test ./...` + bats |

**运行方式**:`--in-place` 顺序驱动(v0.9 学到的教训:并行 sandbox 有账本-worktree 耦合坑);claude 座席 = orchestrator + reviewer;每棒 verify 门如上;全包合并门 = go 全仓 + vitest + tsc + bats 全绿。
**长跑验证点(这次 dogfood 要观察的)**:①跨多任务/多 kind 的连续驱动稳定性;②escalation 质量(卡住时人能看懂);③(本包完成后)ACP 棒 vs cmd 棒的投递可靠性对比——为「kimi 默认翻 acp」决策供数。

## 7. 风险与边界

- **ACP 规范漂移**:各 vendor 的 ACP server 成熟度不一(kimi 原生最稳,claude/codex 经 npx bridge)。对策:宽容解析 + 全部 hermetic 测试 + 默认 cmd。
- **npx bridge 冷启动慢/不在 PATH**:doctor 预检报告;AcpRunner spawn 失败 = 软失败回退语义(路由器本期不自动 fallback 到 cmd——显式优于隐式,失败信息指向 `--transport kind=cmd`)。
- **briefing 膨胀**(知识层):4KB 截断上限,门在注入端。
- **范围外**(明确不做,防蔓延):CLI 路径的 session resume、memory 自动提炼、ACP 权限的 web 实时交互 UI、C-22 Live PTY 流(等本包 ACP 事件流落地后另开)。
