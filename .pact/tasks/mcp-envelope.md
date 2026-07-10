# Task mcp-envelope (C-11) — MCP 工具统一 {ok, data|error} 返回信封

## 目标
`internal/mcp/tools.go` 现在成功回裸文本(textResult)、失败回 Go error(SDK 协议层错误)。
统一为**机器可读信封**,agent 端可靠判断成败:
- 成功:`{"ok":true,"data":"<原文本>"}`(单行 JSON 文本内容)。
- 失败:`{"ok":false,"error":"<err.Error()>"}` 且 CallToolResult.IsError=true —— **不再返回
  (nil,nil,err)**(协议层错误在部分 client 里丢失细节;信封 + IsError 两条路都有信号)。

## 改动(internal/mcp/tools.go + server_test/tools 相关测试)
1. 加 helper:
   ```go
   func okResult(data string) *sdk.CallToolResult   // {"ok":true,"data":data}
   func errResult(err error) (*sdk.CallToolResult, any, error) // {"ok":false,"error":...}, IsError=true, 返回 nil error
   ```
   (JSON 用 json.Marshal 拼,别手拼字符串——data 可能含引号/换行。)
2. **全部工具**(status/join/assign/checkpoint/accept/changes/merge/list/projects/validate…)
   的返回路径统一替换:`return nil,nil,err` → `return errResult(err)`;`textResult(x)` → `okResult(x)`。
   textResult 保留但仅 okResult 内部用或删掉(看现有引用)。
3. 入参校验类错误(resolveProject 失败等)同样走 errResult。
4. 工具 Description 不必改。
5. 测试:现有 mcp 测试若断言裸文本,更新为解析信封(json.Unmarshal→ok/data 断言);
   加两例:成功工具信封 ok=true+data 含预期文本;失败(如 join 非法 roles 或 resolveProject
   不存在项目)→ ok=false+error 非空+IsError=true。

## 兼容说明(写进代码注释)
消费方是 LLM agent(宽松解析)+ 我们自己的 e2e;CLAUDE.md 的 pact-MCP 使用说明不依赖裸文本格式。
信封让 agent 能程序化判断"verb 到底成没成",是 C-11 的目的。

## 验收(视角: correctness — 全工具无遗漏、错误不再丢进协议层、JSON 合法)
verify: go build ./... && go test ./internal/mcp/ && bats tests/mcp_project.bats
(bats 需 jsonschema python?mcp_project.bats 不用 python;直接跑)
