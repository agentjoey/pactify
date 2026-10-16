# agy 大任务评估（AJ-650，与 Phase 0 同等规模）

> M2 里程碑（Linear P-AJ-18 · AJ-650）。方法照搬 [`phase0-report.md`](phase0-report.md)，结果可以和 codex、kimi 的首轮直接对比。
> 实测 2026-10-16 · 基线 `88df743`（与 Phase 0 相同）· agy 1.3.5，评估途中自动升级到 1.3.6 · 模型 `gemini-3.8-flash-high`（agy 列表里最强的 Gemini 档）· pactify main `182afa9`（含 AJ-649）· host：Claude Code headless，模型 haiku，经插件 `/pactify:agy` 委派

> AJ-711 supersedes the read-only closure below with the v2 plan policy and fail-closed baseline. The evaluation results remain historical; reevaluation is outside AJ-711.

## 结论

| 指标 | codex（Phase 0） | kimi（Phase 0） | **agy** |
|---|---|---|---|
| 一次完成（10 个大任务） | 9/10 | 6/10 | **1/10** |
| 小问题 | 0 | 2 | 5 |
| 失败 / 需返工 | 1 | 2 | **4**（3 题是 Gemini 后端出错，1 题空闲超时） |
| 返工一次后完成 | — | — | **2/10**（失败的 4 题各续接一次：T04 完成；T03、T05、T07 又被后端打断） |
| 只读任务收敛（3 个） | — | 0/3（plan 模式打满工具上限） | **0/3**（只读档下 shell 一律被拒，agy 随即结束本轮，30–40 秒内无结论） |
| 模型兑现 | 26/26 | — | **13/13**（`init.model` 回报，与请求一致） |
| 会话留痕 | 无 | 无 | **无**（镜像 HOME，用户的 agy 会话索引全程不变） |

**Owner 已拍板（2026-10-16）：保持实验性，关闭只读档，不再测。**

按 kimi K1 的先例处理：`sub run agy --read-only` 以退出码 2 拒绝执行，并指向 `codex --read-only`；旧只读 job 的续接同样拒绝，doctor 的只读探测为 SKIP。
Gemini 4 Argon 重跑的只读任务同样 0/3；整轮重跑因个人额度耗尽未完成。

## 逐题结果

| 题 | 首轮 | 判定 | 要点 |
|---|---|---|---|
| T01 | 成功 | minor | 修复正确，覆盖了全部 spawn 点；回归测试的超时只有 500ms，评审复跑时偶发失败 |
| T02 | 成功 | minor | 主体正确；分支缺失时会悄悄退回基线目录跑门禁；默认门禁的类型仍从基线目录识别；gofmt 未跑 |
| T03 | **失败** | — | 调用工具 103 次后 Gemini API 返回 EOF；续接一次后又因 stream 中断失败 |
| T04 | **失败** | 返工后 complete | 10 分钟无进展，触发空闲超时；续接后完成，评审判为 complete |
| T05 | **失败** | — | 503 UNAVAILABLE；续接一次后再次 503 |
| T06 | 成功 | **complete** | 实现正确，测试用了真实的 git 冲突，时序稳定 |
| T07 | **失败** | — | 「The stream was interrupted」；续接时 `agy models` 被超时杀掉（pactify 缺陷，见下） |
| T08 | 成功 | minor | `--follow` 首次读取可能把一行切成两半；4 个文件未跑 gofmt；`status <id> --json` 的输出结构变了 |
| T09 | 成功 | minor | `st.Error` 的改动没有测试，还丢掉了成功 job 原有的行为；未跑 gofmt；附带了范围外的重构 |
| T10 | 成功 | minor | 代码正确，验收也能通过；但 agy 启动测试后就结束了本轮，没有核对和报告结果 |

判定口径与 Phase 0 相同：先跑任务自带的验收命令（评估全部跑完后统一串行执行，避免负载干扰），再由独立评审对照任务说明逐个读 diff，只有 complete 记作一次完成。验收命令通过不等于完成：T03、T05 的现有测试在改动不完整时照样能过。

## 观察到的行为

- **后端可靠性是最大问题。** 10 个大任务里 3 个被 Gemini 后端打断（EOF、503、stream 中断），4 次续接里又有 2 次因同样原因失败。
- **会在后台启动命令后结束本轮。** T10 的最后一句话是「已启动测试，完成后再查看」，T04 首轮也是这样之后陷入空闲。在 brief 里写明「前台运行并等待完成」后有所缓解（续接时 T04 完成了）。
- **代码质量的细节**：多题没有跑 gofmt；回归测试时序偏紧；会做 brief 之外的小改动。
- **成本**：中位数每题约 22.8 分钟，输入 744 万 token（含缓存），输出 6.7 万。host 侧（haiku 转发）每次约 $0.015。
- **评估期间 agy 自动升级到 1.3.6**，pactify 的版本漂移检测随即给出警告。这在真实环境里验证了 Phase 0 时没能触发的 K3。

## 评估中发现并已修复的 pactify 缺陷

- **AJ-661**（#94）：agy 的终稿把过程中的旁白也拼了进去，改为取最后一个回复步骤。
- **AJ-670**（#99）：「CLI version changed」被记成 job 的失败原因，掩盖了真实原因。
- **AJ-671**（#101）：`agy models` 预检在负载下超时，导致 job 失败。预检失败现在不再阻断，agy 自己和 `init.model` 仍会校验模型。

## 数据与复现

- 任务说明、运行和验收脚本、逐题日志：Phase 0 评估目录（`briefs/`、`run-agy.sh`、`accept-agy.sh`）。
- 每题的 job：`agy-home/jobs/<id>`（含 worktree、`log.jsonl`、`result.json`）。
