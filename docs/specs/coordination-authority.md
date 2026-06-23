# Spec — Coordination Authority (项目寻址 + base 单写入车道)

Status: **draft** · Created 2026-06-23 · Owner: claude (orchestrator)
Source: 两次 dogfood 事故(linx orchestrate 撞 OCRC relay 投递 + MCP 绑死在 OCRC)。

## 1. Problem

pact 自称「多 agent 通过 git + 文件协同」,但真正决定协同的两个资源**都不是显式、可寻址、可加锁的对象**,而是隐含在「进程 cwd」和「裸 git ref」里。两次事故是同一个病根的两个面:

| 事故 | 现象 | 隐式资源 |
|---|---|---|
| **A. MCP 绑死单项目** | OCRC 启动的 MCP session 报 `project: opencode-remote-control`,中途看不到/换不到 linx 的 `.pact/` | 「我在协调哪个项目」= 进程启动时的 cwd(`paths.Dir()` 返回相对 cwd 的 `.pact`),无中途 retarget 工具 |
| **B. 两个写入者撞 main** | linx serial orchestrate 在 relay worker 收尾时,把 `bcf9bf8 chore(pact): ignore .pact/orchestrate/` 直接提交到 `main`,移动了 base → 外部 `--ff-only` merge abort | 「现在谁能写 base」= 谁都能 `git merge`,pact 不仲裁;`working_tree_holder` 只锁工作树且只有 pact-aware 的人尊重 |

pact 现在只协调了两个**次要**资源(工作树 `working_tree_holder` + 任务生命周期 ledger)。**集成点(base 分支)**和**协调目标(项目)**这两个最关键的反而裸奔。

### 病根定位(代码)
- A:`internal/paths/paths.go` `Dir()` = `PACT_DIR` 绝对覆盖,否则相对进程 cwd 的 `.pact`。MCP server(`internal/mcp`)全程走 `paths.Dir()`,进程一启动就钉死,无 retarget。CLI 座席拿到裸 `pactify mcp`(`internal/agent/agent.go` 仅 desktop 类加 `--project`)。
- B:`internal/orchestrate/parallel.go` `ensureRuntimeIgnored` 在每次 serial `run()`(`loop.go:111`)开头,对**当前活动分支**(serial = `main`)`gitx.CommitPaths(...)` 提交 `.gitignore`。这是 bcf9bf8。而 driver 的 feature **merge** 本就落 base,与外部 ff-merge 同样无锁。

## 2. 产品原则(一句话)

> pact 要把**「集成点(base 分支)」**和**「协调目标(项目)」**做成显式、有名、可加锁的资源,交给常驻的 `serve` broker 仲裁——而不是放任它们隐含在进程 cwd 和裸 git ref 里。

之后:任何 session 能寻址任何项目;同一时刻只有一个写入者碰 base;autonomous fleet 活在自己的线上,只通过一个排队、gated 的 handoff 跨进人类分支。

## 3. 两层产品设计

### Layer A — 项目寻址的协调面(解 A)
`serve` 已持有机器级注册表(`~/.pactify/projects.json`,name→path),目前只是只读 dashboard 后端。**升格为协调 broker**,协调目标按**名字**寻址而非 cwd:
- MCP 工具加可选 `project` 入参(经注册表解析),或 session 支持 `use-project <name>`。
- 一个 orchestrator session 同时驱动 linx + OCRC + dogfood;cwd 绑定退化为「默认项目」而非「唯一项目」。

### Layer B — base 作为单写入者集成车道(解 B)
1. **没人裸写 base。所有集成走 `pactify merge`**,它是 base 唯一写入者且被串行化。ledger 的锁从「工作树占用」扩展到「base 集成占用」(`integrating: <seat>` / merge token);并发集成**排队**(merge-queue 语义)而非抢。
2. **merge 遇「base 在脚下动了」要自愈**:fetch → rebase → 重试,而非 abort(relay worker 这次是手动 rebase+ff 救回的,这正是 merge-queue 该自动做的)。
3. **autonomous driver 默认不踩人类 main**:`--sandbox`(park 主树 + 隔离 worktree,仅最终 merge 结果落 base)成为**默认**而非可选 flag;跨进人类 `main` 永远是一次受控、排队、gated 的 handoff。
4. **一次性 setup 写入(`.gitignore` / `.gitattributes` union)归 init/wiring 期**,稳态 driver 永不再往共享分支重发——bcf9bf8 这类提交就不存在。

