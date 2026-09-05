# Spec — Single Canonical Ledger(单一规范账本)

> Status: **DRAFT** · 2026-09-04 起草，2026-09-05 更新（§5.1 已拍板 · §5.2 有实测结论待拍板 · WS-A.1 已落地）
> 承接 `docs/backlog.md` 的「🔴 架构级债务」三条（共同根因）与 `[UI-GATE]`。
> 本文只到「方案与分期」为止，**不含实现**。§5 的 1、2 已解决（2 是带实测依据的建议，待一句确认），
> 3、4 要等 WS-B/C 开工才有意义。
> 所有现状描述均为源码实读，标注 `文件:行号`（起草时 HEAD = `feat/fallback-par`，现已并入 main）。

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
canonical = 一个**不被 checkout 的孤儿分支** `pact-ledger`（§5.2 实测后由 `refs/pact/*` 改为
`refs/heads/*`：只有后者能被朴素 `git clone` 自动带走，而 §5.1 拍板工作树文件不再 tracked 之后，
账本在 git 里的存在全部押在这个 ref 上）里的 blob；
工作树的 `.pact/log.jsonl` **降级为导出产物**——地位与今天的 `STATE.yml` 完全一致
（`STATE.yml` 早就是投影，`rules.go:416-425` 要求它逐字节等于 `Render(log)`）。

为什么 C 同时满足两边：

- 分支切换、worktree 创建、merge **不再碰 canonical**——ref 不在任何分支的树里（解耦）。
- 仍然在 git 里：**朴素 `git clone` 自动带走**（§5.2 实测），可 `git cat-file` 逐版本 diff，
  历史完整（守住 I-2）。
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
| ↳ A.1 | ✅ **已做**（2026-09-05，PR #54）：`internal/ledger` 建包 + `serve` 全量迁移 + 约束测试 | 零（外加一处 bug 修复） | serve/ledger/pact/orchestrate 四包绿 |
| ↳ A.2 | 待做：`internal/pact` 引擎自身改走 `ledger`（进程级变体），`orchestrate/remote.go` 的 git pathspec 用共享常量 | 零 | — |
| **B** | ref 存储 + CAS 实现，**双写双读校验**（flag 关闭时走旧路径） | 零（默认关） | 双读比对测试 + `-race` |
| **C** | 切换 canonical 到 ref，工作树文件降级为导出 | 有 | 迁移测试 + 全门 |
| **D** | 删除 §3.4 的变通代码路径 | 有（都是删） | `ignorePact` 拿掉后 sandbox 测试仍绿 |
| **E** | 迁移工具（`pactify migrate-ledger`）+ `validate` 扩展 + 文档 + 三份 entry 文件同步 | — | bats + 真仓库演练 |

**WS-A 现在就能做**，且无论 §5 怎么拍板都不会白做——它本身就是在还「8 处各自读文件」的债。

**A.1 落地后的一条实证（值得记住，因为它把「收敛入口」从洁癖变成了修 bug）**：
迁移 `serve` 时发现它内部对「项目 X 的账本在哪」有**两种答案**——`dto.go`/`stats.go` 拼
`<projectRoot>/.pact/log.jsonl`（项目级，对），而 `cockpit.go:125` 的 `seatKind` 兜底走
`paths.LogIn(p.Path)`（进程级）。后者在 `PACT_DIR` 是绝对路径时会**忽略传入的 base**，
于是 serve 这个多项目进程会**静默改读 PACT_DIR 指向的那个仓库**，折出别的项目的座席 kind，
进而拉起错的 agent。它从来测不出来，是因为 `testenv.Isolate()` 为整个测试二进制清掉了
`PACT_DIR`——**没有任何测试处在能触发它的环境里**。

⇒ 教训写进包注释并用约束测试钉住：「进程级」与「项目级」是两种都正当的语义
（前者承载 runner 把 worker 钉到 driver worktree 的机制），混用不是风格问题而是 bug 来源。
后续 WS 每碰一个包，都应先问它属于哪一种。

## 5. 决策状态（1、2 已解决；3、4 要等 WS-B/C 真正开工才有意义）

