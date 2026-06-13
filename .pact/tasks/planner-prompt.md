# Task p-prompt · planning prompt 组装

**Feature:** planner · **Owner:** claude · **Reviewer:** opencode-worker · **Deps:** p-manifest

## 目标
把"目标 + repo 结构 + roster + pactify 约定 + manifest schema"组装成给 planner agent 的指令文本。这是 planner 智能的脚手架——核心。

## 改文件
新建 `internal/planner/prompt.go` + `prompt_test.go`。

## 契约
```go
package planner
type SeatInfo struct { ID string; Roles []string; Drivable bool } // Drivable = agent.Get(kind).Runner() ok（GUI=false）
type PromptInput struct {
    Goal     string
    Feature  string
    RepoTree string       // 顶层 + 关键目录树文本（调用方采集）
    Seats    []SeatInfo
}
func BuildPrompt(in PromptInput) string
```

## BuildPrompt 产出的 prompt 必须指示 planner agent：
- 把目标拆成最小可交付 task 集 + 依赖链（串行，规避单工作树）；
- 每个 task 写规格到 `.pact/tasks/<feature>-<id>.md`（含目标/文件/契约/验收/`verify:` 行）；
- 写 manifest 到 `.pact/plan-<feature>.json`，schema = Plan/PlanTask（prompt 里给出 JSON 例）；
- 座席分配：owner≠reviewer；**优先用 Drivable=true 的座席**；按复杂度分配（复杂→能力强座席）；GUI（Drivable=false）仅必须时用并在 spec 注明需人工交接；
- 每个 task 给机器可读 `verify:` 命令（go test / vitest 等）。

## 验收（prompt_test.go，纯函数，断言关键片段存在）
BuildPrompt 输出含：Goal、Feature、RepoTree、每个 Seat 的 id+Drivable 标注、`.pact/tasks/` 与 `.pact/plan-` 路径、manifest schema 示例、owner≠reviewer 与"优先可驱动座席"指示、verify 行要求。

## verify
```
go test ./internal/planner/ -run Prompt
```

## 完成方式
TDD。座席 claude。`pactify checkpoint p-prompt` 附 verify 输出。不自接受。
