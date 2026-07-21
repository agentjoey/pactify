# Spec — Seat Identity（身份 = 工作副本属性，永不进追踪文件）

Status: **draft** · Created 2026-07-22 · Owner: claude (orchestrator)
Source: 2026-07-22 诊断复现——同仓两个 claude-code 座席（lead=orchestrator / reviewer）静默塌缩成 last-writer-wins。

## 1. Problem

pact 的座席身份被写进了**每仓单例的共享文件**。身份本应是"会话/工作副本"的属性，现在却成了"仓库"的属性——同仓第二个同 kind 座席接线时，第一个座席的身份被静默覆盖。

### 复现事实（2026-07-22，两条路径均触发，exit 0，零告警）

在已有 `CLAUDE.md` 的仓接入 `lead`（orchestrator）+ `reviewer` 两个 claude-code 座席（`init --seat …×2` 或 init 后 `agent add` 均可）：

| 产物 | 期望 | 实际 |
|---|---|---|
| `CLAUDE.md` 托管块 | 两座席各自可见 | 只剩 `reviewer`；`seat \`lead\`` 出现 0 次 |
| `.mcp.json` `env.PACT_AGENT_ID` | 按座席区分 | 从 `lead` 被覆盖为 `reviewer` |
| `.pact/` roster | — | 两座席都在（账本与接线**静默背离**） |
| `pactify doctor` | 报冲突 | 只查 binary/auth/transport，不报 |

**功能性断裂点在 `.mcp.json`，CLAUDE.md 只是表症**：该仓启动的任何 Claude Code 都读同一份 `.mcp.json` → 同一个 `PACT_AGENT_ID` → 只能是最后写入的座席。lead 从此无法以 orchestrator 身份连上 MCP。

### 病根定位（代码）

- `internal/pact/seat.go:50`：托管块只有**一对**无座席维度的 `pact:begin/end` 标记；`bakeBlock` 写前 `stripBlock` 抹掉已有整块再追加 → 同 entry 第二次写必然覆盖第一次。
- `internal/agent/briefing.go:92 WireAt` → `mergeServer`：pact server 键恒为 `"pact"`，`env.PACT_AGENT_ID` 单值 → 同 config 第二次 wire 覆盖身份。
- 跨 kind 普遍成立：gemini 的项目级 `.gemini/settings.json` 同构；**kimi 的 `~/.kimi-code/mcp.json` 是机器级单例，更糟**（2026-07-19 e2e F5 实测：全局写死的 `PACT_AGENT_ID=kimicode` 需要"座席同名迁就"才能绕过）。
- **第二病灶（同根）**：宿主 MCP 配置里写死的 env **盖过进程 env** → orchestrate driver 给 headless stint 注入的 `PACT_AGENT_ID` 失效（F5 的另一半）。身份钉死在配置里，既堵死多座席，也劫持了 driver 注入。

### 与 coordination-authority 的关系

coordination-authority 把两个隐式资源显式化了：「协调目标（项目）」从进程 cwd 解绑（P2 项目寻址）、「集成点（base）」从裸 git ref 升格为单写入车道。**座席身份是第三个隐式资源**——它被隐含在"仓里的配置文件"里。本 spec 把它归位到正确的宿主：工作副本 + 会话。

## 2. 产品原则（一句话）

> **座席身份是工作副本（checkout）/会话的属性，永远不写进被 git 追踪的文件；追踪层只描述 roster（这仓有哪些座席）与绑定方法。**

之后：同一个仓可以有任意多个同 kind 座席；谁是谁由"你在哪个工作树 / 你带什么 env 启动"决定；driver 注入的身份永远生效。

## 3. 设计

### 3.1 统一身份解析链（CLI 与 MCP 同链）

```
进程 env PACT_AGENT_ID   （最高——driver 注入、显式 export、legacy 配置注入都落在这层）
  > .pact/seat 文件      （工作副本默认身份，untracked 单行文件）
  > fail-fast            （具名错误：列出 roster + 指引 `pactify seat use <id>` 或 export）
```

- **`.pact/seat`**：单行 seat id。git 卫生：`.pact` 本身被 gitignore 的仓天然不追踪；**tracked-`.pact` 仓由写入方同步把 `.pact/seat` 写进 `.git/info/exclude`**（复用 coordination-authority P0a 的 `ensureRuntimeExcludedLocal` 通道），exclude 写入失败即响亮报错——身份文件被提交是必须堵死的事故。
- 新动词 **`pactify seat use <id>`**：校验 id 在 roster（不在则 fail-fast 列出 roster）→ 写文件 + exclude。**`pactify seat`**（无参）：打印当前解析结果与来源（`env` / `file` / `unresolved`）——排障入口。
- 实现落点：`internal/paths.AgentID()` 从"只读 env"改为走本链（paths 需拿到项目根：沿用现有 `PACT_DIR`/cwd 解析，seat 文件位于其下）。CLI 全部动词与 `pactify mcp` 自动统一，零逐点改造。

