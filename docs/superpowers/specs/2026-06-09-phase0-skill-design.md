# Phase 0 — Pact 协议 Skill 设计

> 日期：2026-06-09 · 状态：Approved (brainstorm) · Owner：Joey
> 目标：用 Claude skill + bash 函数实现 pact 协议，dogfood 验证"消灭人肉中继"
> 上游：[ROADMAP.md](../../ROADMAP.md) Phase 0 · [architecture.md](../../architecture.md) · [ADR-001](../../decisions/ADR-001-open-core-boundary.md)

## 0. 目标与 Exit Gate

Phase 0 验证一个命题：**多 agent 在同一 repo 协作时，人能否从"上下文快递员"降级成"启动按钮"。**

**Exit Gate（过 → 进 Phase 1；不过 → pivot）：**
- ✅ 人全程只说"开始"，不传任务内容 / 上下文 / diff
- ✅ worker（opencode）仅靠 `git pull` + 自动读入口文件就知道"我是谁、干什么"
- ✅ 两条铁律真的拦住违规（worker 试图 accept 自己的 task → 被拒）
- ✅ 制造一次 crash（worker 中途被杀）→ 重启 join 同座位 → 续干成功

## 1. 交付物

1. **`.pact/` 骨架** — `PROJECT.md`（章程+座位表）+ `STATE.yml`（投影）+ `tasks/<id>.md` + `log.jsonl`（源）
2. **`.pact/bin/pact.sh`** — 7 动词 bash 函数（状态机实现）+ `log`/`validate` 辅助 + `--help` 自文档。**这是未来 Go CLI 的命令契约**，随 repo 走
3. **Claude skill（薄）** — Claude 端便利皮肤；删掉协议照常运转
4. **dogfood 跑通** — 用本协议驱动 Phase 1 第一个 CLI 任务的开发

### 1.1 `.pact/` vs `.agent/` 分工（不同海拔，共存）

| 目录 | 海拔 | 内容 | 谁读 |
|---|---|---|---|
| `.agent/` | Sprint/Roadmap 层 | CURRENT.md / BACKLOG / sprints | 人（高层进度看板）|
| `.pact/` | Task 执行层 | STATE.yml / tasks / log.jsonl | 多 agent（协议执行账本）|

Phase 0 **不强迁** `.agent/` → `.pact/`，避免双重任务追踪。将来 `.pact/` 成熟可吸收 `.agent/`，非 Phase 0 目标。

## 2. 状态模型

### 2.1 STATE.yml（Phase 0 最小 schema）

```yaml
project: pactify
working_tree_holder: null        # { seat, ts } 或 null
agents:                          # roster = 项目本地"总表"（自声明，非全局）
  - { id: claude-opus, roles: [orchestrator, reviewer] }
  - { id: opencode,    roles: [worker] }
features:
  - id: CLI-INIT
    branch: feat/cli-init
    status: in_progress          # planned|in_progress|awaiting_review|accepted|shipped
    tasks:
      - id: T1
        owner: opencode
        status: assigned          # todo|assigned|in_progress|awaiting_review|accepted|changes_requested
        spec: .pact/tasks/T1.md
        reviewer: claude-opus
        evidence: null
```

**task 状态机：** `todo → assigned → in_progress → awaiting_review →（accepted | changes_requested → in_progress）`
**feature 状态机：** `planned → in_progress → awaiting_review → accepted → shipped`

### 2.2 log.jsonl（append-only，事件总线雏形）

字段直接喂给 M1.1 正式 schema：

```jsonc
{"ts":"2026-06-09T10:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"assign","task_id":"T1","feature":"CLI-INIT","payload":{"owner":"opencode"}}
{"ts":"...","agent_id":"opencode","role":"worker","event_type":"checkpoint","task_id":"T1","payload":{"evidence":"237 tests green, build ok"}}
{"ts":"...","agent_id":"claude-opus","role":"reviewer","event_type":"accept","task_id":"T1","payload":{}}
```

`event_type` 枚举（Phase 0）：`init | assign | join | checkpoint | accept | changes_requested | merge`

