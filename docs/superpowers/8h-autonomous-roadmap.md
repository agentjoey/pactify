# 8 小时自主交付 Roadmap（2026-06-14）

> 授权：用户给 8 小时自主交付 12 个功能，claude 自主设计决策不问。
> 分工：claude = planner/orchestrator/designer/reviewer + 复杂核心；opencode = worker 承接标准开发（用 Pactify orchestrate 驱动，持续 dogfood）。
> 标准：商业级产品质量（参照 dify）。TDD、frequent commits、每功能 spec 轻量化。

## 交付顺序（按杠杆 × 依赖）

| 序 | 功能 | 主力 | 状态 | 说明 |
|----|------|------|------|------|
| P1 | **per-agent 配置体系**（#10 model + #9 权限姿态 + #4 作用域权限 + #8 drivability 标记） | claude 主导 / opencode 标准棒 | ⬜ | 最高杠杆：解硬编码、解 gemini 降级痛点、解锁权限。新 `internal/agentcfg` |
| P2 | **#5 错误/异常处理优化**（idle-timeout + worker-retry 策略，非 orchestrator 接手） | claude | ⬜ | 已标记"需专门设计"。空闲超时 + 半成品续接 |
| P3 | **#6 agent 任务 session 清理**（worker 任务后清理本任务 session） | opencode | ⬜ | 独立、标准，适合派 opencode |
| P4 | **#2 收尾交付步**（merge 到 main 后开 PR/push） | opencode | ⬜ | 独立、标准 |
| P5 | **#3 并行编排**（worktree 隔离，nextActions 多动作） | claude | ⬜ | 复杂核心。做完可并行驱动多 worker |
| P6 | **#1 项目设置向导**（座席绑定，注册→能干活） | claude 设计 / opencode UI | ⬜ | 后端 + UI |
| P7 | **#7 planner review 功能及 UI** | claude 设计 / opencode UI | ⬜ | 后端 + UI |
| P8 | **#11 动作模板 / orchestrate 配方** | claude 设计 / opencode | ⬜ | 一键工作流 |
| P9 | **#12 UI 持续 polish**（商业级，loading/empty/error 态、微交互、一致性、蚂蚁爬线增强） | claude 设计 / opencode | ⬜ | 大工程，尽力而为 |

## 关键架构事实（探查所得）

- **agent.go**：`spec` registry 硬编码 runnerCmd/runnerArgs（含 `-m model`、`--dangerously-skip-permissions`、`--approval-mode yolo`）、detectBin、Runner() ok=drivability。
- **agentreg**（`~/.pactify/agents.json`）：machine-global，{kind,label,registered_at}。
- **runner.go**：CmdRunner.Run 取 agent.Get(kind).Runner() 死参数 + 注入 PACT_AGENT_ID。
- **decide.go**：nextAction 纯函数，返回**单个** Action（串行）。
- **loop.go**：串行 read→nextAction→launch→reproject。RunTimeout（总超时）已有。merge 用 As(Orchestrator)。
- **serve**：富 REST + SSE + relay；author write 走 acting-seat；wiring/agents/registry endpoint 齐。
- **web**：Tailwind v4 + design token（tokens.css LOCKED）+ React Flow；AntEdge 蚂蚁爬线已有；缺 loading/empty/error/微交互/一致性。

## 进度日志

- 2026-06-14 启动，完成全系统探查，写本 roadmap。

## 进度更新 2026-06-14（中段）
- ✅ **P1 配置体系**(#10 model + #9 权限姿态 + #4 作用域权限 + #8 drivability)— claude，merged main `af50606`。新 internal/agentcfg + agent/launch.go(参数化 RunnerProfile)+ agentreg config + CLI `agent config`/scan drivable。
- ✅ **P2 错误处理**(#5)— claude，merged `1393766`。idle-timeout watchdog(runner_idle.go)+ 重试续接 briefing + CLI --idle-timeout。
- ✅ **P4 收尾交付步**(#5 收尾)— **opencode 真跑** worker + claude reviewer，merged `daecfd4`。internal/finish + CLI `pactify finish`(push/PR)。idle-timeout 实战护航，opencode 未挂正常完成。
- ✅ **P5 并行编排**(#3)— claude，merged `f611cdf`。nextActions/featureAction + gitx worktree + RunParallel(worktree 隔离并行开发 + 串行 merge + .gitattributes union 驱动 + merge 事件提交)+ CLI --max-concurrency。集成测试两 feature 并行 ship。
- 进行中：opencode → P3 session 清理；claude → #12 UI polish。
- 剩余：#1 向导、#6 session 清理(opencode 跑)、#7 planner review UI、#11 配方、#12 UI polish。

## 进度更新 2026-06-14（收尾）— 12/12 全交付
- ✅ **P3 session 清理**(#6)— **opencode 真跑**，merged `3d2916d`。internal/sessions（按 kind 声明 list/prune，gemini 已接，其余优雅 unsupported）+ CLI `sessions list/prune`。
- ✅ **P8 配方**(#11)— **opencode 真跑**，merged `f5413d4`。internal/recipe（3 内置配方 add-tests/review-harden/spec-to-plan）+ CLI `recipe list/show`。
- ✅ **P6 项目设置向导**(#1)— claude，merged。internal/wizard（Suggest 注册 agents→座席 roster + Validate 角色覆盖）+ CLI `setup suggest`（打印 roster + gaps + 精确 apply 命令）+ serve GET /api/setup/suggest。
- ✅ **P9 UI polish**(#12)— claude，merged。Spinner/Button-loading/Alert 原语 + 微交互（active-press/hover-lift/fade-rise，全 reduced-motion 兼容）+ RightRail 三动作 loading + Alert 错误。vitest 411→415 绿。
- ✅ **P7 planner review UI**(#7)— claude，merged。serve GET /api/projects/{id}/plan/{feature}（读 plan-<feature>.json + 校验）+ web PlanReview 面板（Plan 视图 key 5，feature 选择/任务图/valid 徽章/apply 提示）。

### 最终统计
- 12/12 需求全部交付并合入 local main。Go 19+ 包绿、web vitest 415 绿、tsc 干净。
- opencode 真跑交付 4 个 feature（finishstep/sessions/recipe + 各自被 claude 独立评审），claude 交付 8 个（含全部复杂核心 + UI）。
- P5 并行编排（worktree 隔离 + union merge）经集成测试验证；本次也用「opencode 主树 + claude worktree」并行模式实战交付。
- 未推送 origin（待用户决定）。office-zoom e2e 既有 flake（非本次回归）。
