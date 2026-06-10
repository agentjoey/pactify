# pact 插件:个人测试与发布操作手册

> 适用对象:维护者(Joey)。覆盖三条路径:本地开发加载、自有 marketplace 真实一键(无需审核)、
> 以及日后提交 Anthropic 社区 marketplace 的完整步骤(备查)。
> 依据:code.claude.com/docs/en/plugins 与 plugin-marketplaces(2026-06 验证)。

## 0. 前置条件(一次性)

```bash
# pactify 二进制必须在 PATH 上(插件的 MCP server 跑的是裸 `pactify mcp`)
curl -fsSL https://pactify.dev/install.sh | sh
pactify version          # 应显示 v0.3.0+
pactify doctor           # PATH ✓ + mcp server ✓ 即可
```

测试仓建议用一个**已 pact 化的 repo**(如 `~/AgentWorks/Code_Claude/pact-dogfood-m2`),
这样 MCP tools 有真实状态可操作。

⚠️ **座位身份(最常见的坑)**:插件的 MCP server 从 **Claude Code 启动时的环境**继承
`PACT_AGENT_ID` —— 会话里再 `export` 不会传给 server。测试时这样启动:

```bash
cd <pact 化的 repo>
PACT_AGENT_ID=<你的座位> claude
```

(或先 `pactify setup` 给 repo 写带座位的项目级 `.mcp.json`,但那就和插件**双重注册**了
`pact` server —— 测插件时二选一,见 plugins/pact/README.md 的 double-wiring 说明。)

## 路径 1 — 本地目录加载(开发迭代最快)

不安装、不走 marketplace,直接从工作树加载:

```bash
cd <测试 repo>
PACT_AGENT_ID=<座位> claude --plugin-dir ~/AgentWorks/Code_Claude/pactify/plugins/pact
```

会话内:
- `/help` → 应看到 `pact` 命名空间下的 skill(`/pact:pact`)
- `/mcp` → 应看到 `pact` server 及其 tools(status/join/assign/checkpoint/accept/changes/merge/validate)
- 调 `status` tool → 应返回 repo 的 STATE
- 改了插件文件后,`/reload-plugins` 热重载(skill/hooks/MCP 都会重载)

同名优先级:`--plugin-dir` 的本地副本会覆盖已安装的同名插件(本会话内)——
适合"装了正式版、又要试改动"的场景。

## 路径 2 — 自有 marketplace 真实一键(推荐的"上传 Claude 测试")

repo 是 public 的,`.claude-plugin/marketplace.json` 已在 main —— 它**就是**一个
live marketplace,任何人(包括你)无需审核即可安装:

```bash
# 任意机器的 Claude Code 内:
/plugin marketplace add agentjoey/pactify
/plugin install pact@pactify
```

- 安装后插件被**拷贝到本地缓存**(不引用 repo 工作树),所以这测试的是"用户拿到的真实产物"。
- SessionStart hook 验证:在一个 **pactify 不在 PATH** 的环境启动会话(如
  `PATH=/usr/bin:/bin claude` 临时测),应看到三行安装提示;正常 PATH 下应静默。
- **更新发布流**:改插件 → bump `plugins/pact/.claude-plugin/plugin.json` 的 `version`
  (不 bump 用户收不到更新)→ push main → 用户侧 `/plugin marketplace update pactify`
  后重装/更新即可拿到新版。
- 卸载/禁用:`/plugin` 打开管理界面操作。

## 路径 3 — 个人 skills 目录(可选,不推荐用于本插件)

`claude plugin init <name>` 会在 `~/.claude/skills/<name>/` 生成个人插件,
下次会话自动加载(`<name>@skills-dir`)。适合纯个人 skill;本插件带 MCP+hook 且
要验证 marketplace 链路,用路径 1/2 更贴近真实。

## 测试清单(pact 插件特有)

- [ ] `/help` 列出 `pact` skill;`/mcp` 列出 `pact` server 与 8 个 tools
- [ ] 以正确座位启动(`PACT_AGENT_ID=<seat> claude`),`join` tool 成功注册座位并切分支
- [ ] 不带座位启动,`join` 报错信息符合 SKILL.md 的指引(提示从启动 shell 导出或 pactify setup)
- [ ] binary 缺失环境下 SessionStart hook 打印 3 行提示且**不阻塞会话**(exit 0)
- [ ] 与项目级 `.mcp.json` 共存时确认只启用其一(避免双 `pact` server)
- [ ] 跑一轮最小协议流:status → join → (orchestrator 另起会话 assign) → checkpoint

## 日后:提交 Anthropic 社区 marketplace(备查,暂不执行)

1. 本地预检:`claude plugin validate ./plugins/pact`(审核流水线跑同样的检查 + 自动安全扫描)。
2. 提交入口(二选一):
   - Claude.ai:`claude.ai/settings/plugins/submit`
   - Console:`platform.claude.com/plugins/submit`
3. 过审后插件被**钉在特定 commit SHA** 收录进 `anthropics/claude-plugins-community`
   目录;此后你每次 push,官方 CI 会自动 bump 钉住的 SHA。
4. 公共目录**每夜同步**,过审到可安装有延迟;在
   `anthropics/claude-plugins-community` 仓的 `.claude-plugin/marketplace.json` 里搜
   `pact` 确认是否已可装。用户侧:`/plugin marketplace add anthropics/claude-plugins-community`
   → `install pact@claude-community`。
5. 官方 `claude-plugins-official` 是 Anthropic 单独策展的,无申请流程,提交表单不会进入它。

## 版本对齐提醒

`plugins/pact/.claude-plugin/plugin.json` 的 `version`(现 0.3.0)与 repo release tag
**没有自动同步机制**(M2.2 终审 deferred minor M1)——每次发版 checklist 里手动对齐,
或日后加 release 钩子。