`role` 字段**由动词派生，不单独传入**：`init/assign/merge → orchestrator`、`join/checkpoint → worker`、`accept/changes_requested → reviewer`。一个座位身兼多角色（如 claude-opus）时，调哪个动词就记哪个角色。只读动词（`status/log/validate`）不写 log。

### 2.3 写穿模型（Phase 0 务实简化）

每个 pact 函数同时：**先 append log（源）→ 后改 STATE.yml（投影）**。`pact_log --replay` 从 log 重建 STATE 并比对，验证"log 为源、STATE 为投影"不变式。

## 3. pact.sh 命令契约（= 未来 Go CLI 契约）

| 函数 | 角色 | 动作 |
|---|---|---|
| `pact_init` | orchestrator | 生成 `.pact/` 骨架 + 按座位表烘焙各 vendor 入口文件 |
| `pact_assign T1 --owner opencode --reviewer claude-opus` | orchestrator | task → assigned + log（要求 owner≠reviewer）|
| `pact_join opencode` | worker | 冷启动注册 + join 事件 |
| `pact_checkpoint T1 --evidence "..."` | worker | task → awaiting_review + 写证据 + log |
| `pact_accept T1` | reviewer | task → accepted + log |
| `pact_merge CLI-INIT` | orchestrator | `--no-ff` 合并 + feature → shipped + log |
| `pact_status` | 任何 | 读 STATE 打印 |

辅助：`pact_log [--replay]`、`pact_validate`、`pact.sh --help`（自文档）

### 3.1 两条铁律（the pact —— pact.sh 强制 + validate 复查）

1. **worker 不能自标 accepted** —— `pact_assign` 要求 `owner != reviewer`；`pact_accept` 要求 `$PACT_AGENT_ID == task.reviewer`
2. **未全 accepted 不能合并** —— `pact_merge` 要求 feature 下所有 task 都是 accepted，否则拒绝

## 4. agent 身份模型

### 4.1 agent_id = 项目内自声明的"角色座位（seat）"

不做跨厂商身份认证（那是 Team 层）。只做一件事：**在本 repo 内用名字保证职责分离。**

- **roster = per-repo 总表**：`STATE.agents[]`，agent `pact_join` 时自声明 id + roles，随 git 走
- **id = 稳定座位，不是进程/session**：
  - 一个 Claude session 兼 orchestrator+reviewer → 一个座位 `claude-opus`，roles `[orchestrator, reviewer]`
  - 想用两个 session 分别当 worker/reviewer → 两个座位（必须不同 id）
  - session 崩溃重启 → 重新 join 同座位，接着干（拉取式冷启动恢复）
- **不自动生成 per-session 唯一 id**（会破坏座位稳定性 → 重启接不回原任务）

### 4.2 规范化规则（pact.sh + validate 强制）

- id 必须是 slug：`^[a-z0-9][a-z0-9-]*$`
- 约定命名：`<vendor>[-<seat>]` —— `opencode` / `claude-opus`，同厂商多座位 `claude-worker` / `claude-reviewer`
- roster 内唯一：`pact_join` 拒绝重复 id 声明不同 roles
- log 中每个 `agent_id` 必须存在于 roster（`validate` 复查）

### 4.3 信任模型

Phase 0 = TOFU 自声明（trust-on-first-use）。pact 强制**一致性 + 职责分离**，不是密码学身份。认证推到 Team 层。

### 4.4 握手：agent 怎么拿到自己的 agent_id

**从自己的 vendor 入口文件拿，人不需手动告诉它。** 这正是"消灭人肉中继"的机制。

1. **`PROJECT.md` 声明座位表（唯一事实源）：**
   ```yaml
   seats:
     claude-opus: { roles: [orchestrator, reviewer], entry: CLAUDE.md }
     opencode:    { roles: [worker],                  entry: AGENTS.md }
   ```
2. **`pact_init` 按座位表烘焙各入口文件**——头部写好：
   ```bash
   # AGENTS.md 顶部（opencode 自动加载）
   export PACT_AGENT_ID=opencode
   source .pact/bin/pact.sh && pact_join opencode
   ```
3. **人说"开始"** → opencode 自动加载 AGENTS.md → 得知自己是 `opencode` → join → 读 STATE → 发现 owner=opencode 的 assigned task → 干活。全程人没传 id / 任务 / 上下文。

