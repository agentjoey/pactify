# Task a1-qa-fixes — A1 实机 QA 揪出的 6 处前端/契约修复
staging 真环境全流程走查(登录/建户/链接/Settings)发现并修复:
1. MeResponse 形状错配(email 嵌套 user 下,web 按顶层读 → "Welcome, " 空邮箱)
2. sessions/identities 端点返回包裹对象,web 按裸数组用 → AccountPanel 崩溃(identities.map)
3. sessions 列表返回截断 8 字符 id,DELETE 按全长匹配 → 列表→吊销回路永久 404
4. 会话行 expiresAt 字段不存在(relay 返回 createdAt)→ "expires Invalid Date"
5. 魔链邮件链接指向 web 域(无此路由)→ 改指 relay 自身(x-forwarded-proto 推导 base)
6. 魔链登录不建 email Identity 行 → verify 时幂等 upsert
另:hosted 模式 Settings 泄漏 PROJECT/MACHINE 面板(打本地 /api 的死数据)→ 按 capability 隐藏,
默认落 Account 面板。
verify: cd web && npx tsc --noEmit && npx vitest run && cd ../cloud/relay && npx tsc --noEmit && npm test
