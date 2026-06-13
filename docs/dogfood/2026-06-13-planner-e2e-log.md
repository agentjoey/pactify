# Planner (pactify plan) orchestrate 真 LLM e2e 观测 — 2026-06-13

载体：planner feature（pactify plan）。第三次 orchestrate 自主交付，**首次混编模型**：核心棒 claude(claude-opus-4-8)、标准棒 opencode(deepseek-v4-pro)。

## 结果
4 棒 p-manifest/p-prompt/p-apply/p-cmd 全 accepted、merge shipped（按复杂度分配：p-prompt/p-cmd→claude/opus，p-manifest/p-apply→opencode/deepseek）。但过程不像前两轮一次过——压出 4 个真问题（dogfood 价值）。

## 发现
### #1（已修）agent 挂死无超时
opencode/deepseek 3min 写完 p-manifest+测试过，但 checkpoint 前死挂 55min（进程 sleeping、0.7% CPU、无输出），无限阻塞串行驱动器。修：加 `--run-timeout`（per-agent 运行子上下文超时→杀子进程→软失败 Fails++→重派 owner=worker）。默认 30min，本轮用 15min。空闲超时（更精）记 backlog。

### #2（设计待定，backlog）orchestrator 接手是错模式
本轮手动替 opencode checkpoint p-manifest（其活恰好 100% 完成）——但若 worker 只干 5% 就挂，orchestrator 接手就成 worker，自主性破产。正确=杀+重试 worker，反复失败才升级。半成品续接/分类恢复/放弃阈值待设计（backlog）。

### #3（流程）分支错位假阴性
恢复时 runner_test 修复提交到 main，feat-planner 陈旧 → 硬门挂在旧测试。合 main 进 feat-planner 解。教训：fix-as-you-go 改的若是被 feature 分支共享的文件，要同步进 feature 分支。

### #4（真 bug，待正解）orchestrate merge 缺 acting seat
orchestrate 的 Merge 调引擎需 PACT_AGENT_ID，orchestrate 进程没设 → 卡最后一步。临时 `PACT_AGENT_ID=claude` 解。正解：orchestrate 加 `--as <seat>`（默认 PACT_AGENT_ID），启动时 fail-fast 若无 acting seat（而非干完所有活才在 merge 失败）。

## 递归 dogfood（#2 关键验收）✅
用 `pactify plan "<一句话>"` 让刚造好的 planner 规划 #3（liveview）：
- 生成 3 棒任务图（step1 orchestrate 状态吐出→step2 serve 端点→step3 前端面板），dep 链 + verify 齐全，**过机器校验门**，`plan apply` 成功 assign。
- planner **真懂代码库**：status 设计为投影（照搬 log/STATE 架构）、发现 serve 多项目作用域并把端点改对、owner≠reviewer 按复杂度+Drivable 分配。
- 结论：planner 不只是跑通，**规划质量真实可用**。liveview 即 #3，已规划+派发，可直接 orchestrate 建。

## 结论
混编模型自主交付成立；orchestrate 在更复杂任务下压出 4 个真问题（#1 已修硬化、#4 待正解、#2 待设计、#3 流程教训）——前两轮一次过是错觉，这轮才真正压测了它。planner 递归 dogfood 验证 #2 端到端可用。
