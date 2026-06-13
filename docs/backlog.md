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
