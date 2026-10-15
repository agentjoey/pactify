# Spec — Harness Subagents（把其他 coding agent harness 当 subagent 调用）

> Status: **DRAFT** · 2026-10-15 起草。方向已由 Human Owner 拍板（§0），本文到「方案、验证与分期」为止，**不含实现**。
> 现状描述为源码实读，标注 `文件:行号`（起草时 HEAD = `2950d17`）。标 **[待验证]** 的是起草时未实测的假设，
> Phase 0 的任务就是把它们逐条变成实测结论。

## 0. 定位与已拍板的决策（2026-10-15）

**一句话**：pactify 是 coding agent 的 **harness 路由器**——在 Claude Code 里一个 slash 命令，就把
codex / opencode / gemini(agy) / kimi … 作为 subagent 拉起来执行一个任务，体验接近原厂 subagent，
同时按任务在各家 harness 的能力与订阅额度之间做平衡。类比 `musistudio/claude-code-router`，
但路由的是 **harness**，不是模型。

| # | 决策 | 结论 |
|---|---|---|
| D1 | 产品归属 | **就是 pactify**。不另起品牌、不拆仓库 |
| D2 | 第一版 host | **只做 Claude Code** |
| D3 | 现有协议 / 看板 / 云端 | **降级**：冻结新投入，变成可选的「治理模式」；具体删减见 §8 |

**为什么第三方能做、而且只有第三方会做**：host 厂商不会让自家 harness 拉起竞品（商业上不允许）；
callee 厂商会做「别人的 host 调我」（`openai/codex-plugin-cc` 34.2k★ 已做 Claude→Codex），
但没人会做「任意 host × 任意 harness + 按成本/能力选路」这个中立层。
与 CCR 的结构差异：pactify 拉起的是**各家官方二进制、用各家官方登录**，不碰鉴权与代理，
每份订阅都在其条款内被使用——「把重活从 Claude 额度挪到 Codex 额度」本身就是合规的成本平衡。

## 1. 目标场景

```
（Claude Code 会话中）
> /pactify:codex 把 internal/stats 的 per-task LOC 归因改成按 commit SHA 计算，补回归测试
  ⎿ codex · gpt-5.x · workspace-write · 4m12s · 38k tok
    改了 3 个文件（+84 −21）· 测试已跑 ✅
    [codex 的终稿摘要……]
    续接：/pactify:codex --resume …   完整日志：~/.pactify/jobs/<id>/log
```

- 主线程的 Claude 只负责**转写任务 + 读回结果**，探索、编辑、跑测试都在 callee 里完成。
- 长任务转后台，完成时以 Claude Code 原生的后台任务通知回到主线程（§5.2）。
- 同一 repo 可以 `--resume` 续接上一次的 callee 会话。

## 2. 非目标（第一版）

- 不做 Claude Code 以外的 host（D2）。
- 不做模型代理 / 鉴权转发（那是 CCR 的地盘，且有条款风险）。
- 不做多 agent 编排、任务状态机、看板——这些属于降级后的「治理模式」（§8）。
- 不在 callee 里中转审批到 host 的审批 UI（P1 只做「只读 / 可写」两档预设，§5.4）。

## 3. 原则（实现不得违背）

- **P-1 官方二进制 + 官方登录**：只拉起用户本机已装、已登录的官方 CLI / server，不读写其凭据。
- **P-2 模型绑定要么兑现、要么明说**：请求的模型无法在所选传输上兑现时，**拒绝执行或在结果首行标红**，
  绝不静默降级。（这是 `[ACP-MODEL]` 那次 243 次静默失真的教训。）
- **P-3 权限语义统一**：用户只面对 `--read-only` / `--write` 两档，由 pactify 映射到各 harness 的原生参数。
- **P-4 结果有界**：回到 host 上下文的只有结果信封（§4.3），完整日志落盘给路径，不回灌整段过程。
- **P-5 host 侧开销最小**：转发 subagent 用最便宜的模型（haiku），只做一次 Bash 调用，不读代码、不轮询。

## 4. 架构

```
Claude Code 插件（plugins/pactify，薄皮）            pactify 运行时（Go，复用现有代码）
──────────────────────────────────────            ─────────────────────────────────────
commands/ codex.md opencode.md … sub.md            pactify sub run|status|result|cancel|wait|list
          jobs.md（status/result/cancel）            ├─ job store   ~/.pactify/jobs/<id>/
agents/   <harness>.md                              ├─ worker      detached 进程，持有 harness 会话
          (model: haiku, tools: Bash, 薄转发)        ├─ adapters    复用 internal/cockpit 的 Session
hooks/    SessionStart：检查 pactify 是否可用          └─ router      ~/.pactify/router.json（P2）
skills/   brief 写法（如何把任务转写成自足的 brief）
```

