# Task p-manifest · manifest schema + 解析 + 校验

**Feature:** planner · **Owner:** opencode-worker · **Reviewer:** claude · **Deps:** 无

## 目标
planner manifest 的结构、解析、机器校验门。

## 改文件
新建 `internal/planner/manifest.go` + `manifest_test.go`。

## 契约
```go
package planner
type PlanTask struct {
    ID, Owner, Reviewer, Spec, Verify string
    Deps []string `json:"deps,omitempty"`
}
type Plan struct { Feature, Branch string; Tasks []PlanTask }
func Parse(b []byte) (Plan, error)              // JSON 解析；缺必填/畸形 → error
func (p Plan) Validate(roster []string) error   // 校验门，违规聚合成一个清晰 error
```
注意 JSON tag：字段需 json tag（feature/branch/tasks/id/owner/reviewer/spec/verify/deps），与 Parse round-trip 一致。

## Validate 规则（违规聚合，列出每条）
- Feature/Branch 非空；
- 每个 task：ID/Owner/Reviewer/Spec/Verify 非空；
- Owner、Reviewer ∈ roster（传入的已知座席 id）；
- Owner≠Reviewer（铁律）；
- task ID 在本 plan 内唯一；
- 每个 dep 指向本 plan 内另一 task ID、非自指、整图无环（DFS）。
（spec 文件存在性不在此——apply 层做。）

## 验收（manifest_test.go）
合法 plan→Validate nil；逐个注入违规（未知座席/owner==reviewer/缺 verify/dep 不存在/自指/成环/重复 id）→ 各报对应错误；Parse 畸形 JSON→error；Parse round-trip。

## verify
```
go test ./internal/planner/ -run Manifest
```

## 完成方式
TDD。座席 opencode-worker。`pactify checkpoint p-manifest` 附 verify 输出。不自接受。
