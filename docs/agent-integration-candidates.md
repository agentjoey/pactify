# Agent 接入候选调研（2026-06-14）

为 Pactify 扩展可接入 agent 的调研。**所有结论联网核实**（官方文档 + GitHub），区分 VERIFIED / UNCERTAIN。源自 4 个并行调研 agent 的报告（cursor/windsurf、kimi/hermes、openclaw/aider/amp）+ 生态盘点。

## Pactify 接入模型（每个 agent 要确定的 5 要素）

源自 `internal/agent/agent.go` 的 `spec` struct + `runnerProfiles`：
1. **entry 文件**：agent 读哪个项目指令文件（CLAUDE.md / AGENTS.md / GEMINI.md …）。Pactify 把协议 onboarding 块烘焙进去。
2. **MCP 配置**：能否挂 `pact` MCP server？文件路径 + 格式（JSON `mcpServers`{} / opencode `mcp`{} / TOML / YAML）+ scope（Project repo 内 / Global 机器级）。
3. **headless runner**：一次性跑 prompt 的非交互命令 + 自动批准工具调用的 flag。决定能否被 orchestrate **自动驱动**。
4. **CLI / GUI**：纯 GUI 无法 headless（只能人工/经 escalation 交接）。
5. **检测二进制名**：PATH 检测用。

## 接入候选矩阵

| Agent | CLI/GUI | headless 命令（自动批准） | MCP 格式 / scope | entry | binary | 座席 | runner | 优先级 |
|-------|---------|--------------------------|------------------|-------|--------|------|--------|--------|
| **Kimi Code CLI** | CLI/TUI | `kimi -p "…"`（默认 auto）+ `--output-format stream-json` | JSON `mcpServers`，`.kimi-code/mcp.json`(proj)+`~/.kimi-code/mcp.json` | `AGENTS.md` | `kimi` | ✅ GO | ✅ GO | **一等** |
| **Cursor CLI** | CLI | `cursor-agent -p --force --output-format stream-json "…"` | JSON `mcpServers`，`.cursor/mcp.json`(proj)+`~/.cursor/mcp.json` | `AGENTS.md`/`.cursor/rules` | `cursor-agent` | ✅ GO | ✅ GO | **一等** |
| **Amp**（Sourcegraph）| CLI+IDE | `amp -x "…"` + `AMP_API_KEY` + `--stream-json` | JSON `amp.mcpServers`，`.amp/settings.json`(proj)+`~/.config/amp/settings.json` | `AGENTS.md`(fallback CLAUDE.md) | `amp` | ✅ GO | ✅ GO（workspace MCP 需预批准）| **一等** |
| **Devin CLI**（原 Windsurf）| CLI+GUI | `devin -p --permission-mode bypass "…"` + `COGNITION_API_KEY` | JSON `mcpServers`，`.devin/config.json`(proj)+`~/.config/devin/config.json` | `AGENTS.md`/`.devin/rules` | `devin` | ✅ GO | ⚠️ GO*（纯 JSON 输出 flag 待实测）| 二等 |
| **Aider** | CLI | `aider --message "…" --yes` | ❌ 无原生 MCP（issue #4506 open；需 `mcpm-aider` 外挂）| `CONVENTIONS.md`(via `--read`) | `aider` | ✅ GO | ✅ GO（无 MCP 则协议走 entry/shell fallback）| 二等 |
| **Hermes Agent**（Nous）| CLI/TUI+GUI | `hermes -z "…"` + `--yolo` | ⚠️ YAML `mcp_servers`，`~/.hermes/config.yaml`（**仅 global**）| ⚠️ 不明确 | `hermes` | ⚠️ 有条件（需 config/entry adapter）| ✅ GO | 三等 |
| **OpenClaw** | CLI+GUI | ❌ 无可验证一次性执行 | ⚠️ 仅 Windows Hub "local MCP"，无标准客户端配置 | ❌ 无 | `openclaw` | ❌ NO-GO | ❌ NO-GO | 不接 |

\* Devin CLI 的 `-p` + `--permission-mode bypass` + 纯 JSON 输出能否组合需实测 `devin --help`。

## 关键发现

