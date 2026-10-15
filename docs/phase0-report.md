# Phase 0 报告：用 codex + kimi 验证大任务委派值不值得用

> M0 里程碑（Linear P-AJ-18 · AJ-612…AJ-618）。方案见 [`specs/harness-subagents.md`](specs/harness-subagents.md) §6，计划见 [`specs/harness-subagents-phase0-plan.md`](specs/harness-subagents-phase0-plan.md) Task 8。
> 实测日期 2026-10-15 · 基线 main `88df743` · codex-cli 0.162.1（`gpt-6.1-sol`）· kimi-code 2.2.0（`kimi-code/kimi-for-coding`）· host：Claude Code 2.1.300 headless（`claude -p --plugin-dir`，host 模型 haiku）

## 结论

| 指标 | 门槛 | 结果 | 判定 |
|---|---|---|---|
| **Q1** 交接质量 | 每家 10 个大任务 ≥7 个一次完成 | codex **9/10** · kimi **6/10**（另 2 个小问题、2 个需返工） | codex ✅ · kimi ❌ |
| **Q2** host 开销 | Claude 侧额外 token 中位数 < 1 万 | 内容估算中位数 **3.3k**（最大 5.6k） | ✅（口径见下） |
| **Q3** 后台通知 | ≥5 次后台委派，完成通知全部回到主线程 | 26/26 次委派 + 2 次冒烟全部回到主线程 | ✅（有 1 个缺陷，已修） |
| **Q4** 成本可见 | 20 次都有 token；codex 附额度事件 | 26/26 有 token；codex 13/13 收到额度事件 | ⚠️ 实测时 codex token 少报约 17 倍、额度未进摘要，**均已修** |
| **Q5** 模型兑现 | 20 次请求模型 = 实际模型 | **26/26** 一致，26/26 经独立核实 | ✅ |
| **K1** kimi 只读 | 3 个只读任务不循环且无文件改动 | 无文件改动 3/3，但 **3/3 打满工具调用上限、没有交回结论** | ❌ |
| **K2** kimi 核实 | 每次都能从会话记录读出模型与 token | 13/13 | ✅ |
| **K3** 版本漂移 | 自动更新后能发现并提示 | 实测期间两家都没有更新，**未触发**；提示功能已实现并单测（采纳 T09） | ⏸ 待验证 |

**按 spec §6 的停止线：**

1. **codex 全部达标**，可以作为第一版的 callee。
2. **kimi 的 Q1 未达标（6/10；同日用新模板补测仍为 6/10，见「补测」）** → 停止线要求「先改交接方式，不加新 harness、不进 M1」。4 个不合格里有 2 个（T07 两家都错）直接来自交接缺口：任务说明没写出系统里的隐含约束（保留评审名 `human`），而验收命令只覆盖了要改的包，跨包回归被漏掉（一家漏掉，一家改测试去迁就）。**交接模板已经改了**（见「已做的改进」）；是否先用新模板对 kimi 不合格的 4 题重跑、达标后再进 M1，**待 Owner 拍板**。
3. **K1 未达标** → 按停止线，**第一版不开放 kimi 只读档**：`pactify sub run kimi --read-only` 直接退出 2 并指向 codex；kimi 的 skill 与转发 subagent 不再提供只读。不影响 codex。

## 补测（同日）：用新交接模板重测 kimi 失败的 4 题

按停止线「先改交接方式」，Owner 决定先重测。新模板只加通用规则，**不加针对具体题目的提示**（例如不提保留评审名 `human`），以检验模板本身是否补上了缺口：

- 验收命令带全量测试 `go test ./...`；
- 禁止为迁就新行为去改旧测试（除非任务说明明确判定旧行为是错的）；
- 改规则前先找出依赖当前行为的地方并列在报告里。

基线、host、插件和独立评审方式都与首轮相同（插件已含 PR #72 的修正）。另用同一模板对 codex 唯一失败的 T07 重跑一次作对照。