### 4.1 运行时 CLI 面

```
pactify sub run <harness> [--model M] [--read-only|--write] [--resume|--fresh]
                [--wait 540s] [--cwd DIR] [--json]  < brief
pactify sub wait <job>        # 阻塞到完成；给主线程 run_in_background 用（§5.2）
pactify sub status [<job>] | result <job> | cancel <job> | list
pactify sub doctor            # 哪些 harness 装了 / 登录了 / 能兑现模型绑定
```

`run` 的行为：起一个 **detached worker**（独立进程组）持有 harness 会话并写 job 状态；
前台 CLI 只是等 job 文件。`--wait` 到点仍未完成 → 返回「仍在运行 + job id」，worker 继续跑。
这样同时解决 Claude Code Bash 单次 10 分钟上限和 host 重启后找回 job。

### 4.2 Adapters：以 cockpit Session 为底，不用 orchestrate 的 Runner

| 选项 | 现状 | 结论 |
|---|---|---|
| `orchestrate.Runner` | 接口只返回 `error`（`internal/orchestrate/runner.go:81-83`），终稿从不提取；子进程 stdin/stdout 直连父进程（`runner_idle.go:108,121`） | 不用 |
| `cockpit.Session` | 统一事件流含文本/终稿/工具/用量（`internal/cockpit/backend.go:61-70`），带审批流与 `ThreadID()` 续接（`:88-95`）；codex app-server / claude bridge / ACP 三类后端都已实现 | **采用** |

需要补的（全是定点改动）：

- **codex 模型参数**：`StartOpts.Model` 字段已存在（`backend.go:12`），但 `threadStart` 只传 `cwd`
  （`internal/cockpit/codex.go:283`）。补 `model` 与 sandbox / approvalPolicy 参数。
- **ACP 模型参数**：opencode 复用 #55 的 `OPENCODE_CONFIG_CONTENT` 注入（`internal/orchestrate/acprunner.go:110-120`），
  需要搬进 cockpit 的 ACP 路径（serve 从不传 model，`internal/serve/cockpit.go:159`）。
- **实际模型回报**：从会话初始化事件里取后端实际使用的模型，与请求比对（P-2）。**[待验证]** 每个后端是否都回报。
  codex 已有依据：vendored schema（`codexschema/CODEX_VERSION` = 0.144.4）里 `ThreadStartResponse` 含
  `model` / `modelProvider` / `reasoningEffort` / `sandbox` / `approvalPolicy`；`ThreadStartParams` 可传
  `model` / `sandbox` / `approvalPolicy` / `developerInstructions` / `ephemeral`；`TurnStartParams` 可传 `effort` 与
  `outputSchema`（结构化终稿）。本机 codex 已是 0.162.1，Phase 0 需按当前版本复核。
- **终稿文本**：`item/completed` 的 agentMessage 事件目前不带正文（`codex.go:560-561` 只发 `Final:true`），
  终稿要由 delta 累加或从 raw item 中取。
- **命令行模式兜底**：agy 等没有 server 协议的 kind 才走命令行，且必须改成输出进缓冲区、不碰父进程 stdio。

### 4.3 结果信封（回到 host 的唯一内容）

```
status · harness · model(请求 → 实际，不一致标红) · 权限档 · 耗时 · tokens
改动：N 个文件（+a −b）+ diffstat 前 20 行（internal/diffstat 已有）
终稿：callee 最后一条消息，截断到 ~4 KB
续接：session/thread id · 完整日志路径
```

`--json` 输出同一内容的结构化形式，供 skill / hook 使用。

## 5. 关键设计点

### 5.1 上下文交接（体验成败的关键）

callee 冷启动，没有 host 的对话上下文。第一版做法：转发 subagent 按 skill 把用户请求转写成
**自足 brief**（目标、相关文件路径、约束、验收标准），callee 自己读 repo。
不传会话记录全文（成本高、各 harness 无通用导入格式；codex 有官方导入器，可作为 P2 的 codex 专属增强）。
**Phase 0 必须度量**：同一批任务，brief 质量是否足以让 callee 不需要回问就完成。

### 5.2 前台 / 后台与完成通知

- 小任务：转发 subagent 前台 `pactify sub run … --wait 540s`，结果直接成为 subagent 的返回。
- 大任务：`run` 立即返回 job id，主线程用 Bash `run_in_background` 跑 `pactify sub wait <job>`，
  进程退出即触发 Claude Code 原生的后台完成通知。**[待验证]** 在插件命令 / subagent 的上下文里这条链路是否成立。

