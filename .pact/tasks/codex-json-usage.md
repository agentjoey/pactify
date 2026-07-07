# Task codex-json-usage (W0a) — codex worker 加 --json 捕获 usage

## 目标
codex 作为 worker 跑 `codex exec` 现在**不出 usage**(TOK=0 的洞)。给 codex BuildArgs 加
`--json`,codex 即输出 JSONL(含 `{"type":"turn.completed","usage":{"input_tokens","output_tokens"}}`),
现有 `tokens.Parse` 已能从中提取(usageShape.Usage.input_tokens/output_tokens 正好匹配)。纯加性。

## 已实测(真机 codex 0.142.5)
`codex exec --json ...` 尾部出:
`{"type":"turn.completed","usage":{"input_tokens":11880,"cached_input_tokens":9600,"output_tokens":6,"reasoning_output_tokens":0}}`
`tokens.Parse` 的 usageShape 有 `Usage.InputTokens json:"input_tokens"` + `OutputTokens json:"output_tokens"`,
所以这行会被解析成 input+output。无需改 Parse,只需让 codex 带 --json。

## 改文件
- `internal/agent/launch.go`(codex-cli BuildArgs 加 --json)
- `internal/agent/launch_test.go`(更新 codex 断言)
- `internal/tokens/tokens_test.go`(加一条:喂真实 codex turn.completed 行,断言 Parse 返回 11886、ok=true)

## 契约
- codex-cli 的 BuildArgs:在 `exec` 之后插 `--json`。即 blanket → `{"exec","--json","--sandbox","danger-full-access",<briefing>}`;
  scoped → `{"exec","--json","--sandbox","workspace-write",<briefing>}`;有 model 时 `-m <model>` 位置不变(仍在 sandbox 之后、briefing 之前)。
  **只加 --json 一个 token,其它顺序不动。**
- 更新 `launch_test.go` 里 codex 的三处断言(TestLaunchProfile_DefaultsMatchRunner 的 codex 行、
  TestLaunchProfile_ScopedPosture 的 codex blanket/scoped 两处)加上 "--json"。
- `tokens_test.go` 加用例:
  `Parse("codex", `+"`"+`{"type":"turn.completed","usage":{"input_tokens":11880,"cached_input_tokens":9600,"output_tokens":6}}`+"`"+`)`
  → 返回 (11886, true)。

## 验收 / Acceptance(视角: correctness — args 只多 --json、usage 正确提取、断言同步更新)
- reviewer 独立跑 verify(全仓,因 launch 被多处断言)。

## verify
verify: go build ./... && go test ./internal/agent/ ./internal/tokens/ ./internal/orchestrate/
