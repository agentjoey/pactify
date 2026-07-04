# rp-critic — critic 预评分(默认关)

> 母 spec:`docs/specs/review-runtime-deepening.md` §3。依赖 rp-fix-loop(接力点:verify 门绿之后)。分数**无门控权**——只影响 reviewer 注意力。

## 交付
1. 配置:`.pact/config` 的 `critic: <seat>`(同 gate config 事件机制)+ `--critic seat` flag 覆盖;默认无=零行为变化。
2. loop.go:门绿后、reviewer 前发 critic 棒(该 seat kind 照常解析),briefing=「只读评审 diff vs spec,末行 `CRITIC_SCORE: 0.0-1.0` + 一句理由」。
3. 解析末行分数(缺失→null,越界钳制)→ 任务级 note 事件(payload {critic_score, critic_by, reason},复用现有 note/start 机制不加 event_type)→ reviewer briefing 注入「critic 预评 X.X:<理由>」。
4. critic 棒失败/超时=跳过(软),不重试不升级不阻塞。

## 测试
解析三分支;注入内容断言;失败不阻塞;默认关 golden。`go test ./internal/orchestrate/ -run Critic` 绿。
