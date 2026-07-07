# Task acp-hardening — ACP bridge 加固([ACP-2] / cockpit spec E0-①)

**只改代码,禁止碰 `.pact/`、禁止跑任何 `pactify`/`git` 命令**(orchestrator 负责 pact + git)。

## 背景
cockpit spec E0-① 已核实(2026-07-07,npm view):`@agentclientprotocol/claude-agent-acp`
包名正确(v0.57.0,活跃维护),**无需改包名**。但两处加固待做。

## 改动 1 — pin ACP bridge npx 版本
文件 `internal/orchestrate/acprunner.go` 的 `acpCommand`:给 3 个 npx bridge 钉版本
(现在 `npx -y <pkg>` 拉 latest,有漂移风险):
- claude-code → `@agentclientprotocol/claude-agent-acp@0.57.0`
- codex-cli   → `@zed-industries/codex-acp@0.16.0`
- gemini-cli  → `@google/gemini-cli@0.49.0`(保留 `--acp` 参数)
- kimi-cli 不变(原生 `kimi acp`)
加注释:版本经 `npm view` 核实于 2026-07-07,升级需 deliberate。

## 改动 2 — env 白名单(防 serve 密钥泄给第三方 npx bridge)
文件 `internal/acp/acp.go` 的 `Spawn`:现在 `cmd.Env = append(os.Environ(), env...)`
把 serve 的**全部**环境变量(含 relay/pactify 内部密钥)透传给第三方 npx 子进程。
改为**过滤继承**:丢弃 pactify/relay 内部密钥,保留 vendor agent 认证+运行所需的一切
(PATH/HOME/vendor auth 等)。
- 新增函数 `filteredEnviron() []string`:返回 `os.Environ()` 剔除满足以下任一的 key:
  - 等于 `PACT_RELAY_TOKEN`
  - 以 `PACTIFY_` 开头(pactify 内部配置)
  保留其余全部(系统 env + vendor auth)。
- `Spawn` 改 `cmd.Env = append(filteredEnviron(), env...)`(caller 显式追加的 PACT_* 仍生效)。
- 注释说明:采用 denylist(剔已知 pactify 密钥)而非严格 whitelist,因 vendor 认证依赖
  大量系统/HOME env,严格白名单会破认证;目标是「不泄 pactify 密钥」而非「最小 env」。

## verify(reviewer 会独立跑)
verify: go build ./... && go test ./internal/acp/ ./internal/orchestrate/ 2>&1 | grep -qv FAIL

## 测试要求
- `internal/acp/acp_test.go`:加 `TestFilteredEnvironDropsPactifySecrets`——设 `PACT_RELAY_TOKEN`
  + `PACTIFY_HOME` + 一个普通 var(如 `PATH`),断言过滤后前两者被剔、`PATH` 保留。
- `internal/orchestrate/acprunner_test.go`(或 acp 现有测试):断言 `acpCommand` 返回的
  argv 含钉版本字符串(如 `@agentclientprotocol/claude-agent-acp@0.57.0`)。
