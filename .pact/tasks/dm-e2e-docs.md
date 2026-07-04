# dm-e2e-docs — 集成冒烟 + 文档收尾

> 母 spec:`docs/specs/driver-modernization.md` §6。依赖 dm-acp-runner + dm-session-resume。

## 交付
1. 集成冒烟测试(`internal/orchestrate/acp_smoke_test.go`,`//go:build acpsmoke` tag 或 env 门):若本机 `kimi` 在 PATH 且已登录 → 真跑一个最小 ACP 棒(NewSession+一句 prompt);否则 t.Skip。CI 不跑(无 kimi),本地验证用。
2. `docs/architecture.md`:驱动层小节更新(CmdRunner/AcpRunner/路由/知识注入/会话续接,一段+一图示即可)。
3. `CLAUDE.md` Version 行按 release 惯例补 driver-modernization 描述(简短)。
4. 全仓门:`go test ./...`、`go vet ./...`、bats e2e、`cd web && npx vitest run && npx tsc -b --noEmit` 全绿——这是全包合并门。

## 边界
文档实事求是写已落地行为,不写愿景。
