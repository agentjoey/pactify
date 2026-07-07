# Task audit-redact — 扩展 audit 脱敏 denylist 覆盖更多密钥格式

## 背景
`internal/audit/hook.go` 的 `secretRes` 已覆盖:URL basic-auth、`*key|token|secret|password|passwd|pwd=VALUE`、
GitHub PAT(ghp_/github_pat_)、AWS AKIA、bearer/token:/secret:/sk- 前缀。
仍有常见密钥格式**未覆盖**(实测确认漏):Slack、Google API key、Stripe live、JWT、PEM 私钥头、GCP OAuth。
本任务**只加 denylist 条目**(不反转成 allowlist —— 那个方向 backlog 已判定不做)。

## 改文件(仅这两)
- `internal/audit/hook.go`(给 `secretRes` 追加新 pattern)
- `internal/audit/hook_redact_test.go`(每个新 pattern 加正反用例)

## 契约:给 secretRes 追加以下 pattern(保持"保留引导词、替换敏感值"的既有风格,顺序追加到末尾)
1. Slack token:`xox[baprs]-` 开头 + 后续 token 字符 → 替换为 `xox?-***`(保留 `xox<letter>-` 引导)。
   正则约:`\bxox[baprs]-[A-Za-z0-9-]{10,}\b` → `xox-***`(或保留字母,二选一,测试对齐即可)。
2. Google API key:`AIza` + 35 位 → `AIza***`。正则:`\bAIza[0-9A-Za-z_\-]{35}\b`。
3. Stripe live/test 密钥:`(sk|rk|pk)_(live|test)_` + 后续 → 保留前缀 `$1_$2_` 替 `***`。
   正则:`\b((?:sk|rk|pk)_(?:live|test)_)[A-Za-z0-9]{10,}\b` → `$1***`。
4. GCP OAuth access token:`ya29\.[A-Za-z0-9_\-]+` → `ya29.***`。
5. JWT:三段 base64url(`eyJ` 开头.xxx.yyy) → `***`。
   正则:`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\b` → `***`。
6. PEM 私钥头:`-----BEGIN [A-Z ]*PRIVATE KEY-----` → `-----BEGIN PRIVATE KEY----- ***`(或整体 → `***`;
   目标是不让私钥体原样进日志;若命令里只出现头,替换头即可,保守起见把头到尾用 `(?s)` 匹配到 END 头也可,
   但注意 summary 已截断到 summaryCap,简单起见只脱敏 BEGIN 头这一行即可)。

实现要求:
- 追加到 `secretRes` 切片末尾,保持"应用有序"注释语义。已有条目不改。
- 不破坏现有任何测试(现有 sk-/token 等用例仍须通过)。
- gofmt 干净。

## 测试(hook_redact_test.go 表驱动,每个新格式 present→redacted、且一个"看似像但不该动"的负例)
- Slack:`xoxb-1234567890-abcdef` → 含 `***`、不含原 token 尾段。
- Google:`AIza` + 35 位样例 → 脱敏。
- Stripe:`sk_live_abcd1234efgh` → `sk_live_***`;负例:`sk_live_`(无值)不误伤到崩。
- GCP:`ya29.a0Af...` → `ya29.***`。
- JWT:`eyJhbGciOi.eyJzdWIi.SflKxwRJ` 样式 → `***`。
- PEM:含 `-----BEGIN RSA PRIVATE KEY-----` 的命令 → 私钥头被脱敏。
- 回归:已有一条普通命令(无密钥)redact 后不变(除截断)。

## 验收 / Acceptance(视角: security — 覆盖面正确、不漏不误伤既有格式)
- reviewer 独立跑 verify 门通过 + 阅读 diff 确认只追加、无删改既有 pattern。

## verify
verify: go build ./... && go test ./internal/audit/