1. ~~**工作树的 `.pact/log.jsonl` 还要不要被 git tracked？**~~
   **✅ 已拍板（Human Owner，2026-09-04）：不 tracked。**
   ⇒ 账本的 git 可见性**全部**由那个 ref 承担（`git log pact-ledger` 看历史），
   工作树文件退化为纯本地导出产物，代码 diff 里不再出现账本噪音。
   连锁后果见 §3.3，其中 **`parallel.go` 的改造是这条决定唯一的成本**，必须与 WS-C 同批做。
   ⚠️ 这也意味着 **I-2 完全押在那个 ref 上**：它若不能跟着 clone/push 走，账本就既不进 diff、
   也不跨机——那才是真的把差异化丢掉。**这正是 §5.2 从「可以晚做」升级为前置的原因，
   而 §5.2 的实测结论（孤儿分支）恰好把这个风险消掉了。**
2. ~~**ref 的 push/fetch 默认行为**：是否自动为用户仓库配置 refspec？~~
   **✅ 已有实测答案与建议（2026-09-05）：改用孤儿分支 `pact-ledger`，不用 `refs/pact/*`，
   因而这个问题消失——不需要动用户的 `.git/config`。**
   ⚠️ 这是**带实测依据的建议，尚未被 Human Owner 明确批准**；WS-B 开工前需要一句确认。

   **全部结论来自真 git 实验**（临时 bare remote + 多个 clone，非查文档）：

   | | 自定义 ref `refs/pact/ledger` | 孤儿分支 `refs/heads/pact-ledger` |
   |---|---|---|
   | `git push`（默认）带上 | ✗ | ✗（两者都要显式推，而这一步由 pactify 自己做） |
   | `git push --all` 带上 | ✗ | ✓ |
   | **全新 `git clone` 自动获得** | **✗** | **✓** |
   | 需要改用户 `.git/config` | ✓ **每个克隆都要**加 `remote.origin.fetch` | ✗ |
   | 日常噪音 | 无 | `git branch -a`/`-r` 与 GitHub 分支下拉多一条 |
   | 工作树污染 | 无 | 无（不 checkout，实测 clone 后工作树只有原文件） |
   | 并发写 | 非快进拒绝 | 非快进拒绝 |

   **决定性的一条**：§5.1 已拍板工作树文件**不再 tracked**，于是账本在 git 里的存在**全部**押在这个
   ref 上。若它需要每个克隆手工配 refspec，那么一次朴素的 `git clone` 得到的是一个**完全没有账本**
   的仓库——比今天更差，且对没跑过 pactify 初始化步骤的人直接破坏 I-2/I-3。孤儿分支零配置就能跨机
   传播（实测：全新 clone 立刻可 `git cat-file -p origin/pact-ledger:log.jsonl` 读到账本）。

   代价是 `git branch -a` 与 GitHub 分支列表多一条。这是真代价，但它同时是**可发现性**——
   「账本确实在 git 里」这件事肉眼可见，与 I-2 的意图一致。

   **并发写的处理（已验证收敛）**：两台机器基于同一 base 各追加一条事件后，
   先推的成功、后推的被 git 以非快进拒绝；后者 `fetch` → 按 `event_id` union 两侧 → 重推即成功，
   最终双方都看到全部事件。**这正是 `mergeEventLines`（`sandbox.go:433-456`）已有的语义**，
   WS-C 可直接复用到 ref 层，不需要新算法。

   **推送时机的建议**：不要每个 verb 都推（每次 checkpoint 都联网不可接受）。挂在既有的
   push 时点上（merge / 显式 `pactify sync`），并保留「本地可用、联网可选」这个当前性质。
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
- 一个**从 tracked-`.pact` 迁移过来的**仓库，在并发 run 期间切分支 / 建 worktree / merge，
  账本零冲突、零丢失（迁移前的 624 个事件一条不少）。
- `mirrorLedger` 不再需要区分仓库是否 tracked——那条分支判断随之删除（实时 board 恢复）。
- 一次朴素 `git clone` 后，不做任何配置就能读到账本（§5.2 的零配置承诺，用真 remote 验）。
- 漂移检测：构造「代码已在 base 但任务仍 assigned」，`pactify validate` 能报出来。
- 全门：`go test ./...` + `-race`（pact/orchestrate/serve）· bats · vitest · e2e。

---

*事实来源：2026-09-04 对 `internal/pact`、`internal/orchestrate`、`internal/projection`、`internal/gitx`、
`internal/paths`、`internal/serve` 的逐行实读；跨 worktree 锁的结论为 `git rev-parse --git-path` 实测；
§5.2 的传播与并发结论为 2026-09-05 用临时 bare remote + 多 clone 的真 git 实验。定稿于 `48bccf4`。*
