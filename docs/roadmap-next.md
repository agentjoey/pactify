# Pactify 下一阶段 Roadmap：未实现清单 + UI 规划（2026-06-14）

承接 8h 自主交付（12 功能 shipped）。本文两部分：
- **A. 已提需求中尚未实现的功能点**（盘点全部历史 backlog + 12 功能里的缺口）
- **B. 现有（多为 CLI/后端-only）功能的 UI 界面规划**

---

## A. 已提需求未实现清单

### A1. 链路缺口（核心闭环还差的）
| 项 | 状态 | 说明 |
|----|------|------|
| **#4 人审门 / review gate** | ❌ 未做 | 全自动↔监督式开关：合并前/每棒停下等人点头（`--review-gate`）。现仅 `--dry-run`。是"可信"的关键。 |
| **自然语言命令面板** | ❌ 未做 | dashboard 里"输入一句话→驱动 agent"对话入口 + "等待你"人在环状态。人→编排者的交互面（现 orchestrate 仅 CLI）。 |
| **#3 并行视图聚合** | ⚠️ 半 | RunParallel 已建，但每 worktree 各写自己的 status.json，Live 面板只读单个——多 feature 并行进度未聚合展示。 |
| **#7 plan review 的编辑/apply** | ⚠️ 半 | 面板只读；缺 UI 内编辑 owner/reviewer/deps + "Apply"/"Run" 按钮（现 apply 仅 CLI）。 |

### A2. 已知 bug / 稳定性（应尽快修）
| 项 | 说明 |
|----|------|
| **join 冷启动被未来 dep 误杀** | 真 bug：join 全量校验座席所有 assigned 任务，未来 gated 任务会让 join exit 1（在 checkout 分支前）。本次用专用座席绕过。正解：dep 门控在任务真正开工时校验。 |
| **gemini 免费档静默降级 flash** | 根因已查实（oauth-personal 撞配额自动降 flash）。修复=换 API key 档（`GEMINI_API_KEY`/Vertex，密钥进 Keychain，runner 从 Keychain 取注入子进程 env）。 |
| **post-merge STATE.yml 滞后** | 待查：merge 后工作树 STATE 与 HEAD 提交的 STATE.yml 一度不一致（代码已 merged、仅投影文件滞后）。 |
| **错误处理完整设计** | idle-timeout 已做，但"半成品续接 vs reset 重做的选择、何时放弃转人工、分类恢复"未系统设计。 |
| **plan apply 事务化** | apply 中途某 assign 失败不回滚已 assign 的（需人工清理）。 |

### A3. 可观测 / 工程化（缺）
| 项 | 说明 |
|----|------|
| **成本/可观测** | 每棒 token + 花费、运行历史审计。8h 跑下来无成本可见。 |
| **make install + orchestrate flag 自检** | 防二进制陈旧（本次踩坑：`unknown flag --idle-timeout`）。 |
| **worktree bootstrap 脚本** | web worktree 需软链 node_modules 等。 |
| **架构文档补"并行 merge = union 驱动 + 重算投影"** | 关键洞察未入文档。 |

### A4. 增强（nice-to-have）
- 导入外部 planner manifest；planner 人审默认开关配置化。
- 把 #11 配方做成"委派 spec 模板引擎"（spec 详细度=委派成功率）。
- 产品化"人 + agent 并行（park+worktree）"为 `orchestrate --with-human` 或文档。
- scoped-permissions 的 UI 配置面（见 B 部分）。

### A5. 搁置
- **Canvas P0 交互重做**：原始的画布手感/连线 bug 修复（box 跳动、连线不可交互、Office 无操作/无缩放）——当时决定先跑通无 UI 稳定性，搁置至今。office-zoom e2e 既有 flake 也在此范畴。

---

## B. 现有功能的 UI 界面规划

**核心洞察**：Pactify 现在**后端已能端到端跑通** `注册 agent → 配座席 → 配方/规划 → 评审 → 编排(并行) → 看进展 → 交付`，但**大半只有 CLI**。UI 的任务是把这条链做成 dashboard 里可点的流程。下面按这条用户旅程排，每个对应已建的后端/endpoint。

### B1. Setup / Onboarding 视图（#1 向导）—— 体验起点，最高优先
- **现状**：`internal/wizard` + `GET /api/setup/suggest` 已建，**无 UI**。
- **规划**：新视图（或 Ops 内一页）"Setup"。
  - 读 `GET /api/setup/suggest` → 展示建议座席 roster（lead + workers，drivable 标记），warnings 用 Alert。
  - 每行可编辑 seat id / 角色（下拉）。
  - "Apply" 按钮 → 新增 `POST /api/setup/apply`（后端跑 init + agent wiring）。
  - 空注册表态 → 引导去 Ops 注册 agent（接 `agent scan/register`）。
