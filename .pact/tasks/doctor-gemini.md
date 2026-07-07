# Task doctor-gemini — 修 doctor 的 gemini 认证误报(只改代码,禁碰 .pact/git)

## 背景(已实测确认的 bug)
`internal/doctor/vendor.go` 判断 gemini-cli 认证只看 `~/.gemini/oauth_creds.json`。
但 gemini 经 Google 账号登录(oauth-personal)时凭据在 `~/.gemini/google_accounts.json`
(实测 `gemini -p` 能正常应答但 doctor 误报未认证)。

## 改动
`internal/doctor/vendor.go`:gemini-cli 的认证检查改为**任一存在即通过**:
`~/.gemini/oauth_creds.json` OR `~/.gemini/google_accounts.json`。
- 现在的结构可能是单个 `authRel` 字段;改成支持一组候选路径(如 `authRels []string`
  或加 `authAltRel`),gemini 用两个候选;其它 kind 行为不变(仍单路径)。
- loginHint 不变。

## verify(reviewer 独立跑)
verify: go build ./... && go test ./internal/doctor/

## 测试要求
`internal/doctor/vendor_test.go`:加用例——只有 `google_accounts.json`(无 oauth_creds.json)
时 gemini 判为**已认证**(hasAuth=true)。保持原有 oauth_creds.json 用例通过。
