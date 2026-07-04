# rp-fix-loop — fix-until-green 自修环 + draft 语义

> 母 spec:`docs/specs/review-runtime-deepening.md` §1(先读全节)。核心不变量 I-2:自修环只能收紧进评审的质量,绝不代行 accept。

## 交付
1. loop.go decide 流:worker checkpoint 后、reviewer 棒前,先跑该任务 verify 门(复用现有 gate 解析:task verify: > config gate)。红 → 对同 owner 发 fix 轮 briefing(verify 输出尾 2KB + 「你上一轮已 checkpoint,修复直到门绿再 checkpoint」),重跑;`--max-fix-rounds`(默认 2,进 Options)。fix 轮不计 MaxFails。轮次耗尽 → 现有 changes/escalation 语义(reason 带最后 verify 输出)。绿 → 正常进 reviewer。
2. runtime status(orchestrate status 端点/文件)加 `fixing` 相位 + 轮次 n/max,board 可见。不加新账本 event_type(轮次 driver 内存计数)。

## 测试
fake runner + 注入门命令:红→fix→绿→评审;耗尽→escalate;门绿直通(现状 golden 不变);fix 轮不烧 MaxFails;status 相位断言。`go test ./internal/orchestrate/ -run 'Fix|Gate'` 绿 + `go build ./...`。

## 边界
不动协议事件;不动 pact 引擎;critic/QA 的接力点(门绿之后)留清晰挂点即可,别实现它们。