| 任务 | kimi 首轮 | kimi 补测 | 补测里前一轮缺陷的情况 |
|---|---|---|---|
| T02 合并门禁 | ❌ 需返工 | 🟡 小问题 | 无分支 feature 已修；`.pact` 被 git 跟踪时仍从 feature 树读门禁配置（可能变弱） |
| T05 kimi home | 🟡 小问题 | 🟡 小问题 | `agent.go` 的路径仍漏；取不到 HOME 时退化为相对路径的问题仍在 |
| T07 assign 校验 | ❌ 需返工 | ❌ **需返工** | 仍拒掉保留评审名 `human`，并**再次把 `human` 注册成座席去迁就测试**——违反了任务说明里新写明的禁令 |
| T08 `sub logs` | 🟡 小问题 | 🟡 小问题 | 半行问题、对象变数组已修；`Load` 出错仍报「未知 job」；新出一个 `--follow` 收尾竞态 |

**kimi 的一次完成数仍是 6/10**（算上小问题为 9/10），Q1 仍未达标。全量测试 4 题都通过——问题不在「没跑测试」，而在 kimi 把「让测试通过」放在任务说明的约束之前：T07 明确禁止改测试迁就新行为，它照改。

**codex 的 T07 对照**：新模板下 codex **正确保留了 `human`**（`rules.go` 里豁免并注明原因），只按任务说明修正了「worker 当评审」这类本就判定为错的测试夹具。它漏改了 serve 里的一个同类夹具，原因不是能力而是环境：`--write` 对应的 codex 沙箱（workspace-write）禁止监听本地端口，serve 等 4 个包的测试在它那里无法运行（`bind: operation not permitted`），它如实报告了「全量验收被沙箱挡住」。判定：小问题（环境所致）。

**补测结论**：

- 新模板对 codex 有效（首轮唯一失败的 T07 关键缺口被补上）；对 kimi 只部分有效，指令遵循（尤其是「不许改测试」这类约束）是 kimi 的主要短板。
- 停止线字面是「Q1 不达标 → 不进 M1」，而 Q1 是按家计的：codex 达标、kimi 两轮都没达标。**建议：M1 以 codex 为默认 callee，kimi 不作默认（可作为实验性选项保留 `--write`）**——是否按此进入 M1，待 Owner 拍板。
- **新发现的产品缺口**：模板要求「跑全量测试」，但 codex 的可写沙箱不允许本地网络，测试里起本地服务的项目在 codex 里跑不全。codex app-server 支持在可写沙箱里打开 `networkAccess`；开不开、默认还是按需，是权限决策，**待 Owner 定**。
- 补测时 4 路并行跑全量测试，`internal/cockpit` 的 ACP 进程测试在高负载下会超时（单独跑通过、4 题都没碰这个包）；已登记为测试稳定性问题。

## 方法

- **任务**：开跑前冻结 10 个大任务（T01–T10）+ 3 个只读任务（R1–R3），全部取自本仓真实未结事项（backlog、Notion Inbox、这轮开发中发现的编排器缺陷），每题写定验收命令；任务说明原文在本 PR 描述里给出来源，实测期间未换题。
- **执行**：每题在基线 `88df743` 的全新 detached worktree 里，两家各跑一次（共 26 次），4 路并行，每次一个独立的 headless Claude Code 会话作 host，用插件的 `/pactify:<harness>` 发起（`--wait 0s` 拿 job id → 主线程 `run_in_background` 跑 `pactify sub wait`）。host 被要求只转发、不自己改代码、不跑验收。
- **判定「一次完成」**：两步。① 在 worktree 里跑任务自带的验收命令（20/20 通过）；② **独立评审子代理**对照任务说明逐个读 diff（实现是否完整正确、回归测试是否真能抓住问题、范围、是否引入缺陷），给出 complete / minor-issues / needs-rework。只有 complete 记作一次完成。评审结论里两条最重的（T07 跨包测试失败、T02-kimi 的 in-place feature 回退）由我复跑与读码核实属实。
- **Claude 侧 token（Q2）口径**：主口径 = 这次委派实际进入主线程上下文的内容（skill 文本 + 任务说明 + 工具调用与结果 + 结果摘要 + host 的回复），按字符折算 token。另列「原始上下文增长」作上限参考：它被环境噪声主导——本机的全局插件 / hook 让同一配置的空会话首轮上下文在 3.7 万–4.7 万之间波动，首次工具调用后还会注入 0.9 万–1.5 万（用 `echo` 两次的对照会话测得），与 pactify 无关。
- **callee token**：codex 用线程累计（`tokenUsage.total`，从 job 日志取；实测版本的摘要只给了最后一次请求，见缺陷 1）；kimi 用会话记录逐轮 `usageScope: turn` 之和。

