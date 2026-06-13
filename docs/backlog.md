# Pactify Backlog

产品候选功能（未排期）。源头标注。

## 来自竞品分析（AgentHub，2026-06-13）
- **动作模板 / orchestrate 配方**：把常见多 agent 工作流做成一键配方（"加测试""共同评审""从需求到设计计划"），架在 orchestrate 之上——把门槛从"手写任务图 + assign"降到"选配方"。竞品 AgentHub 已有同款。
- **自然语言命令面板**：dashboard 里一个"输入指令 → 驱动 agent"的对话入口 + "等待你的"人在环状态，作为人→编排者的交互面（当前 orchestrate 仅 CLI 驱动）。

## 差异化备忘（对 AgentHub）
- AgentHub 靠中心化 "Hub"（服务器）协调，更新日志在反复修"Hub 连接不稳定"。Pactify 用 git+文件当唯一事实源、零服务器——**主打"没有 Hub 可以掉线、你的 repo 就是协调层"**，是 ADR-001"守 Team"之外更底层的技术护城河。

## 链路完善候选（2026-06-13 链路走查）
按用户旅程：①装+扫描注册 ✓ → ②建项目/配座席 → ③定义活 → ④orchestrate 跑 ✓ → ⑤看进展/介入 → ⑥交付远端
- **[#1] 项目设置向导（座席绑定）**：扫描注册完 agent 后，引导"用我注册的 agent 配这个项目"（选 agent→派座席+角色），接通"注册完"到"能干活"。是 #2/#3 的前置。
- **[#2] 目标→任务图自动规划（最大摩擦杀手）**：planner agent 把一句话目标拆成 pact 任务图（owner/reviewer/deps/verify），人 review/微调→orchestrate 跑。让 Pactify 从"专家能用"变"说人话就能用"。
- **[#3] 实时编排视图 + 升级 UX**：dashboard 看见正在发生的自主运行（谁在干/哪个 task/进度/token），一等的升级面（暂停→为什么→看 diff→续跑/改/接手）。把黑箱变可信的可见过程。
- **[#4] 人审门（可选开关）**：全自动 ←→ 监督式光谱，合并前/每棒停下等人点头（orchestrate 已有 --dry-run，加 --review-gate）。
- **[#5] 收尾交付步**：orchestrate 合并到本地 main 后集成"开 PR/推送"，链路终点是"交付到远端"。
- 次级：成本/可观测（每轮 token+花费、运行历史审计）、并行编排（worktree 隔离破 F1 单树）、作用域权限（--dangerously-skip-permissions → per-run allowedTools）。

**下一阶段已启动：#2 自动规划 + #3 实时编排视图（#1 作前置）。UI 开发拟交 antigravity。**

## 来自 #2/#3 brainstorm（2026-06-13）
- **scan/registry 标记 headless 可驱动性**：agent 扫描/注册时标出哪些 agent 能被 orchestrate 自动驱动（有 headless runner：opencode/claude-code/gemini-cli）vs 不能（GUI：antigravity/*-desktop——需人工或经 escalation 交接）。用户体验上明确"哪些能无人跑、哪些要你上手"。底层信号 = RunnerSpec ok。
- **headless runner 权限姿态**：claude 需 --dangerously-skip-permissions、gemini 需 --approval-mode yolo --skip-trust 才能自主开发（实测）。当前硬编码进 kind runner；候选改 per-run 可配权限姿态（allowedTools / approval-mode）。
- **antigravity 2.1.4 = 纯 GUI 无 headless 入口**（实测 double-confirmed）；自主编排的第三异构 agent 用 gemini-cli 替代。

## 来自 #2 planner brainstorm（2026-06-13）
- **planner review 在 UI 里做 + 配置默认开关**：`pactify plan` 的人审默认 ON（生成后停）、`--auto` 跳过；后续支持在 UI 里 review/编辑生成的任务图，并把"是否要人审"做成可配置默认（CLI 已有 --auto，UI 加开关）。
- **导入 planner 文件**：支持导入外部 planner 生成的任务图 manifest。
- **plan apply 事务化**：apply 中途某 assign 失败时回滚已 assign 的（当前非原子，需人工清理）。

## 来自 planner 真跑（2026-06-13）
- **orchestrate 空闲超时（比总超时更精）**：本次 opencode/deepseek-v4-pro 在 p-manifest 写完代码+测试过后【挂死】（进程 sleeping、0.7% CPU、25min 无输出、不 checkpoint），无限阻塞驱动器。已加 `--run-timeout`（总运行超时，默认 30min）兜底防无限阻塞；更优 = 空闲超时（无输出 N min 即杀），需在 osExec 包输出活动检测。
- **opencode/deepseek-v4-pro 慢+偶挂观察**：简单 manifest 棒耗时 ~25min 且 checkpoint 前挂死一次。记录为模型/runtime 稳定性观察；per-agent 模型配置（backlog）后可换标准棒模型。

## 错误处理设计（worker 挂死/部分完成的恢复）—— 需专门设计（2026-06-13）
**问题**：worker 卡住时，"orchestrator 接手完成"是错的一般模式——若 worker 只干了 5% 就挂，后面主要工作都由 orchestrator 做，自主性破产（orchestrator 变 worker）。本次手动抢救 p-manifest 只因它恰好 100% 完成（活+测试都过），是一次性补救，不可推广。
**正确方向**：挂死/失败 → 杀掉 + **重试 worker**（worker 重读状态+树里半成品，自己续/重做、自己 checkpoint），worker 始终是干活的人；反复失败到上限 → 升级给人。`--run-timeout` 已走这条路（超时→软失败→重派 owner=worker，非 orchestrator 接手）。
**待设计点**：
- 半成品续接：重试 worker 从未提交半成品续（省力但可能被半残状态搞晕）vs 先 reset 树干净从头（干净但浪费）——怎么选/怎么提示 worker。
- 何时放弃重试转人工：每次全量重试重做+烧 token，代价高，阈值/成本上限怎么定。
- 分类恢复："活干完只是 checkpoint 挂" vs "活没干完挂了"——能否更聪明地分类（前者只补 checkpoint，后者续/重做）。
- 空闲超时（无输出 N min）vs 总超时：空闲更快兜挂死、又不误杀合法慢任务（需 osExec 包输出活动检测）。