### 5.3 隔离

默认在**当前工作树**运行（与 codex-plugin-cc 一致，最符合「像原生 subagent」的直觉），但：
- `--write` 且转后台时，**默认进 worktree**（`internal/gitx` 的 `AddWorktree` 已有），结果给 worktree 路径，
  由 host 决定合并还是丢弃——避开 host 与 callee 同时改同一棵树（`CommitAll` 同类问题）。
- **[待验证]** Claude Code subagent 的 `isolation: worktree` 能否让转发 subagent 的 Bash 落在该 worktree；
  若能，前台可写任务直接用原生隔离。

### 5.4 权限映射（P-3）

| 档位 | codex | opencode | gemini / agy | claude |
|---|---|---|---|---|
| `--read-only` | sandbox read-only | permission 全 deny 写 | approval-mode 默认 | permission-mode plan |
| `--write` | sandbox workspace-write | permission allow edit/bash | yolo | acceptEdits |

**[待验证]** 每一格的确切参数名与当前版本行为；表格本身就是 P1 的兼容性测试清单。

### 5.5 host 侧开销

转发 subagent 用 haiku、只做一次 Bash；host 读回的只有信封。Phase 0 度量
「host 额外 token / callee 完成的工作量」，确认挪走重活确实划算。

## 6. Phase 0 — 一周验证（只做 codex + opencode 两个 harness、前台 + 后台）

| 问题 | 通过标准 |
|---|---|
| Q1 brief 交接是否够用 | 10 个真实任务（取自本仓 backlog）中 ≥7 个 callee 一次完成、无需人工补上下文 |
| Q2 host 开销是否划算 | 每次委派 host 侧额外 token 中位数 < 10k |
| Q3 后台通知链路 | `run_in_background` + `sub wait` 在插件命令里能触发原生完成通知 |
| Q4 隔离继承 | 实测 `isolation: worktree` 下 Bash 的 cwd |
| Q5 模型兑现 | codex / opencode 请求的模型 = 实际模型（从事件中读出，非文档互证） |

任一 Q1–Q3 不过 → 回到本 spec 修改方案，不进 P1。

## 7. 分期

| 阶段 | 范围 | 量级 |
|---|---|---|
| P0 | §6 验证（codex、opencode） | ~1 周 |
| P1 | + gemini/agy、kimi、claude；`sub doctor`；job 管理全套；权限映射兼容性测试进 CI（扩展现有 `codex-schema.yml` 的思路） | Go ~2–3k 行 + 插件 ~10 个 md，2–3 周 |
| P2 | router：命令别名 → 默认 harness、按任务类型选路、额度/限流报错时的回退链（改造 `[FALLBACK-PAR]` 逻辑）、每次委派的成本与 token 统计 | ~1–1.5k 行，1–2 周 |
| P3（可选） | 治理模式：`sub run --task T` 记入 pact 账本，callee = worker，host = reviewer | 视降级后协议的形态再定 |

## 8. 降级（D3）的具体含义

| 资产 | 处理 | 何时 |
|---|---|---|
| 云端（`cloud/`、relay、fly app） | **退役**：不再部署，backlog 云端整节冻结；代码删除另行 PR | 立即冻结；删除待 P1 完成后单独决定 |
| web 看板 + `serve`（~20.7k TS + 6.5k Go） | **冻结**：不再加功能，只保证能构建；`cockpit` 包从 serve 中独立出来供 `sub` 使用 | 立即冻结；简化 / 删除待 P1 完成后单独决定 |
| pact 协议 + 账本 + `pact` 插件 | **保留为可选的治理模式**；不再作为产品主线叙事 | — |
| 单一规范账本 spec | **暂停在 WS-B**（已合入、flag 默认关，不影响现网行为） | 立即 |
| 绑在上述资产上的 backlog 条目（`[UI-GATE]`、`[CLOUD-DOWN]`、`[BACKLOG-LIST]`、`[RUNGUARD-DEAD]` 等） | 标记「随降级冻结」，不再排期 | 立即 |

删除类动作（云端代码、web、serve）都是大范围不可逆改动，**每一项单独提 PR、单独确认**，不在本 spec 的授权范围内。

## 9. 开放问题

1. **命令命名**：插件命令会带命名空间（`/pactify:codex`）。**[待验证]** 无冲突时能否用 `/codex` 短形式；
   若不能，是否把插件名取短（如 `sub` → `/sub:codex`）。
2. **与官方 codex 插件共存**：用户同时装了 `codex-plugin-cc` 时，`/pactify:codex` 是否改为委托给它，还是各走各的。
3. **README / 文档站叙事**何时切换到新定位——建议 P1 完成后。
