# opencode 大任务评估（与 Phase 0 同等规模）

> 方法照搬 [`phase0-report.md`](phase0-report.md)，结果可以和 codex、kimi 以及 [agy](agy-eval.md) 直接对比。v2 评估在前，v1 评估作为历史保留在后。

## v2 评估（Harness v2，2026-10-17）

> 基线 `88df743` · opencode 1.19.0，经 `opencode serve` 接入（AJ-710）· 模型 `minimax-coding-plan/MiniMax-M3`（Owner 指定）· pactify main `42ae3e0` · host 为 Claude Code headless（haiku），经插件 `/pactify:opencode` 原样转交任务说明 · 3 个任务并行 · 验收命令事后串行重跑 · 两名独立评审逐题读 diff，并在各自的副本里跑全量测试。

### 结论

| 指标 | codex（Phase 0） | kimi（Phase 0） | agy v2 | opencode v1（deepseek-flash） | **opencode v2（MiniMax-M3）** |
|---|---|---|---|---|---|
| 一次完成（10 个大任务） | 9/10 | 6/10 | 3/10 | 6/10 | **4/10** |
| 小问题 | 0 | 2 | 5 | 4 | **4** |
| 不完整 / 失败 | 1 | 2 | 2 | 0 | **2 不完整**（T05、T08） |
| job 成功 / 验收通过 | — | — | 9/10 · 10/10 | 10/10 · 10/10 | **10/10 · 10/10** |
| 全量测试新增失败 | — | — | T07 新增 6 个 | — | **0** |
| 只读收敛（3 个） | 3/3 | 0/3 | 3/3（上限 120）/ 2/3（上限 40） | 0/3（只读档关闭） | **1/3**（R1、R3 在第 41 次调用时被上限截断） |
| 模型兑现 | 26/26 | — | 13/13 | 13/13 | **13/13** |

- **未达转正门槛（≥7/10）**，opencode 保持实验性。适配层表现稳定：13 个 job 没有出现后端出错、空闲超时或中断，模型全部兑现，只读三题都没有改动文件。
- **差距在模型侧。** MiniMax-M3 能可靠地修好被点名的位置，但要求「全仓库搜索」时会漏掉不明显的调用点（T05 漏了 `internal/agent/agent.go:121`）；出错时会悄悄回退到旧行为（T02）；测试有时只是复述修复，没有复现原问题（T03、T05）；报告会夸大测试情况（T02 自称三个测试在修复前都失败，实际有两个在基线上就能通过）。另有 3 题未跑 gofmt。
- **与 v1 的对比要看口径。** 本轮评审对漏掉调用点判得更严：v1 的 T05 有同样的遗漏，当时判为 minor，本轮判为 incomplete，与 agy v2 的判法一致。按 v1 口径，本轮是 4 complete、5 minor、1 incomplete（T08）。
- **只读上限是 opencode 的主要障碍。** R2 用 23 次调用给出了结论；R1、R3 都在第 41 次调用时被截断，没有交回结论。agy v2 也是 R1 超限，说明默认的 40 次对逐个文件读取的 harness 偏紧。

### 逐题结果

| 题 | 判定 | 工具调用 | 要点 |
|---|---|---|---|
| T01 | complete | 40 | 两处 stdin 继承都去掉了；回归测试在基线上会挂到 go test 超时才失败，而不是在 3 秒内失败 |
| T02 | minor | 192 | 门禁改在 feature 分支的临时 worktree 里运行；建 worktree 失败时会悄悄退回基线目录；`recover.go` 的改动没有测试；报告夸大了测试情况 |
| T03 | minor | 85 | 写入端统一为 RFC3339 UTC，读取端兼容旧格式；1 个测试文件未跑 gofmt；契约测试没有走生产路径 |
| T04 | complete | 181 | 13 个需要隔离的测试包都调用了 `Isolate`（评审用 `go list -deps -test` 逐包核对过） |
| T05 | incomplete | 74 | 漏掉 `internal/agent/agent.go:121` 写死的 kimi MCP 路径；`authBase` 忽略了传入的 home；一个子测试在基线上也能通过 |
| T06 | complete | 29 | 没有改动，判断正确：基线已含该修复（`540f577`），报告如实说明了这一点 |
| T07 | minor | 128 | 评审人校验正确，并保留了保留评审人 `human`，同步改了 planner、serve、orchestrate 的测试夹具；planner 的提示词和 `Plan.Validate` 没有跟着改；3 个测试文件未跑 gofmt |
| T08 | incomplete | 65 | `--follow` 会把日志打印两遍；`--follow --tail 2` 什么也不打印；`status <未知 id>` 的退出码仍是 1；测试都没覆盖这些情况 |
| T09 | complete | 61 | 跨 cwd 找同一 harness 的上一个 job，并跳过损坏的 job；测试覆盖了 4 种情况 |
| T10 | minor | 34 | 两处修复都正确；但容错放进了共用的 `readKimiLines`，导致一个已有测试变弱 |
| R1 | 未收敛 | 41 | 被只读上限截断 |
| R2 | minor | 23 | 引用全部准确；11 个发现里 5 个成立、3 个部分成立、3 个不成立 |
| R3 | 未收敛 | 41 | 被只读上限截断 |

