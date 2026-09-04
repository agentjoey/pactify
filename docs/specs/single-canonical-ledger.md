# Spec — Single Canonical Ledger(单一规范账本)

> Status: **DRAFT · 待 Human Owner 决策** · 2026-09-04
> 承接 `docs/backlog.md` 的「🔴 架构级债务」三条（共同根因）与 `[UI-GATE]`。
> 本文只到「方案与分期」为止，**不含实现**；§5 的四个问题不拍板，后面全是空谈。
> 所有现状描述均为源码实读，标注 `文件:行号`（HEAD = `feat/fallback-par`）。

## 0. 问题陈述

账本（`.pact/log.jsonl` append-only 源 + `STATE.yml` 投影）**既是协议真相，又是 git 工作树里的普通文件**。
于是每一次分支切换、worktree 创建、merge，都在跟协议真相打架。今天的代码为此长出了一整套变通：

| 变通 | 位置 | 代价 |
|---|---|---|
| 裸文件拷贝 seed sandbox | `sandbox.go:469-485` `syncPact` | 绕过 pact 引擎与账本锁 |
| 第二条独立写路径 | `sandbox.go:258-294` `writeLedger` + `mergeEventLines:433-456` | 按 `event_id` union，**无 `event_id` 的行永不去重** |
| 保账本 checkout 五步舞 | `sandbox.go:338-377` `checkoutFeatureBranch` | 快照→`RestorePaths`(自注 DESTRUCTIVE)→checkout→回写→提交；回滚失败时报「事件 LOST」(`:360-362`) |
| union merge driver | `parallel.go:284-294` `ensureUnionAttrs` | 往用户 base 分支提交 `.gitattributes` |
| 每个 verb 自动提交账本 | `engine.go:130-136` `commitLedgerIfTracked` | 账本提交与代码提交交织 |
| 两套「是否 tracked」判据 | `PathTracked`(`engine.go:130`) vs `PathIgnored`(`loop.go:209-231`) | 同一问题两个答案 |

已经付出的产品代价（代码/测试**自己承认**的）：

- **tracked-`.pact` 仓库没有实时 board**：`loop.go:205-208` `mirrorLedger` 在这类仓库里整段跳过，
  「writing it there would dirty the parked main tree and block teardown's branch restore」。
- **sandbox 恢复受限**：`sandbox_test.go:157-163` 的 `TestRunSandbox_AfterPartialInPlaceProgress`
  必须靠 `ignorePact(t, dir)` 才能过，注释自称「the separate **single-canonical-ledger limitation**」。
- **曾把用户逼上 `--in-place`**：`checkout_feature_test.go:71-76`
  「git refuses when that branch carries a different ledger snapshot … which is what forced users onto risky `--in-place`」。
- **`--in-place` 又带来 `[UI-GATE]` 那次事故**：`Checkpoint` 的 `CommitAll`（`engine.go:974-978`，
  `git add -A` 全树范围）把别的 run 在途的文件提交进本任务。
  （2026-09-04 已加 run 守卫止血，**根因未动**。）
- **ledger 漂移无检测**：绕过 pact 周期直接并进 base 时，无任何机制发现「任务显示 assigned 但代码已在 base」。

**一句话根因**：账本的「位置」由**当前 checkout 的分支**决定，而协议要求它由**仓库**决定。

## 1. 不变量（重构不得破坏）

- **I-1 协议 v1 冻结**：事件语义、两条不变量（worker 不能自接受 / 全部 accepted 才能 merge）、
  `event_id` 与事件类型集合**一律不动**。本包只改「账本存在哪、谁写它」。
- **I-2 账本仍然在 git 里**：可 diff、可审计、可离线读、随 clone 走。
  这是 Pactify 相对同赛道（见 `[COMPETITOR]`）**唯一站得住的结构性差异**——
  为了解耦而把账本挪出 git，是把这次重构的目的本身丢掉。
- **I-3 `cat` 仍然读得到**：任何能读文件的 agent 都能参与，不需要 daemon、不需要 pactify 二进制。
- **I-4 零事件丢失**：迁移可逆、可校验；任何一步失败都必须能还原到迁移前状态。
- **I-5 零外部依赖**：不引入服务、数据库、后台进程。

## 2. 三个候选与取舍

**A. 挪出工作树**（`.git/pactify/log.jsonl` 或 `~/.pactify/<repo>/`）
彻底解耦，改动最小。**否决**：破坏 I-2——账本不再随 git 走，不能 diff、clone 不带、
审计属性归零。这正是 paseo 的做法（`~/.paseo` 私有快照），也正是我们不做它的理由。

