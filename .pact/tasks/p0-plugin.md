# Task p0-plugin

**Feature:** p0-plugin · **Owner:** codex-worker · **Reviewer:** claude-session · **Linear:** AJ-617

## worker 须知（必读）
- 只改「Files」列出的文件；需要改别的文件时，在 checkpoint evidence 里说明原因。
- 跑任何 Go 测试都带 `PACTIFY_HOME="$(mktemp -d)"`；不要运行会写入真实 `~/.pactify` 或 `~/.kimi-code` 的命令（真机探测步骤只读 `~/.kimi-code`）。
- 不要改 CLAUDE.md / AGENTS.md / GEMINI.md；不要提交临时探测程序或生成物。
- 严格按 TDD：先写测试并看它失败，再实现，再看它通过。
- 完成后用 pact checkpoint 提交，evidence 写：verify 命令的完整输出摘要 + 新增测试名列表 + 真机探测结果（若本任务有该步骤）。
- 设计以本文件的「Interfaces」为准；与仓库现状冲突时以本文件为准，并在 evidence 里写明冲突点。
- 背景：这是 pactify 的 harness 路由器 Phase 0（spec `docs/specs/harness-subagents.md`，完整计划 `docs/specs/harness-subagents-phase0-plan.md`；这两个文件可能还不在你的分支上，本文件已包含你需要的全部内容）。

## Global Constraints

- 只拉起用户本机已装、已登录的**官方** CLI；不读写其凭据，不做模型代理（spec P-1）。
- 请求的模型无法兑现时**拒绝执行并给出可选模型**，绝不静默降级（P-2）。
- 用户只面对 `--read-only` / `--write` 两档；映射：codex `sandbox: read-only` / `workspace-write` 且 `approvalPolicy: never`；kimi `session/set_mode plan` / 默认模式（P-3、§5.4）。
- 回到 Claude 的只有结果摘要；最终回复截断到 **4096 字节**；完整日志落盘给路径（P-4）。
- 转发 subagent 用 `model: haiku`、`tools: Bash`，只调一次 Bash（P-5）。
- 每个 job 记录 callee 的 CLI 版本（P-7）。
- **`AccessDefault`（空值）必须保持 serve 现有行为逐字节不变**：本计划对 `internal/cockpit` 的改动只在调用方显式传 `Access`、`Model`、`Effort`、`Env`、`Stderr` 时生效。
- 任何测试不得写开发者真实的 `~/.pactify` 或 `~/.kimi-code`：Go 测试一律用 `t.TempDir()` 作 `PACTIFY_HOME` 与 kimi home；验收命令统一以 `PACTIFY_HOME="$(mktemp -d)"` 运行。
- 被调用的 agent 不提交代码；改动留在工作区，由 host 决定（spec §5.3）。
- job 目录：`$PACTIFY_HOME/jobs/<job-id>/`（`PACTIFY_HOME` 未设时为 `~/.pactify`）。job id 格式 `YYYYMMDD-HHMMSS-<6 位小写十六进制>`。

### Task 7: Claude Code 插件最小版

**Files:**
- Create: `plugins/pactify/.claude-plugin/plugin.json`
- Create: `plugins/pactify/skills/codex/SKILL.md`、`plugins/pactify/skills/kimi/SKILL.md`
- Create: `plugins/pactify/agents/codex.md`、`plugins/pactify/agents/kimi.md`
- Create: `plugins/pactify/hooks/hooks.json`、`plugins/pactify/hooks/check-pactify-sub.sh`
- Create: `plugins/pactify/README.md`
- Modify: `.claude-plugin/marketplace.json`
- Test: `tests/plugin_pactify.bats`

**Interfaces:**
- Consumes: Task 6 的 CLI（`pactify sub run/wait/result`、退出码 0/1/2/3、`--json` 输出含 `jobId`）。
- Produces: `/codex <任务>`、`/kimi <任务>` 两个入口；插件名 `pactify`（`/pactify:codex` 始终可用）。

- [ ] **Step 1: 写失败测试（bats）**