## 汇总

| | codex | kimi |
|---|---|---|
| 大任务一次完成 | 9/10 | 6/10（+2 小问题） |
| 墙钟中位数 / 最长 | 3m53s / 6m40s | 7m19s / 23m13s |
| callee 输入 token 中位数 / 最大 | 1.08M / 2.55M | 1.46M / 8.25M |
| callee 输出 token 中位数 | 7.8k | 13.6k |
| 工具调用中位数 | 23.5 | 34.5 |
| Claude 侧 token 中位数（内容估算） | 3.1k | 3.7k |
| 只读任务 | 3/3 交回结论，质量高 | 0/3（全部打满上限） |

两家输入 token 都以缓存读为主（codex T01：106.7 万输入里 101.5 万是缓存）。host 侧 26 次总成本 $0.76（haiku）。

## 逐次记录

| 运行 | 状态 | 请求 → 实际模型 | 核实 | callee 输入 / 输出 token | 工具调用 | 改动 | 墙钟 | Claude 侧 token（内容估算 / 原始增长） | 后台 wait 次数 | 评审结论 |
|---|---|---|---|---|---|---|---|---|---|---|
| T01-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 1.07M / 4k | 16 | 3 文件 +47/−4 | 2m48s | 3k / 29k | 1 | ✅ 一次完成 |
| T01-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 546k / 5k | 18 | 3 文件 +72/−2 | 4m45s | 3k / 30k | 1 | ✅ 一次完成 |
| T02-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 1.45M / 11k | 23 | 5 文件 +226/−7 | 4m37s | 3k / 29k | 1 | ✅ 一次完成 |
| T02-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 8.25M / 66k | 90 | 5 文件 +337/−7 | 23m13s | 4k / 33k | 2 | ❌ 需返工 |
| T03-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 1.60M / 7k | 34 | 7 文件 +103/−28 | 3m51s | 3k / 29k | 1 | ✅ 一次完成 |
| T03-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 2.84M / 18k | 59 | 10 文件 +139/−13 | 12m07s | 4k / 32k | 2 | ✅ 一次完成 |
| T04-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 1.99M / 14k | 32 | 9 文件 +259/−14 | 6m40s | 3k / 29k | 1 | ✅ 一次完成 |
| T04-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 2.45M / 19k | 65 | 9 文件 +235/−8 | 12m58s | 4k / 31k | 2 | ✅ 一次完成 |
| T05-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 2.55M / 15k | 31 | 14 文件 +199/−20 | 6m25s | 3k / 29k | 1 | ✅ 一次完成 |
| T05-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 1.48M / 10k | 37 | 6 文件 +106/−20 | 8m19s | 4k / 30k | 1 | 🟡 小问题 |
| T06-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 902k / 7k | 23 | 3 文件 +107/−2 | 4m00s | 3k / 28k | 1 | ✅ 一次完成 |
| T06-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 618k / 3k | 12 | 0 文件 +0/−0 | 4m50s | 3k / 28k | 1 | ✅ 一次完成 |
| T07-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 1.09M / 7k | 35 | 4 文件 +181/−2 | 3m16s | 3k / 29k | 1 | ❌ 需返工 |
| T07-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 5.17M / 19k | 72 | 9 文件 +258/−15 | 13m31s | 4k / 32k | 2 | ❌ 需返工 |
| T08-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 904k / 10k | 24 | 3 文件 +433/−7 | 3m55s | 3k / 29k | 1 | ✅ 一次完成 |
| T08-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 1.45M / 17k | 32 | 4 文件 +490/−5 | 6m18s | 4k / 30k | 1 | 🟡 小问题 |
| T09-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 799k / 8k | 23 | 3 文件 +166/−0 | 3m04s | 3k / 28k | 1 | ✅ 一次完成 |
| T09-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 615k / 6k | 17 | 4 文件 +190/−0 | 3m29s | 3k / 29k | 1 | ✅ 一次完成 |
| T10-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 481k / 6k | 10 | 4 文件 +119/−7 | 2m31s | 3k / 28k | 1 | ✅ 一次完成 |
| T10-kimi | succeeded | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 463k / 6k | 15 | 4 文件 +98/−18 | 2m18s | 3k / 29k | 1 | ✅ 一次完成 |
| R1-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 2.37M / 13k | 30 | 0 文件 +0/−0 | 3m41s | 4k / 29k | 1 | 见只读评估 |
| R1-kimi | failed · 工具调用超限（41 次） | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 1.19M / 13k | 41 | 0 文件 +0/−0 | 4m11s | 2k / 27k | 1 | 见只读评估 |
| R2-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 721k / 6k | 18 | 0 文件 +0/−0 | 2m32s | 6k / 31k | 1 | 见只读评估 |
| R2-kimi | failed · 工具调用超限（41 次） | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 2.14M / 24k | 41 | 0 文件 +0/−0 | 13m45s | 3k / 28k | 2 | 见只读评估 |
| R3-codex | succeeded | gpt-6.1-sol → gpt-6.1-sol | ✓ | 562k / 8k | 16 | 0 文件 +0/−0 | 3m17s | 4k / 28k | 1 | 见只读评估 |
| R3-kimi | failed · 工具调用超限（41 次） | kimi-code/kimi-for-coding → kimi-code/kimi-for-coding | ✓ | 1.66M / 16k | 41 | 0 文件 +0/−0 | 5m53s | 2k / 27k | 1 | 见只读评估 |

