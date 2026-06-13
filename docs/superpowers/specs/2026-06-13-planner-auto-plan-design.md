# 目标→任务图自动规划（pactify plan）设计

日期：2026-06-13 · 状态：LOCKED（用户已批准设计）
来源：链路走查 #2（backlog）。把"定义要干的活"从"人手写任务规格 + assign"降到"说一句话目标 → planner 生成任务图 → 人审/微调 → orchestrate 跑"。让 Pactify 从"专家能用"变"说人话就能用"。
前置：orchestrate（已建）。#2 是后端，可被 orchestrate 自主造。

## 0. 目标
- `pactify plan "<目标>"` 驱动一个 planner agent 读 目标+repo+座席，生成一份 pact 任务图（任务规格 + 结构化 manifest）。
- 人 review/微调（编辑文件），`pactify plan apply` 落成 assign，再 `pactify orchestrate` 跑。
- **人审是开关**：默认要审（生成后停下）；`--auto` 跳过审、直接 apply（+可选自动 orchestrate）。
- **非目标**：UI 里 review / 导入 planner 文件（backlog）；planner 自动跑 orchestrate 之外的执行；多 feature 并行规划。

## 1. 流程
```
pactify plan "<目标>" [--feature <id>]      # planner agent(claude -p) 生成文件：
   .pact/tasks/<feature>-<task>.md          #   每 task 一份规格（契约 + verify 行）
   .pact/plan-<feature>.json                #   结构化 manifest
   （默认停在此，人 review/微调文件）
pactify plan apply <feature> [--run]        # 读 manifest → 机器校验 → 逐个 assign（--run 链到 orchestrate）
pactify orchestrate --feature <feature>     # 自主跑到底

# 跳过人审：
pactify plan "<目标>" --feature <id> --auto # 生成 → 校验 → apply（--auto 即 review 开关 OFF；可再 --run 自动起 orchestrate）
```
review 开关默认 ON（生成与 apply 分两步）；`--auto` = OFF。UI 配置默认值 → backlog。

## 2. 组件
- **`cmd/pactify/cmd_plan.go`**：`plan`（生成，可 --auto/--run）+ `plan apply`（落 assign，可 --run）。
- **`internal/planner/`**：
  - **manifest.go**：schema + 解析 + 校验。
    ```go
    type Plan struct { Feature, Branch string; Tasks []PlanTask }
    type PlanTask struct { ID, Owner, Reviewer, Spec, Verify string; Deps []string }
    func Parse(b []byte) (Plan, error)
    func (p Plan) Validate(roster []string) error  // roster = 已知座席 id
    ```
  - **prompt.go**：planning prompt 构造——组装 目标 + repo 结构（顶层 + 关键目录树）+ roster（座席/角色 + headless 可驱动性，via agent.Get(kind).Runner() ok）+ pactify 约定（task 规格格式、verify 行、铁律 owner≠reviewer、优先用可驱动座席、manifest schema + 写到哪）。纯函数，可单测。
  - **apply.go**：`Apply(dir string, p Plan, run bool) error`——校验后对每个 task 调引擎 Assign；run=true 则链 orchestrate。
- **planner agent 怎么跑**：复用 orchestrate 的 `Runner`（CmdRunner spawn `claude -p`），喂 prompt.go 的 planning prompt。**planner 用 claude**（规划是复杂推理，claude 强；不用 gemini-flash）。agent 用自身 Write 工具直接把规格文件 + manifest 写进 .pact/；pactify 随后读+校验。

## 3. 机器校验门（质量底线，类比 orchestrate 硬门）
`plan apply`（及 `--auto`）前校验 manifest，不过则**拒绝 apply** 并报具体问题：
- 每个 owner/reviewer 是已知 roster 座席；
- owner≠reviewer（铁律）；
- deps 都在本 feature 内、无环；
- 每个 task 有非空 verify；
- 每个 task 的 spec 文件存在。
座席分配：planner 优先派 headless 可驱动座席（opencode/claude-code/gemini-cli）；若派了 GUI 座席（无 runner），校验门**告警标注**该棒需人工/经 escalation 交接（不阻断，但提示）。

## 4. 错误处理
- planner agent 写出畸形 manifest（JSON 解析失败 / 缺字段）→ Parse 报错，提示重跑 `plan` 或手改。
- 校验失败 → 列出每条违规，不 apply。
- planner agent 非零退出 / 没产出文件 → plan 命令报错。
- apply 时某 task id 已存在（全项目唯一）→ 引擎 Assign 报错，整体 apply 失败（原子性：先全校验再逐个 assign；中途失败需人工清理——记为已知，候选改进事务化）。

## 5. 怎么造 + 测试（再演一次自主交付）
- **用 orchestrate 自主造 #2**：claude/opencode（+gemini-cli 第三异构，已备好 runner）开发 planner 后端。纯 Go，可全自主。任务图：manifest → prompt → apply → cmd，依赖链。
- **测试**：manifest Parse/Validate（各违规分支单测）、Apply（manifest→assign，临时 .pact）、prompt 构造（确定性上下文组装断言关键片段）。planner agent 的**规划质量是 LLM 相关、不单测**——靠跑验证。
- **递归 dogfood（验收亮点）**：#2 造好后，用 `pactify plan "加一个实时编排 dashboard 视图（#3）"` 让 **#2 规划 #3 的任务图**——自举验证 planner 真能把一句话拆成可跑的图。

## 6. 工艺约束
- planner agent 调用复用 orchestrate Runner（不另造 exec 路径）。
- 校验门独立、机器化（不靠 LLM 自检）。
- manifest 是唯一结构化事实源；spec markdown 是 worker 读的契约，二者一致由校验保证（spec 文件存在性）。
- review 开关默认 ON（谨慎默认）；--auto 显式选择无人审。
