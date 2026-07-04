# rp-e2e-docs — 包2 收尾:quorum 徽标 + 文档 + 全仓门

> 母 spec:`docs/specs/review-runtime-deepening.md` §8。依赖 rp-quorum + rp-fix-loop。

## 交付
1. web board 卡片:任务带 reviewers/quorum 时显示 `n/q ✓` 徽标(消费 rp-quorum 的 DTO 新字段;样式随现有统计条);Live 的 fixing 相位显示「修复中 n/max」。类型定义同步。
2. `docs/architecture.md` 驱动/评审小节更新(自修环、quorum、critic、QA 门、快照、动态座席、schedule——写已落地行为)。
3. `CLAUDE.md` Version 行补包2 描述(简短)。
4. 全仓门:`go test ./...` + `go vet` + bats + `cd web && npx vitest run && npx tsc -b --noEmit` 全绿=包2 合并门。
