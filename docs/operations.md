# Pactify — Operations

> Last updated: **2026-08-29** | Status: **在用**（本文不是占位稿——`relay 配置与运维` 之后的
> 每一节都对应当前代码里真实存在的命令与契约）。
>
> **⚠️ 当前部署实况（2026-08-29 实测）**：**云端 relay 整层没有在运行**——`pactify-relay` 与
> `pactify-relay-staging` 两个 fly app 都是 **0 machines**，`/health` 不可达；`origin/production`
> 停在 2026-07-04（落后 `main` **652** 提交），`origin/staging` 落后 **224**。
> 文档站 pactify.dev 与 dashboard orx.pactify.dev（Vercel）本身正常，但 dashboard 的 hosted 模式
> 打到的是这个不存在的 relay。**当前唯一可用路径是 local 模式**（`pactify serve`，见下）。
>
> **⚠️ 重新拉起 relay 时的硬约束**：`origin/production` 上 `cloud/wire` 的 `AgentKind` zod 枚举
> **没有 `antigravity`**，而 relay 的 `cloud/relay/src/machines.ts:19` 走的是**会抛异常的**
> `MachineInfo.parse`（那条路径没有 catch）。v0.11.0 起的二进制会广播 `antigravity` kind，
> 所以**必须先把 relay 部署到 ≥ `main` 的版本，再让任何 v0.11.0+ 的客户端连上**；
> 顺序反了，一台 agy 机器就能打掉整个账户的机器列表（`GET /v1/machines` 500）。

## 日常开发流程

```bash
git pull
pactify doctor                 # 装机 + 接线 + 各 vendor CLI 预检
pactify status                 # 当前任务图（账本投影）
cat docs/backlog.md            # 开着的条目（gitignored，只在本地）
cat .agent/CURRENT.md          # 本地工作台的交接说明（gitignored）
```

（`.agent/sprints/` 这套 Sprint 文件已于 2026-06-17 随「仓库只留产品文档」一并退役，
不要再去找它们；排期现在走 `docs/backlog.md`。）

## 故障排查

| 症状 | 排查步骤 |
|------|---------|
| STATE.yml 与 log.jsonl 不一致 | `pactify log` 重算，用输出覆盖 STATE.yml |
| task 状态无法转换 | 检查契约规则（worker 不能自标 accepted；只有 reviewer 能 accept，且必须先 `awaiting_review`） |
| Board 上任务卡在 `changes_requested` / `in_progress`，但活其实已经完成 | 账本滞后于现实（agent 交付了没走完协议流转）。**当前只能回命令行**：以 **owner 身份** `pactify checkpoint <task> --evidence …`，再由 reviewer `pactify accept`。⚠️ 先确认没有并发 run 在跑、工作树干净——`Checkpoint` 走 `CommitAll`，会把别人的在途改动一并提交。已记 backlog `[UI-GATE]`。 |
| launchd 拉起的 `serve` 立刻被杀 | `go build` 的 ad-hoc 签名不够，`codesign --force -s - ./pactify` 后重试（见「发版」节） |
| dashboard hosted 模式空白 / 连不上 | 云端 relay 当前未运行（见顶部）。用 local 模式：`pactify serve` |

## relay 配置与运维

`pactify serve` 可将事件 POST 到远端端点，用于云端 relay / audit。不配置时 relay 完全禁用，零开销。

### 启用 relay

```bash
# flag（优先级最高）
pactify serve --relay-url https://relay.example.com/api/events --relay-token "$(security find-generic-password -s pactify-relay -w)"

# 或仅环境变量
export PACT_RELAY_URL=https://relay.example.com/api/events
export PACT_RELAY_TOKEN=$(security find-generic-password -s pactify-relay -w)
pactify serve
```

token 走 **env / Keychain**，不在命令行或配置文件中写入明文 token。

### 监控

