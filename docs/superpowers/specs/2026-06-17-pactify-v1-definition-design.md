# Pactify v1 产品定义 + 推广 — 设计 Spec

> **性质**：本文是 **v1 总纲 spec**（范围定义 + 子项目分解 + 排序 + 验收 + 执行模型）。
> SP1–SP4 各自再走独立 spec→plan→实现循环；SP1 先开。
> **日期**：2026-06-17 · **决策者**：用户 · **起草**：claude (opus-4.8)

## 1. 背景与目标

Pactify 已发布到 v0.5.2，功能丰富（协议 v1 + Go CLI + MCP + orchestrate 串/并行驱动 + planner +
配置体系 + 成本可观测 + audit 层 + 多项目 dashboard + custom-agent manifest + pactify.dev 文档站），
但**从未被一个外部真实项目验证过**——所有 dogfood 都在 pact-dogfood 玩具仓。

**v1 的本质不是加功能，而是「补关键缺口 → 收敛冻结 → 推广到真实项目」**：
- 划定一条稳如磐石、可被外部项目采用的核心链路；
- 补齐三个让产品「完整 + 可推广」的关键缺口；
- 以三个本地真实项目（跨 Go/TS/Python）的成功接入作为推广验收。

## 2. 范围边界

| | 内容 |
|---|---|
| **In（v1 必含）** | 现有全部已交付功能 + §3 三个关键缺口补齐 |
| **Out（推 v1.x）** | 自然语言驱动入口；新 agent 接入扩展（codex/goose/cursor/amp…）；ACP 协议方向；并行 worktree token 汇总 |

**决策记录**（brainstorm 2026-06-17）：v1 定位 = 「补关键缺口再冻结」；推广验收 = 「多项目铺开」；
实施顺序 = 「缺口先行，再 dogfood」；dogfood 三项目 = pactify(self) + OCRC + futu。

## 3. 三个关键缺口 → Deliverable

### ① 稳定性 + 安全收口（地基）
- **错误处理完整设计**：半成品续接策略（续 vs reset 从头）/ 放弃重试阈值与成本上限 / 分类恢复
  （「活干完只是 checkpoint 挂」vs「活没干完挂了」）。承接 backlog「错误处理设计」+ idle-timeout 已落地部分。
- **plan apply 事务化**：apply 中途某 assign 失败时回滚已 assign 的（当前非原子，需人工清理）。
- **post-merge STATE 滞后 bug**：liveview 观察到 merge 后工作树 STATE 显 shipped、HEAD 提交的 STATE.yml
  显 in_progress——查清 pact Merge 的 STATE 提交时序，修复。
- **有副作用 HTTP 操作的横切安全规范**：确认弹窗 + acting-seat 校验 + 沙箱/权限基线。
  这是 ② 所有写操作 UI 的共同地基；暴露公网（pactify.agentjoey.ai）后尤为必要。

### ② dashboard 驱动闭环（建在 ① 的安全规范上）
把 dashboard 从 observe-only + plan-review 升级为「浏览器里跑完整闭环」：
- **B3 Plan apply UI**：Plan 视图内编辑 owner/reviewer/deps + Apply（`POST .../plan/{feature}/apply`，事务化）+ Run。
- **B5 Run 面板**：启动 orchestrate（选 feature / 并发度）（`POST /api/projects/{id}/orchestrate`）。自然语言拆解部分**不在 v1**。
- **B6 Review Gate / 升级 UX**：Live 暂停态显「为什么停 / 看 diff / 批准 / 打回 / 接手 / 续跑」（resume endpoint）。
- **B7 Ship 按钮**：shipped 后 push / 开 PR（`POST /api/projects/{id}/finish` + 确认弹窗）。
- **B8 Sessions prune**：Ops 内每 agent prune 按钮（`sessions prune` + 确认）。

### ③ 新项目 onboarding 一键化（推广前置，与 ② 可并行）
- **setup 一键 apply**：Setup 视图「Apply」直接 init + wire（`POST /api/setup/apply`，mutate .pact 需确认）。
- **agent 绑定向导闭环**：扫描注册完 agent → 引导「选注册的 agent → 派座席 + 角色」，接通「注册完」到「能干活」（#1 UI 部分）。
- **「5 分钟接入」quickstart 文档**：外部项目从零到跑通 1 个任务的最短路径（装 → register agent → setup apply → plan → run → ship）。

