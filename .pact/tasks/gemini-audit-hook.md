# Task gemini-audit-hook (#20a) — gemini worker 原生审计捕获

## 事实(已调研)
- gemini CLI 有 hooks 系统且 **claude 兼容**(`gemini hooks migrate --from-claude` 存在)。
- 配置走 settings.json(项目级 `.gemini/settings.json`),schema 同 claude 的
  `{"hooks":{"PreToolUse":[{"matcher":..,"hooks":[{"type":"command","command":..}]}]}}`。
- pactify 已有 claude 式安装器:`internal/audit/install.go` 的 `installClaudeStyle(kind, path)`
  /`uninstallClaudeStyle`/`Detect`——**复用它**,只是路径换 `.gemini/settings.json`、kind 换 "gemini"。
- `FromHook`(hook.go:37)解析 claude 式 stdin JSON;gemini 的工具名不同:
  `run_shell_command`(exec)、`write_file`/`replace`(write)、`read_file`/`read_many_files`(read)、
  `google_web_search`/`web_fetch`(read)。风险映射在 hook.go 的 tool switch(:81)加这些 case。

## 改动(internal/audit + cmd)
1. `install.go`:`Install`/`Uninstall` 支持 kind "gemini"(installClaudeStyle 到
   `<repo>/.gemini/settings.json`,hook command 用 `pactify audit hook --kind gemini`——看 claude
   分支怎么拼 command,保持一致);`Detect` 也扫 `.gemini/settings.json`(Status 加 gemini 行)。
2. `hook.go`:`FromHook` 接受 kind "gemini"(解析同 claude 式 JSON);风险 switch 加 gemini 工具名映射。
3. `cmd/pactify/cmd_audit.go`:`audit install/uninstall` 加 `--gemini` flag(照 --claude-code 模式),
   target 文案 `.gemini/settings.json`。
4. 测试:install→settings.json 有 hook 条目(merge 不破既有键)→ uninstall 干净;FromHook 喂一条
   gemini 形状的 PreToolUse JSON(tool_name:"run_shell_command",tool_input:{command:"ls"})→
   Record{Kind:"gemini",Tool,Risk:"exec"} 正确;write/read 工具映射各一例。

## 验收(视角: correctness — 安装/卸载幂等不破既有 settings、工具风险映射对)
- reviewer 会做**真机 e2e**:临时 repo 装钩子跑 `gemini -p`,确认 audit 记录落盘。
verify: go build ./... && go test ./internal/audit/ ./cmd/pactify/
