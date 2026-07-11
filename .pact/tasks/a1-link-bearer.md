# Task a1-link-bearer (ACCT A1-2) — link-by-proof + 会话代领 bearer + 管理端点(relay)

## 背景
设计 §4/§5.2/§5.6/§7。承接 a1-schema-auth(identity 模块已有 session/CSRF/me)。
密钥占有证明是身份面↔密钥面唯一绑定途径;身份面绝不接触 secret/私钥。

## 交付(全部在 cloud/relay/src/identity/ 扩展)
### 1. link-by-proof
- `POST /v1/id/link/challenge` → {challenge}(随机 nonce,短 TTL,绑定当前 session)。
- `POST /v1/id/link {publicKey, challenge, signature}`(需 session+CSRF;与 /v1/auth 同
  strict 限速桶):验 challenge 属于本 session 且未过期 → ed25519 验签(复用 src/auth.ts 的
  验签逻辑,抽共享函数别复制)→ Account(publicKey) 存在 → upsert AccountMember(userId,
  accountId, role owner)。签名不过/挑战过期/账户不存在 → 401/404,不泄露细节。
### 2. 账户创建(新用户路径,设计 §5.1)
- `POST /v1/id/accounts {publicKey}`(session+CSRF):publicKey 未被占用 → 建 Account
  (tier free)+ AccountMember(owner)。已占用 → 409(引导走 link)。
### 3. WebSession 代领 bearer(设计 §4 关键新能力)
- `POST /v1/id/token {accountId}`(session+CSRF):当前 user 是该 account 的 member →
  签发与 /v1/auth 同构的 bearer(复用同一 token 铸造与 TTL 逻辑,抽共享函数)→
  {token, accountId, expiresAt}。非 member → 403。
### 4. 管理端点(设计 §5.6 骨架所需)
- `GET /v1/id/sessions`(本 user 的 WebSession 列表:id 前缀/ua/createdAt/current 标记)
- `DELETE /v1/id/sessions/{id}`(吊销;可吊销当前=登出)
- `GET /v1/id/identities` + `DELETE /v1/id/identities/{id}`(**至少留一个**,最后一个删除 → 409,设计 §7.7)
### 5. 测试
link 全流程(challenge→签名→member 出现在 /v1/id/me)、错签名/过期挑战/跨 session 挑战拒、
账户创建/409、token 代领(member 通/非 member 403/领到的 bearer 真能过既有 /v1/runs 鉴权)、
sessions 列表/吊销后旧 cookie 失效、identity 最后一个不可删。签名用 @pactify-apps/crypto
的 deriveAccountKeypair().sign 造真签名(golden 路径,不 mock 验签)。

## 门
cd cloud/relay && npx tsc --noEmit && npm test 全绿。不碰 web/、internal/。
verify: cd cloud/relay && npx tsc --noEmit && npm test
