# Headless Dogfood Stability Log — 2026-06-13

座席：claude(orchestrator+reviewer) / opencode-worker / gemini-worker(antigravity)
载体 feature：relay（M3.4）
spec：docs/superpowers/specs/2026-06-13-headless-dogfood-m3.4-relay-design.md
plan：docs/superpowers/plans/2026-06-13-headless-dogfood-m3.4-relay.md

本轮是 pactify 首次用自己的协议开发自己（self-hosting）。Phase 0（接线，worker 进场前）已暴露一批自托管摩擦点，逐条记录。

## 观察 / 摩擦点

### #0 antigravity kind 无 project entry 文件
- 阶段：A0.3 接线
- 现象：`pactify init --seat ...:antigravity` 报错 "kind antigravity has no project entry file; wire it with pactify agent add"。antigravity 的角色指令不会被烘焙进任何文件。
- 根因：antigravity 是 Global/desktop kind（MCP 全局配置 ~/.gemini/config/mcp_config.json），设计上无 project entry。
- 处置：roster 座席用 gemini-cli kind 占位（有 GEMINI.md entry），MCP 连接另由 `pactify agent add antigravity` 做。worker 角色靠 GEMINI.md 烘焙块 + 启动时 prompt + 自描述 MCP 工具三重兜底。
- 候选改进：给 antigravity 一个 GEMINI.md 之类的 project entry，或让 init 支持"仅 roster 不烘焙"的座席声明。

### #1 MCP/桌面型 agent 进不了 roster
- 阶段：A0.3
- 现象：init --seat 拒绝无 entry 的 kind；而 join 不扩 roster（M1.1）。桌面/MCP-only agent 无官方路径成为可被 validate 接受的 roster 座席。
- 根因：roster 在 init 一次性固定且与 entry 烘焙耦合；assign 不校验 roster（rules.go:34 checkAssign 只查 owner≠reviewer），但 validate 校验（rules.go:238 "agent_id not in seat roster"）。
- 处置：本轮用 gemini-cli kind 占位声明 gemini-worker 进 roster。
- 候选改进：解耦 roster 声明与 entry 烘焙；或提供 `pactify roster add` 之类的后置加座席命令。

### #5 软链共享 entry 与 per-seat 烘焙不可兼容（self-hosting 最硬）
- 阶段：A0.2/A0.3
- 现象：pactify 仓用 `AGENTS.md → CLAUDE.md`、`GEMINI.md → CLAUDE.md` 软链让所有 agent 共享一份 dev 指令。init 给 3 个座席烘焙各自 seat 块时，软链文件被**替换成只含 pact 块的独立文件**（丢失 CLAUDE.md 的 dev 内容；AGENTS.md/GEMINI.md 不再是软链）。
- 根因：pact 要求每座席独立 entry（各含自己的 seat 块），与"多文件软链到同一文件"根本冲突；BakeEntry 遇软链替换为块-only 文件，未内联软链目标内容。
- 处置（本轮，A 就地）：init 前先把 AGENTS.md/GEMINI.md 由软链转为带 CLAUDE.md 内容的真实文件，再 init 追加各自 seat 块——保住 worker 的仓库上下文。软链约定为 self-hosting 必要代价而断开。
- 候选改进：BakeEntry 遇软链时内联目标内容再追加块（不丢 dev 上下文）；或 pact 支持"一个共享 entry 列出全部座席"。

### #3 init 对产品仓根目录足迹大
- 阶段：A0.3
- 现象：init --seat×3 一次落地 .pact/{PROJECT.md,STATE.yml,log.jsonl} + 烘焙 CLAUDE.md/AGENTS.md/GEMINI.md + 生成 .mcp.json/opencode.json/.gemini/（按 kind 连 MCP 配置一起做）。
- 处置：用户选 A（就地、接受足迹）。记录在案。

### #4 init 重跑清空 log.jsonl
- 阶段：A0.3（代码审）
- 现象：engine.go Init 调 `os.WriteFile(LogIn, nil, 0644)` 无条件创建/截断 log.jsonl——已有协调态的仓重跑 init 会清空日志。
- 处置：dogfood 中只 init 一次；记为风险。
- 候选改进：init 若 log.jsonl 非空应 fail-closed 或要 --force。

## 协议稳定性结论（持续更新）
- 待 worker 进场后补充。

### #6 orchestrator 缺自主观测，退化成人肉 bus
- 阶段：t1 派发后
- 现象：worker 启动后，orchestrator（claude）无内建机制感知 worker 何时 checkpoint，需用户口头传"干完了去 review"——人肉中继没消灭，只是从"传任务内容"退化成"传状态信号"。用户一针见血："还是我手动做这个 bus，那有什么意义？"
- 根因：orchestrator 角色是被动 CLI 驱动；协议靠 .pact/ 文件协调，但没有"orchestrator 监听 log.jsonl 状态变迁并自动接手"的回路。serve 有 watcher/SSE，但服务于 dashboard，未反哺 orchestrator 决策。
- 处置（本轮，运营层）：orchestrator 起后台轮询盯 .pact/log.jsonl 的 checkpoint 事件，worker checkpoint 即自动醒来 review，用户只当"启动按钮"。
- 候选改进（产品层）：① `pactify watch --seat claude --on-checkpoint <cmd>` 之类的 orchestrator 守护，状态变迁触发回调；② 正是 M3.4 relay 的延伸——事件流反哺编排端；③ orchestrator 也是 MCP 客户端，用 resource subscription 订阅 STATE 变更。**这条直接关系到协议"消灭人肉中继"的核心价值能否在 orchestrator 层兑现，优先级高。**

### #7 共享单工作树致 orchestrator 提交落错分支
- 阶段：t1 review
- 现象：我架完后台观测后提交 #6 stability log，commit 落到了 feat-relay 而非 main——因为此时 opencode worker 已把【共享工作树】切到 feat-relay。`git log main..feat-relay` 里混入了 orchestrator 的文档提交。
- 根因：F1 单工作树——orchestrator 与 worker 共用一个 git HEAD，worker 切分支后 orchestrator 的任何提交都落到 worker 的分支。
- 处置：本轮容忍（#6/#7/#8 文档随 feature merge 回 main）；记录危害。
- 候选改进：orchestrator 与 worker 各用 git worktree（F1 并发隔离，Phase 1 backlog 已列）；或 orchestrator 文档提交前显式确认分支。

### #8 checkpoint 的 CommitAll 无差别扫入工具垃圾
- 阶段：t1 review
- 现象：worker 交付里混入 `data/state.json`（内容 `{"nextAgent":"build","nextModel":{deepseek-v4-pro}}`——opencode 自己的会话状态文件），范围外。
- 根因：`pactify checkpoint` 调 `gitx.CommitAll`（engine.go:335，stages everything），把工作树里任何工具生成的文件都扫进 feature commit。worker 非故意。
- 处置：changes 退回要求 git rm + gitignore；记录。
- 候选改进：checkpoint 应限定暂存范围（如只 task spec 声明的路径 + 显式变更），或对意外文件告警；或 init 时为常见工具状态目录补 .gitignore。