**fail-closed：** pact 函数检测 `PACT_AGENT_ID` 未设置 → 拒绝执行，报错指向"source 你的入口文件"。

## 5. 异常处理（crash 恢复）

核心：**因为 log 是源、STATE 是投影，几乎所有异常都收敛到"replay 重建"这一个恢复路径。**

| 异常场景 | 处理机制 |
|---|---|
| 写 STATE/log 写到一半 crash | log 先写、STATE 后写；STATE 用临时文件+原子 `mv`；log 用 `O_APPEND` 单行原子追加（<PIPE_BUF）→ 要么整步成功要么没发生 |
| STATE 与 log 不一致 | `pact_validate` 检测 `STATE != replay(log)` → `pact_log --replay` 重建。自愈，log 永远是真相 |
| worker 干到一半 crash（task 卡 in_progress）| 重启重新 `pact_join` 同座位 → 读 STATE → 看 `git status` 确认进度 → 续干。拉取式冷启动本身即恢复 |
| 两个 agent 同时 append log | `O_APPEND` 原子，行不交错；STATE 投影被覆盖也能 replay 修复 |
| 持锁 agent 崩了（`working_tree_holder` 泄漏）| 锁存 `{seat, ts}`；`validate` 标记超时锁 stale → orchestrator/人清除。**Phase 1 用 git worktree 隔离从根免锁** |

Phase 0 不需要事务/补偿逻辑。健壮性靠两个不变式：(a) log-first + 原子写，(b) 任何不一致都能 replay 重建。

## 6. 协议知识分布

**可移植协议知识必须住在 `.pact/`，绝不住在 Claude skill。** 否则非 Claude agent 瞎了——违反跨厂商中立红线。

| 层 | 住哪 | 谁读 | 内容 |
|---|---|---|---|
| 协议本体（可移植）| `.pact/PROJECT.md` + `pact.sh --help` | 任何 agent | 角色、状态机、两条铁律、7 动词、座位表 |
| vendor 入口（薄指针）| `CLAUDE.md` / `AGENTS.md` / `GEMINI.md` | 对应 agent 自动加载 | "你是 X 座位 + 启动命令" → 指向 `.pact/` |
| Claude skill（可选皮肤）| Claude skill | 仅 Claude | 便利层：扮角色判断、checkpoint 提醒话术 |

**验证标准：** 删掉 Claude skill，协议照常运转。`pact.sh --help` + `PROJECT.md` 必须自足。

## 7. dogfood 6 段实跑

对象：用本协议驱动 Phase 1 第一个 CLI 任务（如 `pactify init` 的 Go 实现）。
座位：claude-opus（orchestrator+reviewer）、opencode（worker）。

| 段 | 谁 | 动作 |
|---|---|---|
| ① 创建 | claude-opus | `pact_init` 生成 `.pact/` + 烘焙入口文件；写 `PROJECT.md` 座位表 |
| ② 编排 | claude-opus | 手写 `tasks/T1.md`（spec+plan+验收）→ `pact_assign T1 --owner opencode --reviewer claude-opus` |
| ③ 冷启动 | 人说"开始" → opencode | 自动读 `AGENTS.md` → `pact_join opencode` → 读 STATE 发现 T1 是我的 |
| ④ 实现 | opencode | 写 Go 代码 → `pact_checkpoint T1 --evidence "tests green, build ok"` |
| ⑤ 验收 | claude-opus | 读 diff + evidence + 跑验证 → `pact_accept T1` |
| ⑥ 合并 | claude-opus | `pact_merge CLI-INIT` → feature→shipped |

## 8. 范围外（Phase 0 不做，防 yak-shaving）

- ❌ Go CLI 本身（Phase 1 产物；Phase 0 只用协议**驱动**它的开发）
- ❌ MCP server / dashboard（Phase 1 M1.3）
- ❌ 密码学身份认证、推送式触发、云端（Team 层）
- ❌ STATE schema 正式化（Phase 0 跑出经验 schema，M1.1 再形式化）
- ❌ `.agent/` → `.pact/` 强迁移
