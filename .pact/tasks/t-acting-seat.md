# t-acting-seat：acting-seat 授权基线（SP1 块④）

> 完整 TDD 步骤（含全部测试代码 + 实现骨架）见
> `docs/superpowers/plans/2026-06-17-sp1-stability-security.md` 的 **Task 4**。先读它。

## 目标
给所有「有副作用」HTTP 端点补 acting-seat 授权闸（分层），seat 未配置时 fail-closed。
不做前端确认弹窗（公网入口已由 Cloudflare Access 认证本人）。

## 分层规则
- **project-scoped 协议写**（写该 project 的 `.pact`）：要求 `actingProject` 成功（seat 配置 + ∈ roster）。
  现有 tasks/verbs 已覆盖；**补 `handleWire`**（wire.go）也用 `actingProject`。
- **machine-scoped 操作**（register/config/manifest/registry/recipe/prune）：不要求 roster
  成员（机器级），但加最低闸 `requireSeat`——`s.seat==""` → **422** fail-closed。

## 文件
- 改 `internal/serve/author.go`：加 `func (s *Server) requireSeat(w http.ResponseWriter) bool`
  （`s.seat==""` → writeErr 422 "no acting seat configured" + return false；否则 true）。
- 改各 machine handler 开头加 `if !s.requireSeat(w) { return }`：
  `handleAgentRegister`/`handleAgentUnregister`/`handleAgentConfigSet`（agents.go）、
  `handleManifestCreate`/`handleManifestDelete`（manifests.go）、
  `handleRegistryAdd`/`handleRegistryDelete`（registry.go）、
  `handleSessionsPrune`（sessions.go）、`handleRecipeExpand`（recipes.go）。
- 改 `internal/serve/wire.go` `handleWire`：解析 dir 后加 `actingProject` 校验（422）。
- 建 `internal/serve/acting_seat_test.go`：machine 端点 no-seat→422、有 seat→放行；wire no-seat→拒。

## 纪律
- **TDD**：先写失败测试 → 跑红 → 最小实现 → 跑绿 → checkpoint。
- 现有 author/agents/manifests 测试若因新闸需 `SetSeat`，相应补齐，保持全绿。

verify: go test ./internal/serve/