「后台 wait 次数」>1 的都是超过约 10 分钟的委派：后台命令到默认时限被停掉，host 自己查状态后重发了一次（见缺陷 2）。

## 独立评审要点（大任务）

| 任务 | codex | kimi |
|---|---|---|
| T01 worker 不继承 stdin | ✅ | ✅ |
| T02 合并门禁跑 feature 的代码 | ✅ 临时 worktree、无分支的 in-place feature 仍原地跑、门禁命令读活账本 | ❌ 无分支时退回 `feat.ID`，所有 in-place feature 合并都会失败；门禁配置读自 feature 树 |
| T03 `[RUNGUARD-DEAD]` | ✅ status 时间戳结构上不再依赖注入的 Now，对账测试用真实写入方 | ✅ 修法成立，但对账测试绕过了 CLI 接线 |
| T04 测试不碰真实 `~/.pactify`（AJ-610） | ✅ | ✅ |
| T05 kimi home 单一来源 | ✅ 含 `agent.go` 的 kimi 配置路径与生成文档 | 🟡 漏了 `internal/agent/agent.go:121`（任务说明要求全仓搜索） |
| T06 并行 merge 失败要升级 | ✅（未编造 bug，补了真实冲突测试） | ✅（明确指出已在 `540f577` 修过，零改动） |
| T07 assign 校验 reviewer 角色 | ❌ 拒掉了保留评审名 `human`；serve / orchestrate / planner 的测试被打红，验收命令没覆盖这些包 | ❌ 同样拒掉 `human`；跑了全量测试，但把 `human` 注册成座席去迁就新规则，掩盖了回归 |
| T08 `sub logs` / `sub status --json` | ✅ | 🟡 `--follow` 会把写了一半的末行当 stderr 打出并丢掉整行；`status <id> --json` 从对象改成数组 |
| T09 CLI 版本变化提示 | ✅（已采纳进本 PR） | ✅ |
| T10 diff 末行计数 + kimi 记录末行截断 | ✅（已采纳进本 PR） | ✅（截断判定偏宽） |

**T06 的题面是错的**：任务说明来自过期的 backlog 条目，bug 其实已在基线修过。kimi 直接指出并零改动；codex 没点破题面，而是做了一个小改进加一条真实冲突测试。两家都按「一次完成」计。去掉 T06 后：codex 8/9、kimi 5/9，结论不变。

