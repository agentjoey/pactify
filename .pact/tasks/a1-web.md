# Task a1-web (ACCT A1-3) — 登录页 + 会话模式 RelaySource + 账户/设备管理页骨架(web)

## 背景
设计 §4/§5;relay 端点已就位(a1-schema-auth + a1-link-bearer):/v1/id/oauth/github/start、
/v1/id/magic、/v1/id/me、/v1/id/link{,/challenge}、/v1/id/accounts、/v1/id/token、
/v1/id/sessions、/v1/id/identities。cookie 跨域:web(Vercel)→relay(fly)fetch 一律 `credentials:"include"`;RELAY_URL 见
web/src/lib/source.ts。**CSRF 契约**:`GET /v1/id/me` 响应体带 `csrf` 字段,客户端内存持有,
全部 POST 请求带 `x-aw-csrf: <csrf>` 头(跨域读不到 relay cookie,没有 csrf cookie 这回事)。

## 交付
### 1. 登录页(hosted 模式无 session 时的默认页)
- 「Sign in with GitHub」→ 跳 `${RELAY_URL}/v1/id/oauth/github/start`;
- 邮箱魔链表单(POST /v1/id/magic,提交后显示「查收邮件」);
- 保留现有「直接贴 master secret」入口(向后兼容,折叠为次要选项)。
### 2. 会话模式(免 secret)
- 启动时 GET /v1/id/me(credentials include):有 session 且有 accounts →
  POST /v1/id/token 领 bearer → 构建 **session 模式 RelaySource**:无 master secret,
  RelayClient 以「bearer-only」构造(看 relay-client 构造器,master 参数改可选或加静态工厂,
  decrypt 能力缺省);capabilities 不变,但**解密相关路径守卫**:getState/fetchEventsLog/
  getEvents/cockpitSubscribe 等需要解密的方法在无 secret 时返回明确的「locked」信号
  (抛特定错误或返回空+标志,选一种一致做)。
- UI:无 secret 时看板区显示锁卡:「内容已端到端加密——从已有设备配对接收密钥,或粘贴
  master secret 解锁」+ 内联 secret 输入(解锁后 = 现有完整 RelaySource,路径复用
  connectRelaySource);顶栏显示登录邮箱。
- **明文可见部分照常**:项目列表(listProjects)、机器列表(getMachines)——这两个不需要解密。
### 3. 账户绑定流(设计 §5.1/§5.2)
- 登录后无 accounts → 引导卡二选一:
  a) 「创建新账户」:generateMasterSecret()(@pactify-apps/crypto,web 已依赖 relay-client——
     确认 crypto 可直接 import,不行就经 relay-client re-export)→ deriveAccountKeypair →
     POST /v1/id/accounts {publicKey} → **强制导出步骤**(显示 hex + 复制/下载 .txt,
     勾选「我已保存,明白无法找回」才能继续);
  b) 「链接已有账户」:贴 secret → challenge(POST /v1/id/link/challenge)→ 本地
     keypair.sign(challenge) → POST /v1/id/link → 刷新 me。
### 4. 账户/设备管理页骨架(Settings 内新「Account」区或独立视图,跟现有 Settings 结构走)
- 登录身份(邮箱/identities 列表+解绑,最后一个禁删)、WebSession 列表+吊销、
  机器列表(复用 getMachines)、账户 tier 徽标(free/personal,来自 /v1/id/me)、登出。
### 5. 测试
- 登录页渲染/两入口;me→token→session 模式构建;locked 守卫(无 secret 调 getState 得
  locked 信号不崩);解锁流(贴 secret 后升级为完整 source);创建账户流(mock crypto 或真
  crypto 小向量,导出步骤门控);link 流签名调用;管理页列表/吊销(mock fetch)。
- 全量 vitest + tsc + build 绿(cloud/relay-client 若改了构造器,其 tsc+test 也要绿)。

## 门
verify: cd web && npx tsc --noEmit && npx vitest run