**B. 双写**（canonical 在别处 + 工作树镜像，两边同步）
**否决**：今天所有痛苦的来源就是「账本有两份、靠 union 对齐」。再造一份权威只会把
`mergeEventLines` 的问题换个地方复现。

**C. 专用 git ref（推荐）**
canonical = 一个不属于任何分支的 git ref（下称 `refs/pact/ledger`）里的 blob；
工作树的 `.pact/log.jsonl` **降级为导出产物**——地位与今天的 `STATE.yml` 完全一致
（`STATE.yml` 早就是投影，`rules.go:416-425` 要求它逐字节等于 `Render(log)`）。

为什么 C 同时满足两边：

- 分支切换、worktree 创建、merge **不再碰 canonical**——ref 不在任何分支的树里（解耦）。
- 仍然在 git 里：可 `git push`/`fetch` 该 refspec 跨机同步，可 `git cat-file` 逐版本 diff，
  clone 带 refspec 即完整历史（守住 I-2）。
- 工作树仍有 `.pact/log.jsonl` 供 `cat`（守住 I-3）。
- 并发从「文件锁」升级为 **`git update-ref <ref> <new> <old>` 的 CAS**——
  今天的锁是 `gitx.GitPath(dir,"pactify-log.lock")`（`engine.go:94-109`），
  而 `--git-path` 在 linked worktree 解析到 `.git/worktrees/<name>/`（**实测确认**），
  即**每个 worktree 一把锁**；今天靠「每棵树各自一份账本 + git union 回灌」绕开，
  ref + CAS 才第一次给出跨 worktree 的真原子性。

## 3. 设计（方案 C）

### 3.1 存储与写入
- canonical blob = 全量 `log.jsonl` 内容；ref 每次追加产生一个新 commit（保留账本自身的历史）。
- append 序列：读当前 ref → 校验 `event_id` 未重复 → 写新 blob/commit → `update-ref` 带 old-value CAS。
  CAS 失败 = 有并发写入 → 重读重试（有界），**不再需要 flock**。

### 3.2 读写入口收敛（先决条件）
今天账本被**至少 8 处包外代码直接读文件**：`serve/cockpit.go:125`、`serve/stats.go:30`、
`serve/dto.go:52,68`、`serve/watch.go:152`、`mcp/server.go:43`、`orchestrate/loop.go:353,1535,1550`、
`orchestrate/remote.go:104`（`git show <ref>:.pact/log.jsonl`）。
换存储前必须先把它们收敛到单一 `ledger` 包——**这一步零行为变化、可独立合入**（见 WS-A）。

### 3.3 导出（工作树文件的新地位）
- 每个 verb 之后导出 `.pact/log.jsonl` + `STATE.yml`（与今天 `appendAndRender` 的时机一致）。
- `pactify export-ledger` 供手工重建。
- **工作树文件不再被 git tracked**（Human Owner 2026-09-04 拍板，见 §5.1）。
  两个文件都进 `.git/info/exclude`（沿用今天 `EnsureExcluded` 的做法，不动用户的 `.gitignore`）。

**这条拍板带来的连锁后果（都是好事，但要一起做）**：

1. **`commitLedgerIfTracked`（`engine.go:130-136`）整个消失**——不再有「每个 verb 后自动提交账本」，
   代码提交与协议记账彻底分离。今天那种「一个 checkpoint 里既有代码又有账本」的混合提交不复存在。
2. **`ensureUnionAttrs`（`parallel.go:284-294`）连同它往用户 base 提交的 `.gitattributes` 一起退役**——
   工作树账本不入库，就没有 merge 冲突要靠 union driver 化解。
3. **`checkoutFeatureBranch` 五步舞（`sandbox.go:338-377`）退役**，`gitx.RestorePaths` 的破坏性用法随之消失。
4. **⚠️ `parallel.go` 必须改造**：它今天**结构性依赖 tracked `.pact`**——worktree 的账本是从分支
   checkout 出来的（全仓确认 `syncPact` 只被 sandbox 调用）。账本不入库后，worktree **拿不到账本**，
   必须改成从 ref 读。这是 WS-C 的主要工作量，也是这条决定唯一的成本。
5. **本仓自身要迁移**：pactify 自己的 `.pact/` 现在就是 tracked 的（624 行账本已在 git 里），
   迁移工具必须能把它平滑转成「ref 为准 + 工作树导出」，且**保留既有 624 个事件的完整历史**。

