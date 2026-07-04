# rp-snapshot — 账本持久快照 + 增量 replay(正确性关键)

> 母 spec:`docs/specs/review-runtime-deepening.md` §5。**快照是缓存不是事实源**:任何可疑立即静默全量 fold;绝不提交进 git。

## 交付
1. `.pact/state-snapshot.json`:`{version:1, ledger_bytes, ledger_head_hash, state}`。写:每次 verb 成功后 withLedgerLock 内原子重写(tmp+rename)。读:fold 入口(CLI verbs + serve 冷启动)先验(version && bytes≤size && 头 hash 合)→ 只 fold 偏移后新行;任何不满足→全量。`PACT_NO_SNAPSHOT=1` 逃生阀跳过读写。
2. 快照文件进 runtime-ignore 机制(`.git/info/exclude`,找 v0.8.0 现有接线处追加)。
3. projection.State 确认 JSON 往返无损(有不可序列化字段则修)。
4. 基准:5k 行账本 `go test -bench`,快照路径 >5x 全量。

## 测试(评审最严)
等价性:多形态样例账本(空/单行/长/追加后)全量 fold == 快照+增量;损坏回退(截断/篡改头/版本不符);锁下并发 verb 快照不撕裂。`go test ./internal/pact/ ./internal/projection/` 绿。
