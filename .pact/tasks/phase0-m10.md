---
id: phase0-m10
feature: phase0-sec
owner: kimi-worker
reviewer: claude
---

# phase0-m10 — 修复 M10：单行坏事件 brick 整个账本读取（medium, availability）

## 目标 / Goal
`internal/event/log.go` 的 `parse` 用 1MiB cap 的 `bufio.Scanner`，遇第一条 >1MiB 或畸形行就 `return nil,err` / scanner 停摆——于是所有 ReadAll/ParseAll（全 verb、status、dashboard、validate、snapshot fallback）失败，整个项目账本不可读、无恢复。读账本必须有韧性：跳过坏行（记数/告警）、返回其余，绝不因单行 brick 整个项目。

## 改文件 / Files
- `internal/event/log.go`（`parse` 韧性化，主修）
- `internal/pact/rules.go`（`checkCheckpoint` 给 evidence 加长度上界，次要预防）

## 契约 / Contract
1. **【必做·测试门控】** `parse`：改为容错读取——
   - 把 scanner buffer 上界抬到较大值（如 `16<<20`），使常见的大 evidence 行能读到；
   - 对 `json.Unmarshal` 失败的行 **跳过并 continue**（不再 `return nil,err`）；
   - `sc.Err()` 若是 `bufio.ErrTooLong`（行 >16MiB）**不作为 brick 传播**（返回已读事件 + nil）。
   - 可选更稳：用 `bufio.Reader` 做**有界**逐行读（每行上界 16MiB，超界的行 drain 到下个换行后跳过），既跳过超长行又续读其后的有效行、且内存有界（**别用无界 ReadBytes → OOM，参考 M6**）。
   - 保持成功路径字节等价：全有效行时结果与现在完全一致。
2. **【次要预防】** `internal/pact/rules.go` 的 `checkCheckpoint`：给 evidence 加长度上界（如 >1MiB 拒绝，附可操作错误），使有效事件永远远小于读 cap。
3. 不改其他行为。

## 验收 / Acceptance（dimension: correctness）
- `go test ./internal/event/ -run TestSEC_M10` **两条转绿**（现红：超长/畸形行 brick）。
- 现有 `internal/event` / `internal/projection` / `internal/pact` 测试**全不破**（尤其 snapshot fold 的字节等价）。
- `go build ./...` 通过；`go vet ./internal/event/ ./internal/pact/` 干净。

verify: go test ./internal/event/ ./internal/projection/ ./internal/pact/ -run 'TestSEC_M10|Parse|Read|Fold|Snapshot|Checkpoint' && go build ./...