```bash
#!/usr/bin/env bats
REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"
P="$REPO/plugins/pactify"

@test "marketplace lists the pactify plugin" {
  run python3 -c "import json;d=json.load(open('$REPO/.claude-plugin/marketplace.json'));print([p['name'] for p in d['plugins']])"
  [ "$status" -eq 0 ]
  [[ "$output" == *"'pactify'"* ]]
}

@test "plugin.json is valid and named pactify" {
  run python3 -c "import json;print(json.load(open('$P/.claude-plugin/plugin.json'))['name'])"
  [ "$output" = "pactify" ]
}

@test "forwarding agents use haiku and only Bash" {
  for h in codex kimi; do
    grep -q '^model: haiku$' "$P/agents/$h.md"
    grep -q '^tools: Bash$' "$P/agents/$h.md"
    grep -q "pactify sub run $h" "$P/agents/$h.md"
  done
}

@test "skills route large tasks and wait on the main thread" {
  for h in codex kimi; do
    grep -q '^name: '"$h"'$' "$P/skills/$h/SKILL.md"
    grep -q 'run_in_background' "$P/skills/$h/SKILL.md"
    grep -q 'pactify sub wait' "$P/skills/$h/SKILL.md"
  done
}

@test "session-start hook reports a missing pactify without failing the session" {
  run env PATH=/usr/bin:/bin bash "$P/hooks/check-pactify-sub.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"pactify"* ]]
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `bats tests/plugin_pactify.bats`
Expected: 全部 FAIL（文件不存在）。

- [ ] **Step 3: 写插件文件**

`plugins/pactify/.claude-plugin/plugin.json`：
```json
{
  "name": "pactify",
  "description": "Delegate large coding tasks from Claude Code to other coding agent harnesses (codex, kimi) as subagents, with verified model and cost.",
  "version": "0.1.0",
  "author": { "name": "agentjoey" },
  "homepage": "https://github.com/agentjoey/pactify",
  "repository": "https://github.com/agentjoey/pactify",
  "license": "MIT"
}
```

`plugins/pactify/agents/codex.md`（kimi.md 相同，把 codex 换成 kimi、模型示例换成 `kimi-code/kimi-for-coding`）：
```markdown
---
name: codex
description: Forward one large, self-contained coding task to the codex harness via `pactify sub`. Use only when the main thread has already written a self-contained brief.
model: haiku
tools: Bash
---

You are a thin forwarding wrapper. Your only job is one Bash call:

    pactify sub run codex <flags> --json <<'BRIEF'
    <the brief you were given, verbatim>
    BRIEF

Flags: add `--read-only` if the brief says the task must not modify files, otherwise `--write`.
Add `--model <name>` only if the brief names a model. Add `--resume` only if the brief says to continue
the previous codex session in this repository. Add `--wait 540s`.

Do not read files, search, plan, or summarize. Return the command's stdout verbatim. If the exit code is 3,
return the stdout verbatim as well: it contains the job id that the main thread will wait on.
```

`plugins/pactify/skills/codex/SKILL.md`（kimi 同构；kimi 版另加一段「只读任务：kimi 用 plan 模式，结果首行若出现『工具调用超限』，说明 kimi 在只读模式下没收敛，改用 codex 或改成可写任务」）：
```markdown
---
name: codex
description: Delegate a large coding task (multi-file change, investigation, or anything that needs running tests) to the codex harness and get back a verified result summary. Not for small edits you can do directly.
---

# Delegate to codex

Use this only for **large** tasks: touching 3+ files, or needing exploration and test runs, or likely to take
you more than ~10 minutes. Small edits: do them yourself — each delegation carries a fixed cost of tens of
thousands of tokens on the callee side.

1. Write a self-contained brief. The callee starts cold and cannot see this conversation. Include:
   goal; relevant file paths; constraints (what must not change); acceptance criteria (exact commands to run);
   whether it is read-only.
2. Start it in the background from the **main thread** (not a subagent — a subagent's background command
   notifies only that subagent):

       pactify sub run codex --write --wait 0s --json <<'BRIEF'
       ...brief...
       BRIEF

   It exits 3 immediately with `{"jobId": ...}`. Then run, with `run_in_background: true`:

       pactify sub wait <jobId>

3. When the background command completes, read its output: the result summary. The first line carries any
   warning (model mismatch, tool-call limit, idle timeout). Review the diff it lists before building on it.
4. To continue the same codex session in this repository, pass `--resume` on the next `run`.

For a quick task you are willing to block on (under ~9 minutes), you may instead delegate to the `codex`
subagent, which calls `pactify sub run codex --wait 540s` once and returns the summary.
```

`plugins/pactify/hooks/hooks.json`：
```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/check-pactify-sub.sh" } ] }
    ]
  }
}
```

`plugins/pactify/hooks/check-pactify-sub.sh`（可执行）：
```bash
#!/usr/bin/env bash
# Report whether the pactify CLI with the `sub` command is available. Never fail the session.
if ! command -v pactify >/dev/null 2>&1; then
  echo "pactify plugin: the pactify CLI is not on PATH; /codex and /kimi will not work. Install: go install github.com/agentjoey/pactify/cmd/pactify@latest"
  exit 0
fi
if ! pactify sub --help >/dev/null 2>&1; then
  echo "pactify plugin: the installed pactify has no 'sub' command; upgrade pactify."
  exit 0
fi
exit 0
```

`.claude-plugin/marketplace.json` 的 `plugins` 数组追加：
```json
{ "name": "pactify", "source": "./plugins/pactify", "description": "Delegate large tasks to codex / kimi as subagents with verified model and cost." }
```

`plugins/pactify/README.md`：安装（`/plugin marketplace add agentjoey/pactify` → `/plugin install pactify@pactify`）、前提（`pactify` 在 PATH 上、codex / kimi 已登录）、两个入口的用法、结果摘要字段说明、已知限制（kimi 只读依赖 plan 模式；callee 每次委派有 2–5 万 token 固定开销）。

- [ ] **Step 4: 运行，确认通过**

Run: `bats tests/plugin_pactify.bats`
Expected: 5 个用例 PASS。

- [ ] **Step 5: checkpoint**（Claude 会话评审时会在真实 Claude Code 里装上插件，完成 Task 8 Step 1 的冒烟后才 accept。）

## 验收命令

verify: bats tests/plugin_pactify.bats
