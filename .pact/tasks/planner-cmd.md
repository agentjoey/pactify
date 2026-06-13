# Task p-cmd · CLI（pactify plan / plan apply）

**Feature:** planner · **Owner:** claude · **Reviewer:** opencode-worker · **Deps:** p-prompt, p-apply

## 目标
`pactify plan` / `pactify plan apply` 子命令，串起 采集→启动 planner agent→（审）→apply。集成 + planner agent 启动——核心。

## 改文件
新建 `cmd/pactify/cmd_plan.go`；`cmd/pactify/commands.go` 注册 `newPlanCmd()`（加进 root.AddCommand 列表）；`cmd/pactify/cli_test.go` 加 smoke。

## 行为
- `pactify plan "<goal>" --feature <id> [--auto] [--run] [--planner-kind <kind>]`：
  1. 采集 RepoTree（顶层 + 关键目录，如 `git ls-files` 取顶层或浅层 walk）+ roster（`pact.At(cwd).StateProjection()` 的 Agents）→ SeatInfo（**MVP 简化：Drivable 默认 true**，GUI 精确判定留 backlog）→ `planner.BuildPrompt`。
  2. 经 `orchestrate.NewCmdRunner().Run(ctx, "planner", plannerKind, prompt, cwd)` 启动 planner agent（plannerKind 默认 "claude-code"，模型已 pin）——agent 自身写 `.pact/tasks/<feature>-*.md` + `.pact/plan-<feature>.json`。
  3. 默认停（提示"已生成，审阅后 `pactify plan apply <feature>`"）；`--auto` → 直接走 apply 逻辑；`--run` → apply 后链 orchestrate（exec 或提示命令）。
- `pactify plan apply <feature> [--run]`：读 `.pact/plan-<feature>.json` → `planner.Parse` → `planner.Apply(cwd, plan, roster)` → 报 assign 数；`--run` 链 orchestrate。

## 验收（cli_test smoke，建 binary，PACT_AGENT_ID=claude）
`plan --help` 含 `--feature/--auto/--run/--planner-kind`；`plan apply --help` 含 `--run`；在临时 .pact 项目放一份合法 `.pact/plan-X.json` + 对应 spec 文件 → `plan apply X` 成功 assign（**smoke 只测 apply 路径，不真启 planner agent**——启 agent 需真 LLM，归集成验收）。

## verify
```
go test ./cmd/pactify/ -run Plan && go build ./...
```

## 完成方式
TDD。座席 claude。`pactify checkpoint p-cmd` 附 verify 输出。不自接受。
