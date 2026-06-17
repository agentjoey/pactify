# t-sp3-frontend：Setup apply UI（SP3 前端）

> 完整要点见 `docs/superpowers/plans/2026-06-17-sp3-onboarding.md` 的 **Task t-sp3-frontend**；
> 设计见 `docs/superpowers/specs/2026-06-17-sp3-onboarding-design.md`。先读它们 + 现有 `Setup.tsx`。

## 目标
给 `Setup.tsx` 加一键 Apply，调用已 ship 的 `POST /api/setup/apply`：

- `lib/api.ts` 加 `setupApply(body)`（复用 `writeJSON`，body `{path, project, seats:[{id,roles,entry,kind}]}`）。
- `Setup.tsx`：加项目路径输入框 + 项目名（默认 basename）；沿用现有 suggest 列表 + 角色编辑 + validate 徽章；
  「Apply」按钮（roles 完整时启用）→ `setupApply` → 结果区显示：哪些 wire 落地、哪些 TOML docOnly（带 snippet 复制）、错误提示。

## 文件
- 改 `web/src/components/Setup.tsx`、`web/src/lib/api.ts`、`web/src/components/Setup.test.tsx`

## 纪律
- **TDD**：先写失败 vitest → 红 → 实现 → 绿。测：Apply 禁用条件、POST body、结果渲染（wired/docOnly/error）。
- 复用现有 Setup 的 suggest/validate 逻辑，勿重造。完成跑 verify 绿再 checkpoint。

verify: cd web && npx tsc --noEmit && npx vitest run
