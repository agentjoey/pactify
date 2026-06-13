# Planner（pactify plan）Implementation Plan（orchestrate 驱动）

> **For agentic workers:** 本计划【不走】subagent-driven-development。实现者是 `pactify orchestrate` 拉起的真 LLM——核心/复杂棒 claude(claude-opus-4-8)、标准棒 opencode(deepseek-v4-pro)，模型已 pin 进 runner。本计划由 orchestrator（claude，本 session）执行 §A 运行手册：写任务图 + assign（owner 按复杂度）+ 跑 orchestrate + 观测。§B 是写入 `.pact/tasks/` 的任务规格。

**Goal:** 加 `pactify plan "<目标>"`——driver 一个 planner agent 把一句话目标拆成 pact 任务图（任务规格 + manifest），人审/微调后 `pactify plan apply` 落 assign，再 orchestrate 跑。

**Architecture:** spec = `docs/superpowers/specs/2026-06-13-planner-auto-plan-design.md`（LOCKED）。新包 `internal/planner`（manifest schema/parse/validate、planning prompt 组装、apply→engine.Assign）+ `cmd/pactify/cmd_plan.go`（plan/plan apply，--auto/--run，planner agent 经 orchestrate.Runner 启动）。

**Tech Stack:** Go。复用 `internal/pact`（Assign/StateProjection）、`internal/agent`（kind/roster 可驱动性）、`internal/orchestrate`（Runner/NewCmdRunner 启动 planner agent）。

**约定:** 实现者 TDD；每 task 规格带 `verify:` 行（orchestrate 硬门）；planner agent 调用复用 orchestrate.Runner（不另造 exec）；校验门机器化独立于 LLM 自检。

**owner 按复杂度（用户指定）**：核心/复杂 → claude(opus-4.8)；标准 → opencode(deepseek-v4-pro)；reviewer per-task 翻转（owner≠reviewer）。

---

## §A — Orchestrator 运行手册（claude 执行）

### A0 准备
```bash
cd /Users/xtation/AgentWorks/Code_Claude/pactify
git checkout main && git pull --ff-only && go build -o /opt/homebrew/bin/pactify ./cmd/pactify
git checkout -- .pact/STATE.yml .pact/log.jsonl 2>/dev/null   # 清浮动残留
PACT_AGENT_ID=claude pactify status | head -6                  # 复用 roster：claude / opencode-worker
```
- [ ] **A0.1** 基线干净、二进制最新、roster 确认（claude[orchestrator,reviewer]、opencode-worker[worker]；角色 advisory，assign 只校验 owner≠reviewer）。
- [ ] **A0.2** 写 §B 四份任务规格到 `.pact/tasks/`，commit。

### A1 派发（owner 按复杂度 + 角色翻转，feature=planner branch=feat-planner）
- [ ] **A1.1**（确切子命令以 `pactify assign --help` 为准）：
  - `p-manifest` 标准 → owner=opencode-worker，reviewer=claude，deps 无
  - `p-prompt` 核心 → owner=claude，reviewer=opencode-worker，deps=p-manifest
  - `p-apply` 标准 → owner=opencode-worker，reviewer=claude，deps=p-manifest
  - `p-cmd` 核心 → owner=claude，reviewer=opencode-worker，deps=p-prompt,p-apply
  commit assign 事件。（注：claude 拥有 p-prompt+p-cmd 两棒；join 会把两者翻 in_progress，但 p-cmd 的 deps 未满足故不可派——dep 门 + in_progress 可派逻辑已在 agents 跑验证过。）

### A2 跑 orchestrate（无人闭环，三模型已 pin）
- [ ] **A2.1** dry-run 确认首动作（应 RunOwner p-manifest seat=opencode-worker）：
```bash
pactify orchestrate --feature planner --seat-kind claude=claude-code --seat-kind opencode-worker=opencode --dry-run
```
- [ ] **A2.2** 真跑：
```bash
pactify orchestrate --feature planner --seat-kind claude=claude-code --seat-kind opencode-worker=opencode --max-rework 3 --max-fails 2
```
  完成或卡住（escalation 暂停）都接手；卡住读 `.pact/orchestrate/escalation-*.md` 根因→修→续跑。

### A3 收尾 + 递归 dogfood
- [ ] **A3.1** shipped 后复跑验证（不信 evidence）：`go build ./... && go test ./...`；真机 `pactify plan --help` + `pactify plan "<小目标>" --dry-run`（若实现了 dry-run）。
- [ ] **A3.2 递归 dogfood**：用 `pactify plan "加一个实时编排 dashboard 视图"` 让 **planner 规划 #3 的任务图** → 人审生成的 manifest → 验证 planner 真能把一句话拆成可跑的图。记观测。
- [ ] **A3.3** orchestrate e2e 观测续篇（三模型混编：claude-opus-4-8 核心 / deepseek-v4-pro 标准）、sprint、memory。

