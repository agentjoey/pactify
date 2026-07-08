# Task opencode-acp-default (OC-ACP O2+O3) — opencode 默认走 ACP + 插件降级说明

## A/B 判决(orchestrator 2026-07-09 真跑,两个同构项目各一整轮 orchestrate)
| | ACP | cmd |
|---|---|---|
| 交付 | shipped(含 rework 闭环) | shipped |
| TOK | 150 tok/2 runs 落 tokens.json | **无** |
| 审计 | 18 条风险分级+任务归因 | **0 条** |
→ 同等交付、遥测全面占优,切默认。

## O2:默认 transport 翻转(保留逃生阀)
- 找到**所有**构建 transport modes 的调用点(cmd/pactify/cmd_orchestrate.go 的 transportModes
  解析 + internal/serve 触发 orchestrate 的位置,grep NewRoutedLocalRunner):
  在解析 `--transport` **之前**先播默认 `{"opencode":"acp"}`,用户显式 `--transport opencode=cmd`
  仍能覆盖(后写覆盖前写)。抽一个共享函数 `defaultTransportModes() map[string]string`
  (internal/orchestrate 包,返回 {"opencode":"acp"}),两个调用点都用它,避免漂移。
- RoutedLocalRunner 本身不动(unset→cmd 的语义保留)。
- 测试:cmd 层 —— 无 --transport 时 modes 含 opencode:acp;`--transport opencode=cmd` 覆盖成 cmd;
  serve 触发路径同(若有现成 modes 测试,加断言)。

## O3:审计 JS 插件降级为 cmd-fallback 说明
- `cmd/pactify/cmd_audit.go` install 的 `--opencode` flag 描述改为:
  "install the audit plugin for opencode (cmd-transport fallback — the ACP transport, now the default, audits without it)"。
- `internal/audit/install.go` opencodePlugin 的注释块补一行同义说明。
- 不删插件、不改行为。

## 验收(视角: correctness — 默认 acp、逃生阀有效、两个调用点一致、O3 纯文案)
verify: go build ./... && go test ./internal/orchestrate/ ./internal/serve/ ./cmd/pactify/