**关键性质：seat 文件使方案对宿主 env 行为不敏感。** 即便某宿主 spawn MCP server 时不透传启动 env，server 的 cwd/`PACT_DIR` 指向项目根，seat 文件照样兜住。因此 §4 的宿主实验只决定"env 直通是否额外可用"，**不 gating 方案成立性**。

### 3.2 追踪层去身份化（Wire/BakeEntry 重写，7 kind 全量）

- **入口托管块（CLAUDE.md/AGENTS.md/GEMINI.md）改为 seat-agnostic**：协议说明 + roster 表（各座席 id/roles）+ 绑定指引（`pactify seat use` / env / worktree 信条）。块内容与"哪个座席"无关 → 同 entry 多座席接线**幂等**，覆盖问题自然消失（单块单标记的机制不必动）。
- **配置去身份**：所有 kind 的 pact server 条目**不再写死 `env.PACT_AGENT_ID`**（`PACT_DIR`/`--project` 等寻址参数照旧）。身份由 3.1 链解析。宿主支持文档化 env 展开的（claude-code 已确认，见 §4-E1），可写显式透传 `"PACT_AGENT_ID": "${PACT_AGENT_ID:-}"` 保留 env 直通；展开为空时 3.1 链把空值视同缺席。其余宿主直接省略该 key。
- **全局配置 kind（kimi/claude-desktop/antigravity）**：全局 config 同样去身份。这类 kind 已有 `--project` chdir-root 机制（M2.1），server 落到项目根后 seat 文件即生效——顺带根治 F5 类机器级身份错位。
- 座席专属的补充 briefing（如需）走**不追踪载体**（如 `CLAUDE.local.md`，视 §4-E2 验证结果定），不回流托管块。

### 3.3 并发姿态 = worktree（与 D 组合）

- 信条写进 onboarding 文档："**同仓并发多座席 → 每座席一个 git worktree，每树各自 `pactify seat use`**。"入口/配置是 tracked 的没关系——它们已 seat-agnostic，所有树共享同一份恰好正确。
- **orchestrate driver 在创建 sandbox/parallel worktree 时播种 seat 文件**（写入即将启动的 stint 的座席 id）。这给了 headless 路径第二条身份通道：env 注入（现有）+ seat 文件（新），宿主吞 env 也不再致命。
- 交互式同树切换身份：重新 `seat use` 或 env 覆盖——但文档明确这是串行切换，**并发共树仍然不支持**（工作树本身就是单座席资源，`working_tree_holder` 语义不变）。

### 3.4 兼容与迁移

- **协议零改动**：log 事件、join 语义、STATE 渲染、protocol_version 全部不动。身份链纯属实现层。
- **legacy 配置继续工作**：旧配置里写死的 `PACT_AGENT_ID` 由宿主注入成进程 env → 命中 3.1 链最高优先级，行为与今天一致（含旧碰撞）。迁移是"变好"不是"必须"。
- **迁移动作**：`wire`/`agent add` 重写时自动剥离配置里的 `PACT_AGENT_ID`；`doctor` 新检查——配置中发现 config-pinned identity 时警告（"钉死身份：阻塞多座席，且覆盖 driver 注入；运行 `pactify agent add <kind>` 重新接线"）。
- **join 语义不变**：仍"恒取解析后的身份，不收 seat 参数"——3.1 链已覆盖交互 UX，无需重开 spoof 权衡（本地身份本就 advisory，强制归账户层）。

### 3.5 明确不做

- **每座席独立 MCP server（`pact-lead`/`pact-reviewer`）**：一个会话同时握两个座席的工具集，owner≠reviewer 的隔离精神被亲手拆掉。否决。
- **托管块按座席分区（`pact:begin:lead`）**：治表——身份仍在追踪文件里，worktree 间照样冲突。否决。
- **本地身份签名/防冒充**：本地 identity 是 advisory（shell 逃生舱一直存在）；enforcement 归 relay/账户层（H1/A2 线）。

## 4. 宿主行为验证（实施前实验清单，每项 ≤10 分钟）