- **新增 endpoint**：`POST /api/setup/apply`。

### B2. Recipes 视图（#11 配方）—— 降门槛入口
- **现状**：`internal/recipe`（3 配方）+ CLI，**无 UI、无 endpoint**。
- **规划**：新视图"Recipes"。
  - 配方卡片列表（name + description），选一个。
  - 输入框填"一句话目标"。
  - 实时预览展开的任务图（调 expand）。
  - "Generate plan" → 写 `.pact/plan-<feature>.json` → 跳到 **Plan 视图（#7）** 复审。
- **新增 endpoint**：`GET /api/recipes`、`POST /api/recipes/{name}/expand`（或 generate）。
- **价值**：配方 → plan review(#7) → orchestrate 串成一条可点的链。

### B3. Plan 视图增强（#7，已建只读）
- **现状**：PlanReview 面板已建（只读，Plan 视图 key 5）。
- **优化**：
  - 任务行内编辑 owner/reviewer/deps。
  - "Apply"（→ `POST .../plan/{feature}/apply`，包 `plan apply` + 事务化）。
  - "Run orchestrate"（→ 触发编排，见 B5）。
  - 人审默认开关。

### B4. Agent Config 面板（#10/#9/#4 配置体系）
- **现状**：`agent config`（model/权限姿态/allowed-tools）+ scan drivable，**仅 CLI**。
- **规划**：Ops 内"Agents"页每个已注册 agent 一行：
  - model 下拉（覆盖默认 pin）。
  - 权限姿态切换（blanket 自动批准 ↔ scoped allowlist + 工具多选）。
  - drivable/manual 徽章（已有 scan 信号）。
- **新增 endpoint**：`GET/POST /api/agents/{kind}/config`。

### B5. Orchestrate 控制 + 命令面板（#3 + 自然语言面板）
- **现状**：orchestrate 仅 CLI；Live 视图只读单个 status。
- **规划**：
  - dashboard 顶部"Run"入口：选 feature(s) + 并发数 → 启动 orchestrate（需后端起进程的 endpoint，注意安全）。
  - **自然语言命令面板**：输入一句话 → 选配方/planner 拆解 → 预览 → 跑。"等待你"人在环状态卡。
  - **并行进度聚合**：Live 面板展示多 feature 并行（每 worktree status 聚合）。
- **新增 endpoint**：`POST /api/projects/{id}/orchestrate`（启动，谨慎设计权限/沙箱）、并行 status 聚合读。

### B6. Review Gate / 升级 UX（#4 人审门）
- **规划**：Live 视图里当 orchestrate 暂停在 gate/escalation：
  - 卡片显示"为什么停"+ "看 diff" + 按钮：批准合并 / 打回(reason) / 接手 / 续跑。
  - 接 escalation 文件 + 一个 resume endpoint。

### B7. Ship 按钮（#5 收尾交付步）
- **现状**：`pactify finish`（push/PR）仅 CLI。
- **规划**：feature shipped 到本地 main 后，dashboard 显"Ship"按钮 → push / Open PR（调 finish）。
- **新增 endpoint**：`POST /api/projects/{id}/finish`。

### B8. Sessions 管理（#6，minor）
- Ops 内每 agent 一个"prune sessions"按钮（接 `sessions prune`），unsupported 显灰。

### B9. #12 polish 收尾（已起头）
- 已做：Spinner/Button-loading/Alert/微交互/hover-lift。
- 待补：Ops 三面板的空态 + loading 骨架；fetch 失败用 Alert（现静默）；Board 空态；focus ring 审计；按钮跨视图一致性。

### UI 优先级建议
1. **B1 Setup**（体验起点）+ **B9 polish 收尾**（商业级门面）
2. **B2 Recipes → B3 Plan apply → B5 Run**（把"说人话就能跑"的链点通）
3. **B6 Review Gate**（可信）+ **B7 Ship**（闭环终点）
4. **B4 Agent Config** + **B8 Sessions**（配置面）

> 注：UI 改动走 CLAUDE.md 的画布合并门（vitest + Playwright e2e 双绿）。office-zoom 既有 flake 需先单独修或隔离，避免污染门禁。
