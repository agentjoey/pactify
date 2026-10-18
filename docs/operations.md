# Pactify — Operations

> Last updated: **2026-10-18** | Status: 本地运维与可选治理模式；云端已退役。

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

## serve API

AJ-723 后，serve 只挂载 `/api/sub` 和静态资源，保留 job events SSE 与 CSRF 写保护。
旧项目、编排、cockpit、registry、agent、plan、setup 等 HTTP API 已删除；治理操作改用 CLI/MCP。
AJ-735 已删除 scheduler 和 orchestrator。`--seat` 与可重复的 `--project` 为隐藏的空操作
兼容参数，与旧 relay 参数一样在显式传入时提示忽略，现有 launchd plist 可继续启动。
旧编排器的回滚锚点为 tag `v0.12.0`。

本地读取无需额外 header：`curl http://127.0.0.1:17082/api/sub/jobs`。
浏览器写请求仍检查 Origin，反向代理需设置 `PACTIFY_ALLOWED_ORIGINS`。

## 发版

二进制走 goreleaser + `v*` tag（`.github/workflows/release.yml`）；安装与开发环境见
[`deployment.md`](deployment.md)，版本历史见 [`CHANGELOG.md`](CHANGELOG.md)。当前发布版为 v0.12.0；v0.13.0 挂起，M1 功能仅内部自用。

```bash
git tag vX.Y.Z && git push origin vX.Y.Z      # 仅在 Owner 决定发版后，替换为实际版本号
```

**⚠️ 本地重建的二进制要自己签名**：`go build` 产出的 ad-hoc linker 签名不足以让 launchd 拉起，
会被 `OS_REASON_CODESIGNING` 直接 SIGKILL。`install.sh:74-75` 的 `codesign -s - --force` + `xattr -c`
只覆盖 curl 安装的 release 二进制，**不覆盖本地 `go build` + `launchctl kickstart` 这条开发流程**：

```bash
go build -o pactify ./cmd/pactify && codesign --force -s - ./pactify
```

## 可选 pact 协议流程

人或 agent 使用 CLI/MCP 显式派发、提交评审、接受和合并；命令使用对应座席身份：

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
谁来当 worker/reviewer 无所谓——任何能读文件、能跑 `pactify`(或 pact MCP)的 agent 都行。旧的自动编排命令已删除。

## base 写入契约（spec coordination-authority P3）
保证「main 只被 orchestrator 显式 merge 写」:
- **只有 `pactify merge` 写 base**。`init/join/assign/accept/changes` 只追加 ledger、不 commit；`checkpoint` 提交到**你当前的 feature 分支**——若任务 feature 声明了独立分支而你 HEAD 还在 base,checkpoint **直接报错**(先切到 feature 分支)。
- **机器提交不跑人类钩子**:pactify 的所有 git 提交(checkpoint / merge / setup)都用 `--no-verify`,**绕过仓库的 commitlint/lint-staged 等 pre-commit 钩子**。否则钩子拒掉机器提交会让 work 没落盘、ledger 领先 git(phantom ship)。所以仓库无需为机器提交做「让 commitlint 忽略」之类的特例。
- **空 feature 分支拒绝 ship**:`merge` 当分支无 base 之外的提交(没真实活)时报错而非记 shipped。
- **`accept` 永不触发 merge**,即使是 feature 的最后一个 task;merge 永远是独立显式的一步。
- **`merge` fetch-aware、不分叉**:有 `origin` 时,merge 前先 `git fetch origin <base>` → 本地 base ff 到 `origin/<base>`(分叉则报错)→ feature rebase 到其上(不干净则报错并 abort)→ 再合。无远端的纯本地项目走原路径。
- **`merge` 默认不 push**:本地合完由你决定何时 `git push`,或 `pactify merge --push` 一并推。origin/main 何时前进完全在 orchestrator 手里。

## 已删除的编排功能

AJ-735 删除 `orchestrate`、`plan`、`run`、`schedule`、`audit`（含 SARIF）、`sessions`、
`recipe`、`finish`、`role`，以及 `agent config`、`config gate/critic`、`checkpoint --force`。
role profile、fallback 审批和编排 session 清理随之删除。历史实现见 tag `v0.12.0`；
`pactify join --roles` 等 pact 协议角色继续保留。harness 委派和续接使用 `pactify sub`。