成本：MiniMax 用的是包月 coding plan，不按 token 计费。大任务中位数约 8.7 分钟，输入 447 万 token（含缓存），输出 3.3 万。

## v1 评估（AJ-653，2026-10-16，历史）

> M2 里程碑（Linear P-AJ-18 · AJ-653）。方法照搬 [`phase0-report.md`](phase0-report.md)，结果可以和 codex、kimi 的首轮以及 [agy](agy-eval.md) 直接对比。
>
> 实测时间：2026-10-16。环境：
> - 基线 `88df743`，与 Phase 0 相同；
> - opencode 1.19.0，模型 `deepseek/deepseek-flash`（按 Owner 指示，DeepSeek 只用 flash）；
> - pactify main `ac6ee16`，含 #104、#109、#110、#111；
> - host：Claude Code headless，模型 haiku，经插件 `/pactify:opencode` 委派。

### 结论

| 指标 | codex（Phase 0） | kimi（Phase 0） | agy | **opencode** |
|---|---|---|---|---|
| 一次完成（10 个大任务） | 9/10 | 6/10 | 1/10 | **6/10** |
| 小问题 | 0 | 2 | 5 | **4** |
| 失败 / 需返工 | 1 | 2 | 4 | **0** |
| 只读任务收敛（3 个） | — | 0/3 | 0/3 | **0/3**（模型去调 shell 被拒，本轮随即结束，40 秒左右无结论） |
| 模型兑现 | 26/26 | — | 13/13 | **13/13**（export 里每条 assistant 消息的 provider/model 与请求一致） |
| 会话留痕 | 无 | 无 | 无 | **无**（隔离的 XDG 目录） |

opencode 加 `deepseek-flash` 的特点：**稳，便宜，但还没到达标线。**
- **稳**：10 个大任务全部跑完，没有一次后端出错、空闲超时或被打断；10 题的验收命令全部通过；没有一题需要返工。
- **便宜**：10 个大任务一共 **0.28 美元**；单题耗时中位数约 9.8 分钟。
- **未达标**：一次完成 6/10，低于 Phase 0 的门槛（≥7），和 kimi 持平。4 个 minor 都是细节，没有方向性错误。

**Owner 已拍板（2026-10-16）：保持实验性，关闭只读档。**

> 只读档关闭的决定已被 AJ-710 取代：v2 经 `opencode serve` 接入，只读用 plan agent 加有序的权限规则，结果见上文「v2 评估」。

只读任务 0/3：模型去调 shell，被拒后这一轮就结束了，没有结论。
按 kimi K1 和 agy 的先例处理：`sub run opencode --read-only` 以退出码 2 拒绝执行，并指向 `codex --read-only`；旧只读 job 的按 id、自动选择和 worktree 续接同样拒绝，doctor 的只读探测为 SKIP。

### 逐题结果

