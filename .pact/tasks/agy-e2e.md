# agy-e2e — antigravity 端到端 dogfood 回归

tier: L1
verify: go test ./internal/orchestrate/ -run 'Agy|Antigravity'
dimension: correctness

## 目标 / Goal

把「真 `.pact` 项目 → assign → agy 干活 → checkpoint」这条链固化成**可重跑**的验证。

**为什么排在 token/续接之前**：坑「不传 `--add-dir` 就写进 scratch」是**静默失败**——
agy 照常返回 `SUCCESS`，只是文件写去了别处。**单测抓不到**，只有端到端能发现。
越早有这道回归，后续几棒越安全。

依赖：`agy-kind` 已 accepted。
计划：`.agent/plans/antigravity-cli-integration-2026-08-22.md` §4 A4。

## 已核实事实（前一会话手工 dogfood 实测通过，勿重新调查）

用一个临时 `.pact` 项目手工跑通过，结果：
- `PACT_AGENT_ID` **穿透到 agy 的 shell 工具** —— 账本里 `join`/`checkpoint` 均以该座席写入
- 状态正确停在 `awaiting_review`，**未自标 accepted**（不变量守住）
- 代码落在 **RepoDir 的 feature 分支**，非 scratch
- **零越界**：只碰 spec 允许的文件
- evidence 带真实 `go test` 输出

调用形态（照此复现）：
```
PACT_AGENT_ID=<seat> PACT_DIR=<repo>/.pact \
agy -p "<workerBrief>" --add-dir <repo> --model gemini-3.7-flash-low --effort low \
    --dangerously-skip-permissions --output-format json --print-timeout 300s
```

## 改文件 / Files

- `internal/orchestrate/` 下新增测试文件
- 如需测试脚手架，可加 helper；**不要**改生产代码（那是前两棒的事）

## 契约 / Contract

写一个**可跳过**的集成测试：

- 环境无 `agy` 二进制或未认证时 → `t.Skip`（CI 上必须能跳过，不得让 CI 变红）
- 有 agy 时：建临时 git 仓 + `pactify init` + assign 一个微任务 → 驱动 agy → 断言结果

### Reviewer 路径（Human Owner 已决定 agy 可承担 reviewer）

除 worker 外，须再覆盖一条 reviewer 链：让 agy 作为**另一个** task 的 reviewer，
对一个已 `awaiting_review` 的 task 做裁决。

⚠️ **两条不变量必须被验证，不能只验"能跑"**：
1. **worker 不能自接受** —— agy 作为 owner 时**不得**产出 `accept` 事件
2. reviewer 裁决须是 `accept` 或 `changes_requested` 之一，且由**该 reviewer 座席**写入

reviewerBrief 已有既有形态（`brief.go` 的 `reviewerBrief`）：读 spec、看 `git diff`、
**亲跑验收命令**、然后 `pactify accept` 或 `pactify changes`。照它驱动即可。

**核心断言（worker 路径，缺一不可）**：
1. 账本里出现该座席的 `checkpoint` 事件（协议闭环 + 身份穿透）
2. task 状态为 `awaiting_review`（**不是** accepted——不变量）
3. **产出文件在 RepoDir 内**，且 `~/.gemini/antigravity-cli/scratch/` 下**没有**本次的产物
   ← 这条是坑①的专属护栏，**必须有**
4. 未越界改 spec 之外的文件

## 验收 / Acceptance

评审维度：**correctness**。

- 无 agy 环境下测试 **skip** 而非 fail（用 `exec.LookPath` + auth 文件存在性判断）。
- 有 agy 环境下 worker 四条核心断言全过。
- **reviewer 路径**：agy 作为 reviewer 能对 `awaiting_review` 的 task 产出裁决事件
  （`accept` 或 `changes_requested`），且事件的 agent_id 是该 reviewer 座席。
- **不变量**：agy 作为 owner 的 task 上**不存在** agy 写的 `accept` 事件。
- **反向验证**：临时去掉 `--add-dir` 重跑一次，断言 3 **必须失败**——
  证明这条护栏真的能抓到坑①（把该反向验证的结果写进 evidence，不必留在代码里）。
- 门绿：`go test ./internal/orchestrate/`
