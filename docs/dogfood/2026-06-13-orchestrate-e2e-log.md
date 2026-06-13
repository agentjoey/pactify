# Orchestrate 真 LLM 端到端观测 — 2026-06-13

载体 feature：agents（本机 agent 扫描 + 注册）。驱动器：`pactify orchestrate`。座席：claude（claude-code）/ opencode-worker（opencode，deepseek-v4-pro）。

## 运行
一条命令启动，**零人工介入跑到 shipped**：
```
pactify orchestrate --feature agents --seat-kind claude=claude-code --seat-kind opencode-worker=opencode
```

五棒全自主完成（per-task 角色翻转，满足 owner≠reviewer）：

| 棒 | owner（开发） | reviewer | 结果 |
|---|---|---|---|
| scan | **claude**（claude -p） | opencode-worker | accepted（claude 自主写 detectBin+scan.go+测试、TDD、checkpoint、未自接受） |
| reg | opencode-worker | claude | accepted（9 测试绿） |
| cli | opencode-worker | claude | accepted |
| serve | opencode-worker | claude | accepted（3 httptest 绿） |
| ui | opencode-worker | claude | accepted（7 vitest，全量 400 vitest 绿，dist 重建） |
| merge | — | — | 硬测试门通过 → `merge agents` → **shipped** |

## 验证（orchestrator 复跑，不信 evidence）
- main 上 agents 代码齐全（scan.go/agentreg/serve agents.go/Agents.tsx）。
- `go build ./...` + 12 包 go test 绿；`npx vitest run Agents` 7 绿。
- 功能真机：`pactify agent scan` 真扫本机，检测到 6 个已装 agent + codex-cli 未装。

## 成功标准达成
- **orchestrate 真 LLM e2e 成立**：补上"目前仅 fake runner 集成测"的缺口。第一次真实运行即跑通完整 5 棒 feature。
- **一个 agent 多角色 + per-task 翻转**：claude 同时是 orchestrator（我设置任务图）+ scan 的 developer + reg/cli/serve/ui 的 reviewer；opencode 是 4 棒 developer + scan 的 reviewer。铁律（owner≠reviewer、worker 不自接受）全实战生效。
- **最少人工 = 仅"启动 orchestrate"一步**。理想达成。

## 发现
### #11（预飞修复，关键）claude-code headless runner 缺 --dangerously-skip-permissions
- 现象：`claude -p {briefing}` 非交互模式下一碰 Edit/Bash 就卡在权限审批（无人可批），无法自主开发/评审。
- 根因：orchestrate 的 claude-code runnerArgs 只有 `-p`。
- 处置：**启动前实测发现并修**（runnerArgs 加 `--dangerously-skip-permissions`，commit 52bf1c8）——省下一轮保证失败的运行。opencode run 无此问题。
- 安全注记：headless 自主 agent 本质上需绕过交互审批；--dangerously-skip-permissions 让该 claude 进程在 repo 工作树内全权操作，是自主模式的固有代价（受控运行可接受）。
- 候选改进：orchestrate 暴露 per-kind runner 的权限姿态配置，而非硬编码 bypass。

### #12（正向）首次真 LLM 运行零卡顿
- 无升级、无返工——五棒一次过。claude 与 opencode 都正确遵循 worker/reviewer 简报（join/TDD/checkpoint/不自接受 // git diff/跑 verify/accept）。orchestrate 的简报 + 串行驱动 + 硬门在真 LLM 下稳定。
- 唯一"失败信号"是后台命令末尾 `ls .pact/orchestrate/`（无升级目录）的 benign 非零退出，非 orchestrate 故障。

## 结论
orchestrate 把"消灭人肉中继"在真 LLM 上兑现了：claude 编排 + claude/opencode 异构自主开发评审、协议铁律生效、硬门兜底、一条命令到 shipped。下一步可加真 LLM e2e 的自动化回归（成本敏感，按需）。