1. **`AGENTS.md` 是事实标准**：Kimi/Cursor/Amp/Devin/opencode/codex 都读 `AGENTS.md`。Pactify 对这些 kind 用 `AGENTS.md` entry 一把通吃，MCP 都是 project 级 JSON `mcpServers`——**接入成本极低**（几乎照搬 claude-code/opencode 的 spec）。
2. **headless 自动批准 flag 各异**，orchestrate runner 需按 kind 映射（已有 `PermPosture` 抽象可扩展）：
   - blanket：claude `--dangerously-skip-permissions` / gemini `--approval-mode yolo` / cursor `--force` / devin `--permission-mode bypass` / kimi（`-p` 默认即 auto）/ aider `--yes` / amp（`AMP_API_KEY` 环境）/ hermes `--yolo`
   - 这正好印证了 P1 把权限姿态参数化的价值——新 kind 只需在 `runnerProfiles` 加一个 builder。
3. **stream-json 输出**：Kimi/Cursor/Amp/Devin 都支持结构化输出 → 未来可解析真实模型/token（接 backlog 的成本可观测 + 解决 gemini 静默降级的"可见"方案）。
4. **Windsurf 认知过时**：已被 Cognition 收购更名 Devin Desktop（2026-06-02），且现在带 headless 的 Devin CLI——旧 backlog"antigravity/Windsurf 纯 GUI 不可驱动"对 Windsurf 部分需更新。
5. **Hermes 是异类**：MCP 用 YAML `mcp_servers`(下划线) + 全局级，与 Pactify 的 JSON `mcpServers`/项目级不同构 → 需要一层 config adapter（新增一个 Format 枚举值 `YAMLMcpServers` + Global scope）。
6. **MCP 非硬门槛**：Aider 无原生 MCP 但仍 GO——Pactify 的 shell fallback（`pactify join/checkpoint/...`）让任何能读文件 + 跑 shell 的 agent 都能参与，MCP 只是更顺滑的路径。

## 接入实施建议（对 agent.go 的具体改动）

### 第一批（一等，几乎照搬现有 pattern，建议优先接）
新增 3 个 kind 到 `registry` + `runnerProfiles`：
- **`kimi-cli`**：entry `AGENTS.md`，cfg `.kimi-code/mcp.json` JSONMcpServers Project，detectBin `kimi`，runner `kimi -p {briefing} -m {model}`（model 默认 `kimi-k2`，无需额外 yolo flag）。
- **`cursor-cli`**：entry `AGENTS.md`，cfg `.cursor/mcp.json` JSONMcpServers Project，detectBin `cursor-agent`，runner blanket=`cursor-agent -p --force {briefing}`，scoped 可用 cursor 的工具白名单（待查）。
- **`amp`**：entry `AGENTS.md`，cfg `.amp/settings.json`（注意 key 是 `amp.mcpServers` 嵌套，需新 Format 或写法适配），detectBin `amp`，runner `amp -x {briefing}`（鉴权靠 `AMP_API_KEY` 环境，Keychain 取）。

### 第二批（二等，需小适配）
- **`devin-cli`**：先实测 `devin --help` 确认 headless+JSON 组合，再加；MCP `.devin/config.json` JSONMcpServers Project。
- **`aider`**：无 MCP，走 entry(`CONVENTIONS.md`) + shell fallback；runner `aider --message {briefing} --yes`。新增"无 MCP，仅 entry"的接入路径。

### 第三批（需较大适配 / 谨慎）
- **`hermes`**：需新增 `YAMLMcpServers` Format + Global scope 的 MCP 写入逻辑；entry 约定不明需实测。runner `hermes -z {briefing} --yolo`（注意 memory 副作用，用隔离 home）。

### 不接
- **`openclaw`**：消息网关/个人助手，非仓库内 coding 座席。

---

## 生态盘点（第二批调研：另 7 个 agent）

