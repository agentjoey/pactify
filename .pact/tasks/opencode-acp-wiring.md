# Task opencode-acp-wiring (OC-ACP O1) — opencode 升入 ACP 档:接线

## 事实基础(orchestrator 2026-07-09 真机验证,opencode 1.17.13)
- `opencode acp` 原生 ACP server,握手通过:protocolVersion:1、loadSession:true、
  sessionCapabilities{close,fork,list}。
- **模型**:`session/new` 返回 configOptions,model select 的 currentValue **已是
  deepseek/deepseek-v4-pro**(继承 opencode 全局配置,与 cmd 路径同源)——与 kimi 档一致
  (模型走 CLI 自身配置,ACP 不另钉),真机实答确认。
- **usage**:`session/prompt` 结果带 `usage:{inputTokens,outputTokens,totalTokens,...}`,
  另有 `usage_update` 事件带 cost——现有 acp 客户端 usage 解析路径直接可用。

## 改动(四处接线 + 测试)
1. `internal/orchestrate/acprunner.go` 的 `acpCommand`:加
   `case "opencode": return "opencode", []string{"acp"}, true`(原生二进制,注释一行:模型走
   opencode 全局配置,与 kimi 同模式;usage 从 prompt 结果解析)。
2. `internal/cockpit/acp.go` 的 `acpCommandFor`:同样加 opencode。
3. `internal/serve/cockpit.go` 的 `cockpitCapableKind` + `backendForKey`:加 "opencode"
   (backendForKey → `cockpit.NewACPBackend("opencode")`)。
4. `internal/doctor/vendor.go` 的 `acpKinds`:加 "opencode": true(ACP transport 预检覆盖)。
5. 测试:
   - acprunner_test:acpCommand("opencode") 断言(照 kimi/gemini 现有断言样式)。
   - cockpit acp_test:acpCommandFor("opencode") 断言;**gated 真机 smoke** 加 opencode 变体
     (COCKPIT_SMOKE=1 且 opencode 在 PATH:Start→Prompt 轻量→断言收到 EventMessage/usage;
     照 TestACPBackendSmokeKimi 模式,别重复太多——可参数化或复制小函数)。
   - serve cockpit_test:cockpitCapableKind("opencode")==true(若已有该函数测试,加一行)。
   - doctor vendor_test:opencode 的 transport 检查行为(若有 per-kind transport 测试,加 opencode)。

## 不做(O2/O3 另任务)
- 不切 orchestrate 默认 transport(仍 cmd,--transport opencode=acp 显式可用)。
- 不动 audit JS 插件。

## 验收(视角: correctness — 四处接线一致、真机 smoke 过、零默认行为变化)
- reviewer 亲跑 opencode 真机 smoke + 全量门。
verify: go build ./... && go test -race ./internal/orchestrate/ ./internal/cockpit/ ./internal/serve/ ./internal/doctor/