### 3.4 可以删掉的（重构的实际收益）
`syncPact` · `writeLedger`/`mergeEventLines` · `checkoutFeatureBranch` 五步舞与 `gitx.RestorePaths`
的破坏性用法 · `ensureUnionAttrs` 往用户 base 提交 `.gitattributes` · `mirrorLedger` 的 tracked 分支跳过
· 两套 tracked 判据 · `preserveUnmergedLedger` 的补偿路径（`sandbox.go:384-398`）。

### 3.5 顺带解锁
- **实时 board 对 tracked-`.pact` 仓库恢复**（I 上面那条产品代价消失）。
- **ledger 漂移检测**变得可做：canonical 与工作树导出可随时比对，差异即漂移。
- **`[UI-GATE]` 的 UI 补 checkpoint** 不再受「账本在各分支间已分叉」阻挡。

## 4. 分期（每期可独立合入、独立回滚）

| WS | 内容 | 行为变化 | 门 |
|---|---|---|---|
| **A** | 抽出 `internal/ledger`，收敛 §3.2 的全部读写入口 | **零** | 全量测试逐字节等价 |
| **B** | ref 存储 + CAS 实现，**双写双读校验**（flag 关闭时走旧路径） | 零（默认关） | 双读比对测试 + `-race` |
| **C** | 切换 canonical 到 ref，工作树文件降级为导出 | 有 | 迁移测试 + 全门 |
| **D** | 删除 §3.4 的变通代码路径 | 有（都是删） | `ignorePact` 拿掉后 sandbox 测试仍绿 |
| **E** | 迁移工具（`pactify migrate-ledger`）+ `validate` 扩展 + 文档 + 三份 entry 文件同步 | — | bats + 真仓库演练 |

**WS-A 现在就能做**，且无论 §5 怎么拍板都不会白做——它本身就是在还「8 处各自读文件」的债。

## 5. 待 Human Owner 拍板（不定这四条，后面无法开工）

1. ~~**工作树的 `.pact/log.jsonl` 还要不要被 git tracked？**~~
   **✅ 已拍板（Human Owner，2026-09-04）：不 tracked。**
   ⇒ 账本的 git 可见性**全部**由 `refs/pact/ledger` 承担（`git log refs/pact/ledger` 看历史），
   工作树文件退化为纯本地导出产物，代码 diff 里不再出现账本噪音。
   连锁后果见 §3.3，其中 **`parallel.go` 的改造是这条决定唯一的成本**，必须与 WS-C 同批做。
   ⚠️ 这也意味着 **I-2 完全押在 ref 上**：如果 §5.2 的 refspec 不配好，账本就既不进 diff、
   也不跟着 push——那才是真的把差异化丢掉。**§5.2 因此从「可以晚做」升级为「必须与 WS-C 同批」。**
2. **ref 的 push/fetch 默认行为**：是否自动为用户仓库配置 refspec？
   不配 = 跨机同步默认失灵（今天靠分支自带账本，是能工作的）；
   自动配 = pactify 动用户的 `.git/config`。
3. **并行模式的过渡**：`parallel.go` 今天**结构性依赖 tracked `.pact`**——
   worktree 的账本是从分支 checkout 出来的（全仓确认 `syncPact` 只被 sandbox 调用）。
   切 ref 之后 worktree 怎么拿账本，是 WS-C 的主要风险点。
4. **hosted relay 的上行水位**：`serve/relay.go` 的上传水位以 **行号即 seq** 为前提
   （`.pact/relay-uploaded.json`），账本换存储后这个语义是否还成立需要单独验证。
   （relay 整层当前 0 machines，见 `[CLOUD-DOWN]`——可以晚做，但不能忘。）

## 6. 验收（重构完成的判据，全部可执行）

- `sandbox_test.go` 的 `ignorePact(t, dir)` **删掉后测试仍绿**（今天靠它才过）。
- `checkout_feature_test.go` 断言的那个前提（「plain CheckoutOrCreate 必须失败」）**不再成立**，
  该测试连同 `checkoutFeatureBranch` 一起退役。
- 一个 tracked-`.pact` 仓库在**并发 run 期间**切分支 / 建 worktree / merge，账本零冲突、零丢失。
- `mirrorLedger` 对 tracked 与 untracked 仓库走**同一条路径**（实时 board 恢复）。
- 漂移检测：构造「代码已在 base 但任务仍 assigned」，`pactify validate` 能报出来。
- 全门：`go test ./...` + `-race`（pact/orchestrate/serve）· bats · vitest · e2e。

---

*事实来源：2026-09-04 对 `internal/pact`、`internal/orchestrate`、`internal/projection`、`internal/gitx`、
`internal/paths`、`internal/serve` 的逐行实读；跨 worktree 锁的结论为 `git rev-parse --git-path` 实测。*