| Agent | headless 命令（自动批准） | MCP 格式 / scope | 配置 | binary | 座席 | runner | 优先 |
|-------|--------------------------|------------------|------|--------|------|--------|------|
| **Goose**（Block）| `goose run -t "…"` + `GOOSE_MODE=auto`（默认）+ `--output-format json` | MCP-native（extensions，stdio）| `~/.config/goose/config.yaml`(YAML) | `goose` | ✅ | ✅ | **P0** |
| **Codex**（OpenAI）| `codex exec "…"` + `--sandbox workspace-write`（或 `--dangerously-bypass-approvals-and-sandbox`）+ `--json` | TOML `[mcp_servers.*]` | `.codex/config.toml`(proj)+`~/.codex/config.toml` | `codex` | ✅ | ✅ | **P0** |
| **Continue** | `cn -p "…"` + `--auto` | `config.yaml` | `~/.continue/config.yaml`(YAML) | `cn` | ✅ | ✅ | P1 |
| **Cline** | `cline -y --json "…"` | JSON `mcpServers` | `~/.cline/mcp.json` | `cline` | ✅ | ✅ | P1 |
| **Crush**（Charm）| `crush run "…"` + `--yolo`（位置存疑，建议用 `permissions.allowed_tools` 白名单）| JSON `mcp` 块 | `crush.json`(proj)+`~/.config/crush/crush.json` | `crush` | ✅ | ✅ | P2 |
| **Q Developer**（AWS）| `q chat --no-interactive --trust-all-tools "…"`（trust 有已知 bug，约 50% 生效需重试）| JSON `mcpServers` | `.amazonq/mcp.json`+`~/.aws/amazonq/mcp.json` | `q` | ✅ | ⚠️ | P3 |
| **Zed** | ❌ 内置 agent 无 headless（CLI 只开文件）| `context_servers`(settings.json) | `settings.json` | `zed` | ✅（挂 pact context server）| ❌ | 座席only |

### 生态盘点要点
1. **Codex runner 现可启用（VERIFIED）**：Pactify 已列 `codex-cli`/`codex-app` 两 kind 但 runner 留空（当时 headless 未验证）。本次确认 **`codex exec "…" --sandbox workspace-write`** 是成熟 headless 路径（管道 + JSONL + 沙箱分级）→ **可以给 codex-cli 补上 runnerProfile，标 drivable**。这是最低成本的一个增量（kind 已在，只差 runner）。
2. **Goose 是最干净的编排目标**：`GOOSE_MODE=auto` 是稳定环境变量（无 flag 歧义）、MCP-native，建议与 Codex 并列 P0。
3. **多个 agent 改了认知**：Cline（CLI 2.0 后可驱动，非 GUI-only）、Continue（有 `cn` CLI，flag 是 `-p` 非 `--print`、无 `-y`）。
4. **Zed 反向机会**：Zed 不暴露自身 agent 当 runner，但若 **Pactify 自身说 ACP（Agent Client Protocol）**，Zed/JetBrains/Kimi 都能 host Pactify 驱动的 agent——这是一个协议层扩展方向，值得单列调研。
5. **沙箱/权限的新维度**：Codex 的 `--sandbox read-only|workspace-write|danger-full-access` 比 blanket/scoped 更细——P1 的 `PermPosture` 未来可加一个"sandbox 级别"维度。

### 更新后的接入优先级（合并两批）
- **第一梯队（drivable runner，几乎照搬现有 pattern）**：**Codex**（kind 已在，只补 runner）、**Goose**、**Kimi Code**、**Cursor CLI**、**Amp**。
- **第二梯队（小适配）**：**Continue**、**Cline**、**Crush**、**Devin CLI**（待实测）、**Aider**（无 MCP，走 entry+shell fallback）。
- **第三梯队（需较大适配/谨慎）**：**Hermes**（YAML/global MCP adapter）、**Q Developer**（trust bug 需重试包裹）。
- **仅座席 / 人工交接**：**Zed**、各 desktop/GUI kind。
- **不接**：**OpenClaw**。

### 待实测项（接 runner 前必须装上跑 `--help` 核实，禁止凭文档断言）
- Crush：`--yolo` 在 `run` 子命令还是 root。
- Q：`--no-interactive` vs `--non-interactive` 拼写 + trust bug 是否已修。
- Devin CLI：`-p` + `--permission-mode bypass` + JSON 输出能否组合。
- Cursor：headless 内 `--model` flag。
- Codex：是否仍有 `codex mcp-server`（Codex 作为 MCP server）模式。

### 配套
- 新增 kind 后，`agent config`（P1）的 model/权限姿态对它们即时可用；`setup suggest`（#1）的 lead 启发式可纳入（claude 系仍 lead，新 kind 默认 worker）。
- 每个新 runner 的自动批准 flag 实测验证后再标 drivable（参照当时 claude/gemini/opencode 的实测纪律——禁止凭文档断言就开 runner）。