## 4. 分期

### P0 — 消掉这次的具体撞车(本 spec 落地起点)
- **P0a ✅ DONE(2026-06-23)**:`.pact/orchestrate/` 的 ignore 不再由稳态 `run()` 提交到活动分支。
  - 实现:新 `gitx.GitPath`(经 `rev-parse --git-path` 解析 `.git/info/exclude`,兼容 worktree)+ `ensureRuntimeExcludedLocal`(写 local exclude,零提交)替换原 `ensureRuntimeIgnored`;`ensureUnionAttrs` 只提交 `.gitattributes` union(仍需 tracked),runtime ignore 走 local exclude。
  - 测试:`TestEnsureRuntimeExcludedLocalNeverCommits`(HEAD 不动)+ `TestLoopDoesNotCommitIgnoreToBase`(serial run 后无 `ignore .pact/orchestrate` 提交);既有 `TestLoopScaffoldsRuntimeGitignore`/sandbox/parallel 全绿。
  - 下方原始设计:
  - init/wiring 期把 `.gitignore`(`.pact/orchestrate/`)+ `.gitattributes`(ledger union)写进 scaffold(随 `.pact` 一起被用户提交,共享 hygiene)。
  - 稳态 `run()` 改用 `.git/info/exclude`(per-clone、**永不提交**)兜底 runtime 工件,彻底不碰共享分支。
  - 验收:serial orchestrate 跑完,base 分支**零** `chore(pact): ignore …` 提交;runtime 文件仍不进 `git status`。
- **P0b ✅ DONE(2026-06-23)**:autonomous orchestrate 默认 sandbox(`--in-place` 显式 opt-out)。fleet 默认不与人类共享 main。
  - 实现:`Options.RuntimeDir` + `runtimeDir()`,把 dashboard 可见的 runtime(status.json / streams / escalation)写到主目录而非 worktree;`LaunchContext.StreamDir` 同理给 live 流;`RunSandbox` 设 `RuntimeDir=主目录`。CLI:`--in-place` opt-out,`--sandbox` 降级为 deprecated no-op,dry-run/parallel 仍走原路径;dirty-tree 报错指向 `--in-place`。
  - 测试:`TestRunSandbox_WritesStatusToMainDir`(sandbox run 后主目录有 status.json,dashboard 可见)。
  - 已知前置约束:`RunSandbox` 拒绝 dirty 树。`.pact` ledger 应 gitignored(方案2,linx 已是)——若某项目 track 了 `.pact`,回灌会脏树挡下一次默认 run,需 `--in-place` 或把 `.pact` 加进 `.gitignore`。serve-driven run 现默认 sandbox,脏树会清晰报错(非静默)。

### P1 — 单写入者车道(结构性)
- **base 集成锁 ✅ DONE(2026-06-23)**:每个 `pactify merge` 在 checkout→merge→event→commit 关键段持一把跨进程/跨 worktree 的 advisory flock,并发 merge 排队而非抢 base。
  - 实现:新包 `internal/lockx`(`Acquire(ctx, path)` flock(2),`LOCK_EX|LOCK_NB` 轮询 + ctx 取消);锁文件放共享 git common dir(`gitx.GitPath(dir,"pactify-base.lock")`),所以同仓所有 worktree 争同一把锁;`Project.Merge` 持锁(`baseIntegrationLockTimeout` 3min,var 可测)。
  - 测试:`lockx` 互斥 + ctx 取消两测;`TestMergeBlocksOnHeldBaseLock`(外部持锁时 Merge 被挡,释放后 ship)。
  - 关于「merge 自愈(base-moved → rebase)」:pact 的 Merge 用 `--no-ff`(非外部那种 `--ff-only`),base 前进时本就生成 merge commit、天然容忍;真正缺的是**并发串行化**,即此锁。外部裸 `git merge` 写 base 的人仍不受锁约束——产品上要求他们也走 `pactify merge`(锁才生效)。

