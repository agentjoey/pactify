# Task doctor-json — pactify doctor 加 --json 机器可读输出

## 目标
给 `pactify doctor` 加 `--json` flag,输出机器可读的检查结果(数组),便于 CI / serve /
脚本消费。纯加性:不带 flag 时行为字节不变(仍是 `✓/✗ name — detail` 文本行 + 有问题退非零)。

## 改文件(仅这些 + 测试)
- `internal/doctor/doctor.go`(给 `Check` 加 json tag)
- `cmd/pactify/cmd_doctor.go`(加 --json flag + JSON 分支)
- `cmd/pactify/cmd_doctor_test.go`(新增)

## 契约
1. `internal/doctor/doctor.go` 的 `Check` struct 加 json tag:
   `Name string \`json:"name"\`` / `OK bool \`json:"ok"\`` / `Detail string \`json:"detail"\``。
   不改字段名/语义/任何逻辑。
2. `cmd/pactify/cmd_doctor.go` 的 `newDoctorCmd`:
   - 加 `var asJSON bool` + `cmd.Flags().BoolVar(&asJSON, "json", false, "emit checks as a JSON array")`。
   - RunE 里算出 `checks`(现有逻辑不变)后:
     - 若 `asJSON`:`json.NewEncoder(c.OutOrStdout())` 带 `SetIndent("", "  ")`,`Encode(checks)`
       输出整个数组;仍然遍历算 `allOK`;若有 !OK 仍 `return fmt.Errorf("doctor found issues")`
       (退出码语义与文本模式一致 —— JSON 已输出,错误只影响退出码)。
     - 否则:走现有文本分支不变。
   - 注意:cobra 默认会把 RunE 返回的 error 再打印到 stderr。保持现状即可(文本模式一直如此),
     JSON 仍在 stdout,干净可解析。
3. 需要 import `encoding/json`。

## 测试(cmd_doctor_test.go)
- 直接构造一个 `[]doctor.Check`(含一个 OK=true、一个 OK=false)→ json.Marshal → 断言含
  `"name"`/`"ok"`/`"detail"` 键、ok 的 bool 值正确。(验证 tag 生效。)
- 或/并:跑 doctor 子命令带 `--json`,捕获 stdout,json.Unmarshal 回 `[]doctor.Check`,
  断言解析成功且长度>0、每条有 Name。(注意 doctor 在测试环境可能整体 !OK 导致命令返回 error —
  这没关系,断言 stdout 仍是合法 JSON 数组即可;用 cmd.SetOut(buf) 捕获。)
  若跑真子命令不稳定,以纯 marshal 断言为准,子命令冒烟尽量做。

## 验收 / Acceptance(视角: maintainability — 加性、不带 flag 零行为变化、JSON 合法可解析)
- reviewer 独立跑 verify + 阅读 diff 确认文本分支未动、JSON 分支正确。

## verify
verify: go build ./... && go test ./internal/doctor/ ./cmd/pactify/