- **dropped 计数**：`GET /api/projects` 响应中无直接暴露 relay dropped；运行时指标在 serve 进程日志中。dropped > 0 表示事件因队列满或端点不可达而丢失。
- **健康**：relay 是异步 best-effort，单点失败不阻塞本地工作流。relay 挂掉时，本地 SSE / CLI / dashboard 不受影响。
- **server 日志**：serve 进程 stdout 无 relay 内部日志（静默丢弃），检查远端端点日志确认送达。

### 禁用 relay

清空 URL 即可（默认状态）：

```bash
unset PACT_RELAY_URL PACT_RELAY_TOKEN
pactify serve   # relay 禁用
```

## 发版

二进制走 goreleaser + `v*` tag；详见 [`release-process.md`](release-process.md) 与
[`deployment.md`](deployment.md)，版本历史见 [`CHANGELOG.md`](CHANGELOG.md)。

```bash
git tag v0.11.0 && git push origin v0.11.0      # 触发 .github/workflows/release.yml
```

云端（relay）走分支快进：`main` → `staging` → `production`。**见本文顶部的硬约束**——
拉起 relay 时必须先部署 relay、后放二进制。

**⚠️ 本地重建的二进制要自己签名**：`go build` 产出的 ad-hoc linker 签名不足以让 launchd 拉起，
会被 `OS_REASON_CODESIGNING` 直接 SIGKILL。`install.sh:74-75` 的 `codesign -s - --force` + `xattr -c`
只覆盖 curl 安装的 release 二进制，**不覆盖本地 `go build` + `launchctl kickstart` 这条开发流程**：

```bash
go build -o pactify ./cmd/pactify && codesign --force -s - ./pactify
```

## orchestrate 自主驱动（pactify orchestrate）
写好任务图（assign 各 task + owner/reviewer/deps + 每个 task 规格里加一行机器可读 `verify: <命令>`），然后让 orchestrate 跑到底：

```bash
# 在 repo 根（含 .pact/）运行；为每个参与座席指定 headless runner kind
pactify orchestrate \
  --seat-kind w=opencode \
  --seat-kind orch=claude-code
```
- **座席→kind**：`--seat-kind seat=kind`（可重复）。有 headless runner 的 kind：`opencode`/`claude-code`/`gemini-cli`/`kimi-cli`/`codex-cli`/`antigravity`（antigravity 走 `agy` CLI，2026-08-22 起可被驱动）。GUI/桌面 agent（`*-desktop`、`codex-app`）无法被驱动——那一棒换 CLI 座席或人工。
- **task 规格 `verify:` 字段**：硬测试门与 reviewer 都用它跑验收，例 `verify: go test ./internal/serve/ -run Relay`。缺失则退化为**项目门**（见下）。**只放一行专用 `verify:` 指令，勿写成散文**（首条 `verify:` 行胜出；`>`/`-`/`#` 前缀的不计）。
- **项目硬门（`pactify config gate`）**：task 无 `verify:` 时的回退门按**项目**配置，优先级 task `verify:` > `config gate` > 按项目类型推断的默认。
  - 类型默认：`pnpm-lock.yaml`→`pnpm build && pnpm test`；`package.json`→`npm run build && npm test`；`Cargo.toml`→`cargo build && cargo test`；`go.mod`→`go build ./... && go test ./...`。
  - 显式设（build-first 例）：`pactify config gate "pnpm build && pnpm typecheck && pnpm lint && pnpm format:check && pnpm test"`（写入 ledger 的 `config_gate` 事件，后设覆盖先设）。
- **flags**：`--feature <id>`（只跑某 feature）、`--dry-run`（只打印下一动作不拉 agent）、`--max-rework`(3)/`--max-fails`(2)/`--max-iters`(50)。
- **卡住升级**：返工/失败超阈值或硬门失败 → orchestrate 暂停，写 `.pact/orchestrate/escalation-<ts>.md`（含 task/原因/evidence/建议）并通知。人工修复（改实现/改规格/修协议）后**重跑同一命令即续行**（状态已前进；`--resume` 是文档性同义）。
- **secrets**：runner 不在命令行传 token；agent 自身凭据由其自身配置/Keychain 管。

