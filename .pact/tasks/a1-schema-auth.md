# Task a1-schema-auth (ACCT A1-1) — 身份面数据模型 + SSO 登录核心(relay)

## 背景
设计:`.agent/plans/acct-sso-design-2026-07-11.md`(已 APPROVED)§2/§3/§4/§7。
身份面 = relay 内新模块,与密钥面(challenge/response,零改动)分层;绑定只认密钥占有证明(下个任务)。
本任务:表 + SSO 登录会话本身。全部 additive(prod 与 linx 共库)。

## 交付
### 1. Prisma 迁移 `11_identity_plane`(cloud/relay/prisma)
照设计 §3:User/Identity/WebSession/AccountMember/Subscription 五表 + Account 加
`tier String @default("free")`、`keyEpoch Int @default(0)` 两列。迁移 SQL 与 schema 双写
(照 10_add_pact_event_id 先例,SQL 必须与 prisma 规范生成一致)。全 additive,不动既有表列。

### 2. 身份模块 `cloud/relay/src/identity/`(新目录,路由前缀 /v1/id)
- **会话**:opaque token(32B random)→ 存 HMAC(token) 为 WebSession.id;cookie
  `aw_session`:httpOnly+Secure+SameSite=Lax,30 天滑动(读时续)。`sessionOf(req)` helper。
- **GitHub OAuth**(env:ID_GITHUB_CLIENT_ID/SECRET,未配置则端点 503):
  `GET /v1/id/oauth/github/start`(state+PKCE,state 存短 TTL 表内存 map 即可)→ 302 GitHub;
  `GET /v1/id/oauth/github/callback`:换 code(注入 fetch 以便测试 fake IdP)→ 取 user id+
  primary verified email → upsert Identity(provider github, subject)→ 无则建 User → 建
  WebSession → set-cookie → 302 `ID_WEB_URL`(env,默认 staging web 域)。
  email 未验证 → 拒(设计 §7.4)。
- **邮箱魔链**:`POST /v1/id/magic {email}`(限速 3/小时/邮箱):生成一次性 15min token
  (HMAC 存储同会话模式);投递:env RESEND_API_KEY 配置则真发(fetch resend api),
  否则 log.info 链接(staging/dev 模式);响应恒 `{ok:true}`(不泄露邮箱是否存在)。
  `GET /v1/id/magic/verify?token=` → 建/取 User(email)→ WebSession → 302。
- **`GET /v1/id/me`**:{user:{id,email}, identities:[provider], accounts:[{accountId,role,tier}]}
  (accounts 经 AccountMember join;无 session → 401)。
- **`POST /v1/id/logout`**:删当前 WebSession + 清 cookie。
- **CSRF**:/v1/id/* 的全部 POST 要求 `x-aw-csrf` header == cookie `aw_csrf`
  (double-submit;登录成功时随 session 一起 set,非 httpOnly)。OAuth start/callback(GET)豁免。
- **限速**:/v1/id/magic 与 /v1/auth 同 strict 桶思路(复用现有 rate-limit 设施加桶)。

### 3. 接线与测试
- server.ts 挂载 identity 路由(独立注册函数 registerIdentityRoutes(app, deps),deps 注入
  db/secret/fetch/now——照现有 opts 注入风格,测试不起真网络)。
- vitest(照 cloud/relay/test 既有风格):fake IdP 的 oauth 全流程(start→callback→me)、
  魔链(申请→verify→me;限速;不存在邮箱不泄露)、session 过期/续期/登出、CSRF 拒/通、
  未配置 client id 时 503、email 未验证拒。
- prod 兼容:迁移在空列上默认值,linx 路径零感知(不 import identity 模块也能跑的结构)。

## 门
cd cloud/relay && npx tsc --noEmit && npm test 全绿;prisma migrate SQL 与 schema 一致
(如包内有校验方式则跑)。不碰 web/、internal/。
verify: cd cloud/relay && npx tsc --noEmit && npm test