## 4. 子项目分解 + 排序

```
SP1 稳定性内核 + 安全横切规范   ← 地基，② 依赖它
       │
       ├─→ SP2 dashboard 驱动闭环 (B3/B5/B6/B7/B8)
       └─→ SP3 onboarding 一键化            （SP2 ∥ SP3，互不依赖）
              │
              └─→ 质量债清理（CI Node24 升级 + goreleaser darwin 签名）  ← 冻结前必清
                     │
                     └─→ 🏁 v1 冻结（版本号 + 文档 + Version History）
                            │
                            └─→ SP4 dogfood 铺开
```

**两个判断（已经用户确认）**：
1. **质量债前置到冻结前**：goreleaser darwin arm64 签名无效（首跑 SIGKILL）直接影响 dogfood 项目装 pactify；
   CI Node20→24 影响可信度。两项在 §3 之外、冻结之前清掉。
2. **SP2 ∥ SP3**：onboarding 不依赖 dashboard 驱动，可并行。

**SP4 dogfood 顺序**：pactify(self，开发 §3 过程中顺带持续验证) → OCRC(TS) → futu(Python)。

## 5. 验收标准

### v1 冻结验收
1. 三缺口 deliverable 全部完成且**双绿**（`go test ./...` + web `vitest` + Playwright `e2e`）。
2. **pactify 自身** dashboard 能完成完整闭环：plan → apply → run → review-gate → ship，全程在浏览器里。
3. **install 在 Apple Silicon 干净可跑**（darwin 签名修复后，下载即跑，无 SIGKILL）。

### 推广验收
- **OCRC + futu + pactify 三项目**各自：install → register agent → setup apply → 跑通 ≥1 个真实 feature → ship。
- 跨 **Go / TS / Python** 通用性验证；接入过程暴露的问题逐一修掉（回灌 backlog / 各 SP）。

## 6. 执行模型（self-dogfood，用户指定 2026-06-17）

SP1–SP4 的实施本身用 Pactify 的 orchestrate 驱动（即 pactify 持续 self-dogfood）：

| 角色 | Agent | 职责 |
|---|---|---|
| Orchestrator + Reviewer + 高复杂度核心 | **claude (opus-4.8)** | 编排任务图、每棒独立验收、承接复杂/架构性功能（如错误处理设计、安全横切规范、review-gate resume 引擎） |
| 后端 worker | **opencode (deepseek-v4-pro)** | Go 后端任务（endpoint、事务化、STATE 修复、setup apply、sessions/finish 接线） |
| 前端 UI worker | **kimi (k2.7)** | React/TS UI 任务（B3/B5/B6/B7/B8 面板、确认弹窗组件、绑定向导、Setup Apply UI） |

- **人在环**：用户在 dashboard 关注进度，升级（escalation）时介入。
- worker 不能自标 accepted；只有 reviewer（claude）能 accept；feature 全 task accepted 才能 merge（两条铁律）。
- 每个 SP 独立 spec→plan→orchestrate 跑；跨语言 worker 模型 id 用各 CLI 完整配置 key（教训：kimi 须 `kimi-code/kimi-for-coding`）。

## 7. 风险与已知边界

- **deepseek-v4-pro 慢 + 偶挂**：简单棒 ~25min 且 checkpoint 前挂过一次；靠 idle-timeout + D2 巡检兜底，必要时换标准棒模型。
- **kimi/前端棒**：前端任务有 vitest + e2e 双绿硬门；UI 改动遵守画布工艺规约（节点位置两写入者、RF merge-by-id）。
- **SP4 外部项目特有摩擦**：OCRC/futu 的 agent 配置、entry 文件、MCP 形态可能与 pactify 不同，属预期内、按真实暴露修。
- **范围蠕变**：§2 Out 列表是硬边界；dogfood 暴露的非阻塞改进回灌 backlog，不塞进 v1。

## 8. 下一步

SP1（稳定性内核 + 安全横切规范）先走独立 brainstorm→spec→plan。本总纲不直接产出实现 plan。