### 修复纪律
orchestrate/agent 卡协议或 CLI bug：orchestrator 开 fix/* 独立分支修 + 回归 + PR，记观测，续跑。

---

## §B — 四份 Pact 任务规格（写入 .pact/tasks/）

### p-manifest · manifest schema + 解析 + 校验（owner=opencode-worker, reviewer=claude, deps 无）

**目标**：planner manifest 的结构、解析、机器校验门。

**新建**：`internal/planner/manifest.go` + `manifest_test.go`。

**契约**：
```go
package planner
type PlanTask struct {
    ID, Owner, Reviewer, Spec, Verify string
    Deps []string `json:"deps,omitempty"`
}
type Plan struct { Feature, Branch string; Tasks []PlanTask }
func Parse(b []byte) (Plan, error)                 // JSON 解析；缺必填字段/畸形 → error
func (p Plan) Validate(roster []string) error      // 校验门，违规聚合报错
```
Validate 规则（违规聚合成一个清晰 error，列出每条）：
- Feature/Branch 非空；
- 每个 task：ID/Owner/Reviewer/Spec/Verify 非空；
- Owner、Reviewer ∈ roster（传入的已知座席 id）；
- Owner≠Reviewer（铁律）；
- task ID 在本 plan 内唯一；
- 每个 dep 指向本 plan 内的另一 task ID（同 feature）、非自指、整图无环（DFS）。
（spec 文件存在性校验不在此——由 apply 层做，因它需要 dir。）

**验收**：合法 plan→Validate nil；逐个注入违规（未知座席/owner==reviewer/缺 verify/dep 指向不存在/自指/成环/重复 id）→ 各报对应错误；Parse 畸形 JSON→error；Parse round-trip。

**verify:** `go test ./internal/planner/ -run Manifest`

**完成方式**：TDD。座席 opencode-worker。`pactify checkpoint p-manifest` 附 verify 输出。不自接受。

### p-prompt · planning prompt 组装（owner=claude, reviewer=opencode-worker, deps=p-manifest）

**目标**：把"目标 + repo 结构 + roster + pactify 约定 + manifest schema"组装成给 planner agent 的指令文本。这是 planner 智能的脚手架——核心。

**新建**：`internal/planner/prompt.go` + `prompt_test.go`。

**契约**：
```go
package planner
type PromptInput struct {
    Goal        string
    Feature     string            // 目标 feature id
    RepoTree    string            // 顶层 + 关键目录树文本（调用方采集）
    Seats       []SeatInfo        // roster + 可驱动性
}
type SeatInfo struct { ID string; Roles []string; Drivable bool } // Drivable = agent.Get(kind).Runner() ok（GUI=false）
func BuildPrompt(in PromptInput) string
```
BuildPrompt 产出的 prompt 必须指示 planner agent：
- 把目标拆成最小可交付 task 集 + 依赖链（串行，规避单工作树）；
- 每个 task 写一份规格到 `.pact/tasks/<feature>-<id>.md`（含目标/文件/契约/验收/`verify:` 行）；
- 写 manifest 到 `.pact/plan-<feature>.json`，schema = Plan/PlanTask（给出 JSON 例）；
- 座席分配：owner≠reviewer；**优先用 Drivable=true 的座席**；按复杂度分配（复杂→能力强的座席）；GUI（Drivable=false）座席仅在必须时用并在 spec 注明需人工交接；
- 每个 task 给机器可读 `verify:` 命令（go test / vitest 等）。

**验收**（prompt_test.go，断言关键片段存在，纯函数）：BuildPrompt 输出含 Goal、Feature、RepoTree、每个 Seat 的 id+Drivable 标注、`.pact/tasks/` 与 `.pact/plan-` 路径、manifest schema 示例、owner≠reviewer 与"优先可驱动座席"指示、verify 行要求。

**verify:** `go test ./internal/planner/ -run Prompt`

**完成方式**：TDD。座席 claude。`pactify checkpoint p-prompt` 附 verify 输出。不自接受。

### p-apply · manifest → assign（owner=opencode-worker, reviewer=claude, deps=p-manifest）

**目标**：把校验通过的 manifest 落成 pact assign。

**新建**：`internal/planner/apply.go` + `apply_test.go`。

**契约**：
```go
package planner
// Apply 校验 plan（Validate + 每个 task 的 Spec 文件在 dir 下存在）后，对每个 task 调
// pact.At(dir).Assign(task.ID, plan.Feature, plan.Branch, task.Owner, task.Reviewer, task.Spec, task.Deps)。
// 先全校验再逐个 assign（任一校验失败则不 assign 任何）。返回 assign 了几个 + error。
func Apply(dir string, plan Plan, roster []string) (assigned int, err error)
```
- 校验 = plan.Validate(roster) + 每个 task.Spec 在 dir 下存在（os.Stat）。
- 任一不过 → 返回 error，不 assign。
- 全过 → 逐个 Assign；中途 Assign 失败（如 id 已存在）→ 返回已 assign 数 + error（非原子，已记 backlog）。

**验收**（apply_test.go，临时 .pact 项目，参考 internal/pact 测试建 project + roster）：合法 plan + spec 文件存在 → Apply 成功，StateProjection 显示各 task assigned 且 owner/reviewer/deps 正确；spec 文件缺失 → error 不 assign；校验失败（owner==reviewer 等）→ error 不 assign。

**verify:** `go test ./internal/planner/ -run Apply`

**完成方式**：TDD。座席 opencode-worker。`pactify checkpoint p-apply` 附 verify 输出。不自接受。

### p-cmd · CLI（owner=claude, reviewer=opencode-worker, deps=p-prompt,p-apply）

**目标**：`pactify plan` / `pactify plan apply` 子命令，串起采集→启动 planner agent→（审）→apply。集成 + planner agent 启动——核心。

**改文件**：新建 `cmd/pactify/cmd_plan.go`；`cmd/pactify/commands.go` 注册 `newPlanCmd()`；`cmd/pactify/cli_test.go` 加 smoke。

**行为**：
- `pactify plan "<goal>" --feature <id> [--auto] [--run]`：
  1. 采集 RepoTree（顶层 + 关键目录，如 `git ls-files | 取顶层` 或浅层 walk）+ roster（`pact.At(cwd).StateProjection()` 的 Agents，Drivable 查 `agent.Get(seatKind).Runner()` ok——seat→kind 这里暂用：CLI 默认全 Drivable=true 的占位 OR 从 --seat-kind 映射；MVP 简化：Drivable 先按 roster 角色无法判 kind，故 RepoTree+Seats 采集后 Drivable 默认 true，GUI 判定留 backlog）→ `planner.BuildPrompt`。
  2. 经 `orchestrate.NewCmdRunner().Run(ctx, "planner", "claude-code", prompt, cwd)` 启动 planner agent（claude，模型已 pin）——agent 用自身工具写 `.pact/tasks/<feature>-*.md` + `.pact/plan-<feature>.json`。
  3. 默认停（提示"已生成，审阅后 `pactify plan apply <feature>`"）。`--auto` → 直接走 apply 逻辑（读 manifest+Validate+Apply）；`--run` → apply 后链 `orchestrate`（exec `pactify orchestrate --feature <id> ...` 或提示命令）。
- `pactify plan apply <feature> [--run]`：读 `.pact/plan-<feature>.json` → Parse → `planner.Apply(cwd, plan, roster)` → 报 assign 数；`--run` 链 orchestrate。
- `--planner-kind`（默认 claude-code）：planner agent 的 kind。

**验收**（cli_test smoke，建 binary）：`plan --help` 含 `--feature/--auto/--run/--planner-kind`；`plan apply --help` 含 `--run`；在一个临时 .pact 项目放一份合法 `.pact/plan-X.json` + 对应 spec 文件 → `plan apply X` 成功 assign（不真启 planner agent——smoke 只测 apply 路径；启 planner agent 需真 LLM，归 A3 真机）。

**verify:** `go test ./cmd/pactify/ -run Plan && go build ./...`

**完成方式**：TDD。座席 claude。`pactify checkpoint p-cmd` 附 verify 输出。不自接受。

---

## Self-Review 结论
- **spec 覆盖**：§1 流程→p-cmd（plan/apply/--auto/--run）；§2 组件→p-manifest（manifest）+p-prompt（prompt）+p-apply（apply）+p-cmd（cmd，复用 orchestrate.Runner）；§3 校验门→p-manifest.Validate + p-apply 的 spec 存在性；§4 错误处理→各 task 验收的违规分支；§5 自主造+递归 dogfood→§A 运行手册 A2/A3.2；§6 工艺→复用 Runner、校验机器化。无缺口。
- **owner 按复杂度**：核心 p-prompt/p-cmd→claude(opus-4.8)；标准 p-manifest/p-apply→opencode(deepseek-v4-pro)；reviewer 翻转，全满足 owner≠reviewer。
- **类型一致**：Plan/PlanTask（p-manifest）被 p-apply/p-cmd 引用一致；BuildPrompt/PromptInput/SeatInfo（p-prompt）；Apply 签名（p-apply）被 p-cmd 调；engine.Assign/orchestrate.Runner/NewCmdRunner 对照真实代码。
- **有意留白**：p-cmd 的 Drivable/seat→kind 判定 MVP 简化（默认 true，GUI 精确判定留 backlog）——避免在 plan 阶段引入 seat→kind 持久化（那是另一项）。