结论回填本节。因 3.1 的 seat 文件兜底，E1/E3/E4 只影响"env 直通"这条优化路径的可用性，不 gating 方案。

- **E1 claude-code**（✅ 已查证 2026-07-22，官方文档 code.claude.com/docs/mcp.md）：
  - `${VAR}` / `${VAR:-default}` 展开：**文档确认支持**，作用于 `command`/`args`/`env`/`url`/`headers`。→ claude-code 可用**显式透传** `"PACT_AGENT_ID": "${PACT_AGENT_ID:-}"`（documented），不必赌未记载的继承行为；展开为空串时解析链须把空值视同缺席（落到 seat 文件）。
  - 省略 env key 时的继承行为：**文档未记载**（仅明确会额外注入 `CLAUDE_PROJECT_DIR`）→ 实施时用 fake-server dump env 实测一次；无论结果如何，展开透传 + seat 文件已双保险。
  - `--scope local` 配置存于 `~/.claude.json` 按项目路径分键、不进 git；worktree 独立性未明示（路径分键推断独立）。本设计不依赖它（选了 seat 文件），仅备注。
- **E2 `CLAUDE.local.md`**（✅ 已查证 2026-07-22，官方 memory.md）：**仍被自动加载、未弃用**，与 CLAUDE.md 同等对待，官方明确建议加入 `.gitignore`；`@path` import 是补充非替代。→ 座席专属补充 briefing 的载体成立。
- **E3 gemini-cli**：`.gemini/settings.json` 省略 env key → 继承行为。
- **E4 kimi-cli**：全局 `mcp.json` 省略 env key → 继承行为；server 进程 cwd 语义（决定 seat 文件可达性）。
- **E5 opencode / codex-cli**：同 E3/E4 逐项确认（codex TOML doc-only，只改文档模板）。

## 5. 分期

### P0 — 身份解析链 + seat 动词（不动 Wire，先立地基）
- `paths.AgentID()` 改三级链 + `.pact/seat` 读写 + exclude 卫生 + fail-fast 具名错误。
- `pactify seat use <id>` / `pactify seat`。
- 测试：链优先级（env 胜文件、文件胜空、全空 fail-fast 列 roster）、tracked-`.pact` 仓 exclude 生效、MCP 与 CLI 同链。
- 向后兼容验收：现有全部 bats/go 测试零改动通过（env 路径行为不变）。

### P1 — 追踪层去身份化（7 kind Wire/BakeEntry 重写 + 迁移）
- seat-agnostic 托管块模板 + roster 表渲染；`mergeServer` 剥离 `PACT_AGENT_ID`（写入时顺带清洗 legacy 键）。
- doctor legacy 警告；`docs/agent-onboarding.md`、plugin、官网 quickstart 同步。
- 验收：**本次诊断的复现脚本反转**——同仓 `lead`+`reviewer` 双 claude-code 座席接线后，入口块一份且 seat-agnostic、配置零身份、两个终端分别 `seat use` 后各自以正确身份走通 join→(assign/checkpoint)→accept。

### P2 — orchestrate 播种 + 无人闭环回归
- **✅ 播种 DONE**：`seedSeatIfIsolated`（launch 单漏斗，per-stint 写当前座席）——仅隔离 worktree（判据 `LedgerDir != "" && LedgerDir != Dir`，sandbox/parallel 都成立），in-place 用户树永不碰；写前 `EnsureExcluded(.pact/seat)`（用户仓 track .pact，否则 worker checkpoint `git add -A` 会把身份文件卷进 feature 分支）；env 注入仍是主通道，此为第二通道，两者皆 best-effort。机制 e2e 实证：隔离 worktree 无 env 解析出 `source: file`。
- **dogfood（待跑，brief 已交付）**：同仓双 claude-code 座席（lead 编排评审 + worker 同 kind）经 `pactify orchestrate` 全自主走通；kimi 全局配置去身份后重验 F5 场景（不再需要"座席同名迁就"）。

## 6. 风险

- **宿主 env 语义差异**：某宿主既不透传 env 又不落项目根 cwd → 该 kind 暂保留 config 身份并由 doctor 标注（预期不会发生，E1–E5 确认）。
- **seat 文件被提交**：exclude 写入失败必须 fail loud（不是 best-effort）；validate 可加"追踪文件中不得出现 `.pact/seat`"检查。
- **迁移期混合态**：一个仓 config 有身份、另一个树用 seat 文件 → env 注入优先级保证行为可预测；doctor 是唯一权威排障入口（`pactify seat` 显示来源正是为此）。