| 题 | 判定 | 要点 |
|---|---|---|
| T01 | **complete** | 两个 spawn 点都把 stdin 置空；回归测试在回退修复后会失败，`-race` 下连跑 15 次稳定 |
| T02 | **complete** | 门禁改在 feature 代码上跑（主检出已在该分支时原地跑，否则用临时 worktree，用完删除）；两个回归测试在回退后都会失败。小瑕疵：默认门禁类型仍从基线目录识别 |
| T03 | **complete** | 写入端统一为 RFC3339 UTC，读取端兼容旧格式；回退写入端或读取端都会有测试失败 |
| T04 | **complete** | 测试隔离 `PACTIFY_HOME`，覆盖了所有需要隔离的测试包（评审逐包核对过依赖） |
| T05 | minor | kimi 目录迁移的主体正确，但 `agent.go` 里 kimi 的 MCP 配置路径仍然写死为 `~/.kimi-code`，运行时会写错目录；doctor 的 kimi 探测忽略了注入的 home 参数；还附带了一处任务范围外的 `testenv` 改动 |
| T06 | **complete** | 基线已有升级处理，实际只需在原因文字里写上 feature 名；新测试正好覆盖这一点 |
| T07 | **complete** | Phase 0 两家都做错的题。保留了保留评审名 `human` 的豁免；只修正了在新规则下本来就无效的测试数据（包括 codex 当时漏掉的 `serve/plan_apply_test`），全量测试通过 |
| T08 | minor | `--follow` 先读日志、后读状态，中间追加的最后几行可能丢失（窗口很窄）；任何状态读取错误都报成「未知 job」；`status <id> --json` 的输出结构变了 |
| T09 | minor | 版本变化提示排在 `Warnings[0]`，会盖住真实的失败原因（AJ-670 修过的同类问题）；有一个重复的辅助函数 |
| T10 | minor | 两处修复正确，但「容忍末行截断」同时作用到了会话索引文件，导致已有的「坏索引」测试碰巧通过；改成了整文件读入，不再流式读取 |

判定口径与 Phase 0 相同，分两步：
1. 先跑任务自带的验收命令。评估全部跑完后统一串行执行，避免负载干扰。结果 10/10 通过。
2. 再由两个独立评审子代理对照任务说明逐题读 diff；能回退修复的，回退后确认回归测试会失败。

只有判为 complete 的才记作一次完成。评审结论里最重的两条（T08 的读写顺序、T09 的 `Warnings[0]`）由我读码核实属实。

### 观察到的行为

- **可靠性**：13 个 job 没有一次后端错误、超时或中断。这和 agy 形成鲜明对比：agy 10 题里有 3 题被后端打断。
- **成本与用量**：
  - 10 个大任务合计 0.28 美元，单题 0.01–0.06 美元；
  - 单题输入中位数约 193 万 token（含缓存命中，缓存命中占绝大部分），输出约 2 万。
  - token 数用 export 重新加总得出：评估当时 pactify 只记录了最后一步的 token，这个缺陷由本次评估发现，已在 AJ-680（#113）修复。
- **只读档**：3 题全部是同一模式：模型先去调 shell，被拒后这一轮就结束了，没有改用读文件的工具。这和 agy 一样，属于委派方式下「拒绝即结束」的问题；交互使用时不会这样。
- **minor 的共性**：都是对任务说明之外的隐含约束考虑不足，比如运行时同一个路径还有第二处使用、读写顺序、警告的排列顺序。实现方向都是对的。

### 关于模型选择

本次结论基于 `deepseek/deepseek-flash`。之前用 `deepseek/deepseek-v4-pro` 跑过两轮，都不计入结论：
- **第一轮**（pactify `86106a5`）：因账户余额不足中断，13 题只完成了 1 题；
- **第二轮**（pactify `ac6ee16`）：13 题都跑完了，但 Owner 随后要求 DeepSeek 只用 flash，于是只做了 T01、T02 的验收（都通过），没有做独立评审。日志另行存档，仅作参考。

### 评估中发现的 pactify 缺陷

- **AJ-674**（#109）：失败的 job 记 0 token。在第一轮因余额不足失败时发现。
- **#104**：`opencode export` 经管道读取时会被截断，改为写入文件后再读。
- **AJ-680**（#113）：opencode 成功的 job 只记了最后一步的 token，少记约 10 倍。
- **已登记到 Notion Inbox**：在还没有任何提交的仓库里，`sub run`（包括只读档）会直接失败，报错是 `git stash create`。

## 数据与复现

- 任务说明、运行和验收脚本、逐题日志都在 Phase 0 评估目录里：`briefs/`、`run-oc.sh`、`accept-oc.sh`、`driver-oc3.sh`。
- 每题对应的 job 原在 `oc-home/jobs/<id>` 下（worktree、`log.jsonl`、`result.json`），已于 2026-10-17 清理磁盘时删除；逐题日志（`logs/`）和评审结论仍保留。
- v2 评估：`run-oc4.sh`、`driver-oc4.sh`、`accept-oc4.sh`，job 在 `oc4-home/jobs`，评审材料在 `review-oc4/`。
