# Task sarif-output (C-7) — SARIF 2.1.0 输出:audit 记录 → GitHub Code Scanning

## 目标
新增 `pactify audit sarif`,把 audit 记录导出为 **SARIF 2.1.0** JSON,可直接喂 GitHub
Code Scanning。核心转换放在**可复用的纯包** `internal/sarif`(自带输入类型,不 import audit,
方便日后也给 review 事件复用)。纯加性,不改任何现有行为。

## 改文件(仅这些 + 测试)
- 新增 `internal/sarif/sarif.go`
- 新增 `internal/sarif/sarif_test.go`
- 新增 `cmd/pactify/cmd_audit_sarif.go`(或加到 cmd_audit.go —— 二选一,推荐单独文件)
- `cmd/pactify/cmd_audit.go`:把新子命令挂到 `newAuditCmd` 的 `AddCommand(...)`
- 新增 `cmd/pactify/cmd_audit_sarif_test.go`

## 契约

### internal/sarif/sarif.go(纯包,零外部依赖)
```go
package sarif

// Finding is one SARIF result's source-agnostic input.
type Finding struct {
    RuleID  string // e.g. "pact.audit.exec"
    Level   string // "note" | "warning" | "error" (SARIF result level)
    Message string
    // optional metadata surfaced under result.properties
    Seat, Task, Project, TS string
}

// Build assembles a SARIF 2.1.0 Log from findings. Rules are de-duplicated by
// RuleID and listed under tool.driver.rules. driverName e.g. "pactify".
func Build(driverName, driverVersion string, findings []Finding) Log
```
- `Log` 及其嵌套类型(Run/Tool/Driver/Rule/Result/Message/...)用带正确 json tag 的
  struct 精确建模 SARIF 2.1.0 的最小子集:
  - 顶层:`{"$schema":"https://json.schemastore.org/sarif-2.1.0.json","version":"2.1.0","runs":[...]}`
    (Schema 字段 json tag 用 `"$schema"`)。
  - `runs[0].tool.driver`:`name`(driverName)、`version`(driverVersion,omitempty)、
    `informationUri`(可给 "https://github.com/agentjoey/pactify")、`rules`(去重后的规则,
    每条 `{id, name, shortDescription:{text}}`,name 用 ruleId)。
  - `runs[0].results[]`:每个 finding 一条 `{ruleId, level, message:{text}, properties:{seat,task,project,ts}}`。
    properties 里空字段 omitempty。
- 无 physical location(audit 记录不绑源码行),SARIF 允许 result 无 locations,不要伪造。
- Level 只接受 note/warning/error;若 Finding.Level 非法或空,Build 里回退成 "note"。

### CLI `pactify audit sarif`
- flags 复用 audit 查询:`--project --seat --task --session --risk --since`(同 `audit log`),
  外加 `--out <file>`(为空则写 stdout)。
- 逻辑:`audit.Query(Filter{...})` 拿记录 → 每条 map 成 `sarif.Finding`:
  - RuleID = `"pact.audit." + r.Risk`(r.Risk 为空则 "pact.audit.unknown")。
  - Level = riskLevel(r.Risk):exec→"warning"、mcp→"warning"、write→"note"、read→"note"、
    其它→"note"。
  - Message = r.Summary(为空则用 r.Tool)。
  - Seat/Task/Project/TS 照填。
- `sarif.Build("pactify", <version 可传空串或已有 version 变量>, findings)` → `json.MarshalIndent(log, "", "  ")`
  → 写 out/文件。写文件成功后打印一行 `wrote N results to <file>`。

## 验收 / Acceptance(视角: correctness — SARIF 结构合规、level/rule 映射正确)
- `go build ./... && go test ./internal/sarif/ ./cmd/pactify/` 通过。
- sarif_test:Build 输出 version=="2.1.0"、runs 长度 1、driver.name=="pactify"、
  同 RuleID 的多个 finding 只产生 1 条 rule(去重)、level 非法值回退 note、
  json.Marshal 后能 json.Unmarshal 回 map 且含 "$schema"/"runs" 键。
- cmd 测试:构造几条 audit.Record(可用 audit.Append 到 t.TempDir 的 PACT 目录,或直接
  测 map 函数),跑 sarif 子命令,断言 stdout 是合法 JSON 且 results 数正确、level 映射对。
  (若注入 audit 存储路径困难,可把「Record→Finding 的 map 函数」提成 cmd 包内可测函数,
   单测该函数的 ruleId/level/message 映射,子命令本身冒烟即可。)

## verify
verify: go build ./... && go test ./internal/sarif/ ./cmd/pactify/
