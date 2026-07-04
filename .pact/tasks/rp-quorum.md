# rp-quorum — quorum 多评审(协议扩展,严格 opt-in)

> 母 spec:`docs/specs/review-runtime-deepening.md` §2。**单 reviewer 路径必须逐字节不变**(golden)。

## 交付
1. `assign --reviewers a,b,c --quorum 2`(与 --reviewer 互斥,cmd 校验);账本 assign payload 加 `reviewers[]`+`quorum`,旧格式兼容读。
2. 投影:awaiting_review 后,本次 checkpoint 之后的不同 reviewer accept 数 ≥ quorum → accepted;任一 changes → rework 且清零本轮 accepts(重 checkpoint 后全员重新表态)。DTO 保留 reviewer 字段(填第一个),新增 reviewers/quorum/accepts。
3. 引擎:accept 校验 acting seat ∈ reviewers;worker 自 accept 依旧拒(推广现有校验)。
4. 驱动:多 reviewer 串行逐个跑,已 accept 跳过,凑齐提前停。

## 测试
fold:2/3 凑齐、changes 清零重计、旧账本 golden;引擎拒绝用例;驱动顺序+提前停。`go test ./internal/pact/ ./internal/projection/ ./internal/orchestrate/` 绿。

## 边界
web UI 不做(rp-e2e-docs 里加 board 徽标);merge 门语义不动(accepted 即 accepted)。