### P2 — 打通项目寻址(解 A)✅ DONE(2026-06-23)
- 每个 MCP action 工具加可选 `project` 入参(经 `~/.pactify/projects.json` 注册表解析 → dir-aware `pact.At(path)`),空 = 启动 cwd(向后兼容);新增 `projects` 工具列出可寻址项目。一个 session 启动绑在某仓,也能按名驱动任何注册项目——座席恒为本 session 的 `PACT_AGENT_ID`,寻址只换「哪个项目」不换「我是谁」。
  - 实现:`internal/mcp/tools.go` `projectField` 内嵌 + `resolveProject`;`registry.Load()` 查名;briefing/onboarding 文档同步。
  - 测试:`TestProjectAddressingByName`(launch dir=alpha,`status project=beta` 读到 beta;未知名报错列出已知;`projects` 列出 beta)。
  - 已知约束:解析走 `pact.At(path)`,而 `PACT_DIR`(绝对)会经 `DirIn` 覆盖一切 base——真实 MCP 启动只设 `PACT_AGENT_ID`、不设 `PACT_DIR`,故无碍;但若某启动设了绝对 `PACT_DIR`,按名寻址会被它劫持(后续可让命名路径绕过 `PACT_DIR`)。

### P3 — base 写入契约(OCRC 半自动工作流)✅ DONE(2026-06-23)
来源:OCRC 的 4 条核心 + 1 可选要求。一句话:**main 只能被 orchestrator 显式 `pactify merge` 写;merge 要先 fetch+ff/rebase、失败即报错、默认不 push;accept 不连带 merge;init/assign/accept 等绝不碰 main。**
- **P3-1 只有 merge 写 base ✅**:非-merge verb 走 `appendAndRender`(只写 ledger 文件、不 commit),本就不碰 base;新护栏——`Checkpoint` 当任务 feature 声明了独立分支却 HEAD 在 base 时**拒绝提交**(work 必须落 feature 分支)。in-place(未声明分支)保留向后兼容。测试 `TestCheckpointRefusesOnBaseWhenFeatureHasBranch` / `TestCheckpointCommitsOnFeatureBranch`。
- **P3-2 accept 不触发 merge ✅**:`Accept` 仅 `appendAndRender`,无 commit/merge;回归测试 `TestAcceptDoesNotMergeLastTask`(最后一个 task accept 后 feature 未 shipped、feat/x 未进 base)。
- **P3-3 merge fetch-aware、不分叉 ✅**:有 `origin` 时,merge 前 `gitx.Fetch(origin,base)` → `FastForwardTo(base, origin/base)`(分叉则报错)→ `RebaseOnto(feature, origin/base)`(不干净则 abort+报错)→ 再合。无 origin 走旧路径。测试 `TestMergeFetchAwareIntegratesAdvancedBase` / `TestMergeFetchAwareRefusesUnrebasable`。
- **P3-4 merge 默认不 push ✅**:引擎 `Merge` 从不 push(全仓只有 `finish`/serve ship 流显式 push);CLI 加 `--push`(默认 false),合后推 `origin <base>`。
- **P3-5 半自动模式文档 ✅**:`docs/operations.md` 新增「半自动模式(不跑 orchestrate)」+「base 写入契约」两节,写明 `assign → join → checkpoint → accept → orchestrator 单独 merge[--push]`。

## 5. P0a 验收标准(TDD 目标)
1. `ensureRuntimeIgnored`(或继任者)在 serial `run()` 路径下**不产生任何 git commit**。
2. 跑完后 `.pact/orchestrate/` 下的 runtime 文件不出现在 `git status`(被 `.git/info/exclude` 或已提交的 `.gitignore` 覆盖)。
3. 既有并行(`ensureUnionAttrs`)路径行为不回归:union attrs 仍生效。
4. 既有测试全绿;新增针对「serial run 不提交 .gitignore」的回归测试。