## 半自动模式（不跑 orchestrate）
全自动不是唯一路径。verbs 本身就支持人/agent 手动走完整条链，orchestrator 只在最后单独合并:

```bash
# orchestrator: 派活
pactify assign T1 --feature F --branch feat/x --owner w --reviewer rev --spec .pact/tasks/T1.md
# worker(座席 w): 上自己的分支干活 → 提交评审
pactify join w --roles worker            # 切到 feat/x
#   ...写代码（必须在 feat/x 上，不在 base 上）...
pactify checkpoint T1 --evidence "tests pass"
# reviewer(座席 rev): 验收（只标记，绝不连带 merge）
pactify accept T1
# orchestrator: 自己决定何时合、何时推
pactify merge F            # 默认只本地合
pactify merge F --push     # 或合并后顺手推 origin
```
谁来当 worker/reviewer 无所谓——任何能读文件、能跑 `pactify`(或 pact MCP)的 agent 都行。orchestrate 只是把这套循环自动化，不是前提。

## base 写入契约（spec coordination-authority P3）
保证「main 只被 orchestrator 显式 merge 写」:
- **只有 `pactify merge` 写 base**。`init/join/assign/accept/changes` 只追加 ledger、不 commit；`checkpoint` 提交到**你当前的 feature 分支**——若任务 feature 声明了独立分支而你 HEAD 还在 base,checkpoint **直接报错**(先切到 feature 分支)。
- **机器提交不跑人类钩子**:pactify 的所有 git 提交(checkpoint / merge / setup)都用 `--no-verify`,**绕过仓库的 commitlint/lint-staged 等 pre-commit 钩子**。否则钩子拒掉机器提交会让 work 没落盘、ledger 领先 git(phantom ship)。所以仓库无需为机器提交做「让 commitlint 忽略」之类的特例。
- **空 feature 分支拒绝 ship**:`merge` 当分支无 base 之外的提交(没真实活)时报错而非记 shipped。
- **`accept` 永不触发 merge**,即使是 feature 的最后一个 task;merge 永远是独立显式的一步。
- **`merge` fetch-aware、不分叉**:有 `origin` 时,merge 前先 `git fetch origin <base>` → 本地 base ff 到 `origin/<base>`(分叉则报错)→ feature rebase 到其上(不干净则报错并 abort)→ 再合。无远端的纯本地项目走原路径。
- **`merge` 默认不 push**:本地合完由你决定何时 `git push`,或 `pactify merge --push` 一并推。origin/main 何时前进完全在 orchestrator 手里。

## Roles and fallback

A **role** is a named (agent, model) profile; a **seat** binds to a role. Roles
live in `~/.pactify/roles.json` (machine-level, `PACTIFY_HOME`-aware) because
they reference locally installed agents — they are advisory guidance for task
assignment, not protocol state.

```bash
pactify role set frontend --kind claude-code --model claude-opus-4-8 --fallback frontend-cheap
pactify role set frontend-cheap --kind kimi-cli
pactify role bind w2 frontend
pactify role list
```

At launch a seat resolves in this order: `--seat-kind` override → an approved
fallback (this run only) → the seat's role binding → the roster kind → a
spawner's `--roster-kind` hint. Two seats of the same kind can therefore run
different models.