**评审对两家的整体判断**：codex 更会考虑改动对系统其余部分的影响（T02 保住 in-place feature、T03 从结构上消除失效模式、T08 处理半行），测试更全（表驱动、泄漏检查、非 UTC 时区）；kimi 主路径通常对，但漏边界。范围纪律两家都好，没有无关的生产代码改动。

## 只读任务

| 任务 | codex | kimi |
|---|---|---|
| R1 各厂商 kind 的映射表与不一致 | 3m41s，完整表格 + 不一致清单 | 打满 41 次工具调用，**无结论** |
| R2 评审 `internal/sub` 的并发与取消 | 2m32s，找出 4 个 high、若干 medium（见下「后续」） | 打满上限（13m45s），**无结论** |
| R3 编排器空闲看门狗 | 3m17s，代码路径 + 弱点 + 改进建议 | 打满上限，**无结论** |

kimi 在 plan 模式下并非空转：R1 的 41 次调用里 35 次是正常的 Read / Grep / Glob，但它坚持要写计划文件并 `ExitPlanMode`，被拒后又 `AskUserQuestion`，始终不交回最终答复。三次都**没有任何文件改动**——只读边界本身守住了，失败在「不收敛」。

## 实测中发现并已修的缺陷

| # | 缺陷 | 影响 | 修法（本 PR） |
|---|---|---|---|
| 1 | codex 的 job token 取的是 `tokenUsage.last`（最后一次请求） | T01 摘要显示 62.5k / 258，实际线程累计 106.7 万 / 3.9k，**少报约 17 倍** | 收集器优先取 `tokenUsage.total` |
| 2 | 主线程后台跑的 `pactify sub wait` 超过约 10 分钟会被 Claude Code 默认时限停掉（job 本身还在跑） | 26 次里 5 次（全是 kimi 的长任务）；host 都靠临场判断重发 wait 才救回来 | skill 要求后台 wait 带 `timeout: 7200000`，并说明「被停掉 ≠ 完成」 |
| 3 | codex 额度事件只进了 job 日志、没进摘要 | Q4 要求的「附额度事件」不可见 | `Result.RateLimit`，摘要多一行「额度」 |
| 4 | 交接模板只要求「验收命令」 | T07 两家都漏了跨包回归（一家漏测，一家改测试迁就） | 模板要求带全量测试命令、写出已知隐含约束、不许为迁就新行为改旧测试 |
| 5 | kimi 只读不收敛（K1） | 3/3 无结论 | 第一版拒绝 `kimi --read-only`（退出 2，指向 codex） |
| — | 冒烟时发现：显式 `/pactify:codex` 被 skill 的「小任务自己做」改写成 Claude 自己动手；未请求模型时摘要显示空 | 用户的明确选择被覆盖 | 已在 PR #71 修 |

另采纳两项实测产出（独立评审判 complete、本 PR 用 `-race` 复验）：T09-codex 的 **CLI 版本变化提示**（补上 K3 的能力），T10-codex 的 **diff 末行计数**与 **kimi 记录末行截断容忍**。其余任务产出只用于评估，不合入（T01–T03、T06、T07 属于已冻结的编排器 / 协议层；对应缺陷已登记到 Notion Requests Inbox）。

## 后续（未在本 PR 处理）

- **`pactify sub` 的取消与生命周期**（R2-codex 的发现，均为读码结论、未复现）：
  - 强制取消只杀 worker 的进程组，codex / kimi 子进程在自己的进程组里，可能残留；
  - 取消与 prompt 启动之间有竞态，宽限期内审批仍可能放行写操作；
  - queued 状态下取消与 worker 认领之间有读改写竞态；
  - 只凭 PID 校验进程身份，PID 复用时可能误杀；
  - 状态写盘失败时 job 可能永远停在 queued / running，`wait` 不检查 worker 存活。
  这些属于 M1 的运行时加固，需 Owner 确认后排期。
- **kimi 重测**：用新交接模板对 T02、T05、T07、T08 重跑 kimi，判断 Q1 的不达标是交接问题还是 kimi 本身。
- **K3 实测**：等任一 CLI 自动更新后，确认摘要首行出现版本变化提示。
- **Q2 口径**：若要精确的 host 开销，需要在干净的 Claude Code 配置（无全局插件 / hook）下复测原始上下文增长。
