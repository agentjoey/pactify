# 复盘：8 小时自主交付 12 功能（2026-06-14）

claude 自主交付 12 个需求，opencode 协同（真跑 4 个 feature）。全部 shipped 到 main 并推送 origin。本文记录 learnings 与可优化点。

## 一、有效的做法（保留）

1. **基础设施先行**。先建 per-agent 配置体系（P1：解硬编码的 model/权限/drivability），后续每个 orchestrate runner 启动都受益，且顺带打通了 gemini 模型 pin 的可配路径。"先建地基再盖楼"被本次再次验证。
2. **给被委派 agent 写"零上下文可做"的详细 spec**。给 opencode 的 3 个 spec（finishstep/sessions/recipe）都带了：精确文件清单、代码骨架、TDD 要求、验收命令、边界。结果 3 个全部一次性（或一轮返工内）成功、无挂死。**spec 详细度 ≈ 委派成功率**。这直接印证了 #11 配方的价值——把 spec 模板化。
3. **claude(worktree) + opencode(主树) 并行**。opencode 在主树跑 orchestrate（占用 base 分支），claude 在隔离 worktree 开发，互不碰撞；merge 串行回 main。这套"人/agent 并行 + 串行 merge"实战跑通了 5 次。
4. **flake 先对基线验证再下结论**。office-zoom e2e 失败时，先 stash 自己的改动在干净 main 上复现，确认是既有 flake 而非回归。避免了误判。
5. **每个 feature 独立分支 + TDD + 合并门**。Go test/vitest/tsc/e2e 多重门；每 feature 单独 commit + `--no-ff` merge，历史清晰、可回退。

## 二、踩的坑与 learnings

1. **二进制陈旧**：提交 P2（加 `--idle-timeout`）后没重装二进制，第一次 orchestrate 直接 `unknown flag: --idle-timeout` 失败。→ **依赖新 flag 的 orchestrate 前必须 `go build -o $(which pactify)`**。可优化：`make install` + orchestrate 启动时 version/flag 自检。
2. **pact.Merge 的 base 争用**：Merge 内部 `git checkout base`，而主树占用 base → worktree 并行 merge 必然冲突。这逼出了 P5 的"park 主树 + worktree 串行 merge"架构。learning：**base 分支同一时刻只能一个 worktree 持有，merge 必须串行**。
3. **并行 merge 撞 ledger 投影文件**：两 feature 从同一 base 独立追加 log.jsonl/STATE.yml，第二个 merge 冲突。关键洞察：**StateProjection 从 log.jsonl 重算，STATE.yml 只是缓存**——所以 `.gitattributes` 给 log.jsonl/STATE.yml 设 `merge=union` 即可（log union=全部事件正确；STATE 变 garbage 但无所谓）。这值得写进架构文档。
4. **pact.Merge 不提交 merge 事件**：它把 merge 事件 append 到工作树但不 commit（串行流程靠读工作树文件够用）。并行流程 RemoveWorktree 会丢掉它 → feature merge 了代码却不显 shipped。→ 并行 merge 后必须显式 commit merge 事件。
5. **join 冷启动被未来 dep 误杀（既有 bug）**：复用 opencode-worker（跨多 feature owner）时 join 可能 choke。本次用**全新专用座席（oc-finish/oc-sessions/oc-recipe）**绕过。bug 仍在 backlog。
6. **worktree 缺 node_modules**：web worktree 跑 vitest 前要软链主树 node_modules。可优化：worktree bootstrap 脚本。
7. **无成本/可观测**：整个 8h 没有 token/花费追踪，复盘时报不出精确消耗。印证 backlog 的 cost/observability 真实需要。

## 三、可优化点（actionable，已挂 backlog）

| 优化 | 说明 | 优先级 |
|------|------|--------|
| `make install` + orchestrate flag 自检 | 防二进制陈旧 | 高 |
| 修 join 冷启动被未来 dep 误杀 | 既有 bug，影响多 feature 座席 | 高 |
| 把 #11 配方做成"委派 spec 模板"引擎 | spec 详细度=委派成功率，模板化降门槛 | 高 |
| 产品化"人 + agent 并行（park+worktree）" | 本次手动跑了 5 次，可做成 `orchestrate --with-human` 或文档化 | 中 |
| idle-timeout 调优 + 设为主超时手段 | 本次未触发挂死（spec 好），但仍是挂死正解 | 中 |
| 成本/可观测（每棒 token+花费 + 运行历史） | 8h 无成本可见 | 中 |
| worktree bootstrap 脚本（node_modules 等） | 减少 worktree 启动摩擦 | 低 |
| 架构文档补"并行 merge = union 驱动 + 重算投影" | 关键洞察未入文档 | 中 |

## 四、协作数据

- **opencode 真跑交付 4 feature**：finishstep、sessions、recipe（+ 每个都经 claude 独立评审：重跑验收命令、查越界）。全程 orchestrate 自动驱动，无人工传话。
- **claude 交付 8 feature**：配置体系、错误处理、并行引擎、向导、UI polish、planner review UI 等全部复杂核心 + UI。
- **零升级（escalation）**：12 个 feature 没有一个需要人工介入解困——spec 质量 + idle-timeout 护航 + 专用座席避坑的综合结果。
