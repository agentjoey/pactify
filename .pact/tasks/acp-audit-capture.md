# Task acp-audit-capture (#20b) — ACP 档 worker 工具调用直写 audit(覆盖 kimi)

## 背景
kimi CLI **无 hooks 系统**(已调研:仅 acp/doctor/config),cmd 路径(`kimi -p`)的工具调用不可见。
但 **ACP 传输**的 `session/update` 带 `tool_call`/`tool_call_update` 事件——在 `AcpRunner`
(internal/orchestrate/acprunner.go)把它们直写 audit,kimi(以及任何走 ACP 的 kind)即获审计。
cockpit 路径已有审计(cockpit-audit);本任务补 **orchestrate worker 棒**的 ACP 路径。

## 改动(internal/orchestrate/acprunner.go + 测试)
1. `AcpRunner.Run` 的 `conn.OnSessionUpdate` 回调(:119)里:`u.Kind == "tool_call"` 时构造
   `audit.Record{TS: now RFC3339, Project: lc.Project, Repo: lc.RepoDir, Seat: lc.Seat, Task: lc.Task,
   Kind: "acp:"+lc.Kind, Session: string(u.SessionID), Tool: <从 u.Raw 解析>, Summary: <同>, Risk: <分级>,
   Decision: "allow"}` → `audit.Append(rec)`(best-effort,错误忽略,绝不影响 run)。
   只记 `tool_call`(新调用),忽略 `tool_call_update`(输出增量,防刷屏)。
2. 解析 u.Raw:ACP tool_call update 形如 `{"sessionUpdate":"tool_call","title":...,"kind":...,
   "rawInput":{...}}`(字段尽力解析:title→Summary、kind/title→风险线索、rawInput.command 存在→exec)。
   抽一个包内纯函数 `acpAuditRecord(lc LaunchContext, u acp.SessionUpdate, now time.Time) (audit.Record, bool)`
   便于单测;解析不出 title 用 "tool_call"。风险分级:rawInput.command 或 kind/title 含 shell/exec/bash→"exec";
   含 write/edit/create/delete→"write";其它→"read"(保守)。**Summary 走 audit.Record 常规路径即可
   (Append 前调用方不用再脱敏——FromHook 里的 redact 是 hook 专用;这里 Summary 用 title(短语),
   不放 rawInput 原文,避免泄密)。**
3. 单测:acpAuditRecord 喂三种 Raw(shell 类、write 类、无 title)断言 Tool/Summary/Risk;
   `tool_call_update` 返回 ok=false;Run 集成不必(现有 fake conn 测试保持绿)。

## 验收(视角: security — kimi ACP 棒有审计行、Summary 不含 rawInput、零 run 影响)
verify: go build ./... && go test ./internal/orchestrate/ ./internal/audit/