`--seat-kind` is an OPERATOR flag and is the only one that outranks a role
binding (it drops the profile's model pin and warns — see `[KIND-2]`).
`--roster-kind` is the machine-facing twin `pactify serve` writes when it starts
a run: the kinds it derives from the ledger (init events → roster → seat-name
heuristic) are configuration, not intent, so they go on their own channel and
lose to everything above. Don't type it by hand; typing `--seat-kind` when you
mean "use the configured kind" is what silently unbinds a role.

**Recommended profiles — antigravity (`agy`).** Given `gemini-3.7-flash`'s
current capability, antigravity suits lightweight work — frontend, test, ops,
docs — not planner/orchestrator or architecture-class tasks (those stay on
stronger roster kinds). This is a role/kind binding, not a tier default: tier
is about task complexity, role is about who's suited for it; the two stay
orthogonal. Nothing below is built in — `roles.Load()` reads exactly one file,
`roles.Path()` (`$PACTIFY_HOME/roles.json` when `PACTIFY_HOME` is set, else
`~/.pactify/roles.json`), and treats a missing file as an empty config, so a
fresh machine behaves exactly as before until you opt in with these commands (or
the equivalent hand-edited JSON):

```bash
pactify role set frontend --kind antigravity --model gemini-3.7-flash-medium
pactify role set test     --kind antigravity --model gemini-3.7-flash-medium
pactify role set ops      --kind antigravity --model gemini-3.7-flash-low
pactify role set docs     --kind antigravity --model gemini-3.7-flash-low
pactify role bind <seat> frontend   # etc. — binding is what actually routes work
```

Equivalent `roles.json` fragment (`$PACTIFY_HOME/roles.json`, default
`~/.pactify/roles.json`):

```json
{
  "profiles": {
    "frontend": {"kind": "antigravity", "model": "gemini-3.7-flash-medium"},
    "test":     {"kind": "antigravity", "model": "gemini-3.7-flash-medium"},
    "ops":      {"kind": "antigravity", "model": "gemini-3.7-flash-low"},
    "docs":     {"kind": "antigravity", "model": "gemini-3.7-flash-low"}
  }
}
```

Do **not** set `--fallback` on these profiles, and leave the profile's `effort`
field unset. (`pactify role set` takes only `--kind`, `--model` and
`--fallback` — there is no `--effort` flag; `Profile.Effort` is reachable only
by hand-editing `roles.json`.) `agy`'s tier is fully encoded in the model
name's `-medium`/`-low` suffix; antigravity's
`RunnerProfile.EffortArgs` is `nil` because `agy --model <tier> --effort
<mismatched-tier>` hard-errors (exit 1, `status:ERROR`, "conflicts with" — see
the antigravity kind registration). Leaving `Profile.Effort` empty means
`agentcfg.ResolveSeat` never has an explicit per-seat effort to inject for
this kind, so it can't reconstruct that conflicting flag pair.

**Fallback.** When a stint fails having produced *nothing* (quota exhausted,
auth expired, a missing binary — "env-class"), the driver proposes the seat's
next fallback role, writes `.pact/orchestrate/fallback/<scope>.json` beside the
escalation, and pauses. `scope` is the feature id (one file per concurrently
driven feature under `--max-concurrency > 1`) or `all` for an unfiltered serial
run. Approve a proposal by naming its **task**:

```bash
pactify orchestrate --resume --approve-fallback <task-id>   # repeatable
```

The escalation record prints the exact command. Naming a task that has no
pending proposal is an error and the run does not start — otherwise you would
believe the agent was swapped while the run burns another budget cycle on the
same configuration. Under `--max-concurrency > 1` the approval applies **only to
the feature whose proposal you named**, even when another feature shares that
seat.

The approval applies to the current run only — tomorrow's quota may have reset.
A failure where the agent *did* deliver work ("logic-class") is not a fallback
case: swapping agents will not fix wrong work, so the escalation instead offers

```bash
pactify orchestrate --resume --reset-task <id>   # discards UNCOMMITTED work only
```

**Known limitation.** Gemini's free (`oauth-personal`) tier silently downgrades
to flash when it hits a quota instead of failing, so pactify cannot detect that
case at all. Use an API-key tier when model determinism matters.
