# Onboarding — 5 分钟把一个新项目接入 Pactify

把任意 repo 接入 Pactify，让你已装的 agent（opencode / Claude / agy / Codex / Kimi …）
协同开发它。零外部服务——协调层就是你的 git + `.pact/` 文件。

## 前置：装 pactify（一次）

```bash
curl -fsSL https://pactify.dev/install.sh | sh   # 装 pactify 二进制
pactify doctor                                    # 自检安装 + agent 探测
```

## Step 1 — 注册你的 agent（一次，机器级）

让 pactify 知道你这台机器上装了哪些 agent：

```bash
pactify agent scan        # 探测已装 agent
pactify agent register opencode   # 注册
pactify agent register claude-code
```

注册是机器级的（写 `~/.pactify/`），所有项目共享。

## Step 2 — 在新项目里 setup

**A. CLI（任何 repo 内）**

```bash
cd /path/to/your-project
pactify setup            # 引导：建议座席 + 角色，确认后 init + wire
```

`setup` 会 scaffold `.pact/`、给每个座席烤 entry 文件（`AGENTS.md`/`CLAUDE.md`）、把 pact MCP
server 接进 agent 配置。非交互环境用 `pactify setup suggest` 看建议，再手动 `init` + `agent add`。

Dashboard 现用于 harness jobs（Jobs、Dashboard、Settings、Live），不再提供治理模式的
Setup 页面或 setup API（AJ-723）；项目初始化使用上面的 CLI。

## Step 3 — 规划任务

```bash
pactify plan "把登录加上双因子认证"   # planner agent 把一句话拆成任务图
# 生成 .pact/plan-<feature>.json + 各 task spec；默认停下等你 review
pactify plan apply <feature>          # 确认后落地为 assign
```

## Step 4 — 跑

```bash
pactify orchestrate --feature <feature> \
  --seat-kind <worker-seat>=opencode --seat-kind <reviewer-seat>=claude-code
# 自驱：worker 干活 → reviewer 独立重跑验收命令 → accept → 全 accept 后 merge
```

暂停（escalation）后修复原因，再用 `pactify orchestrate --resume` 续跑；旧 dashboard 编排 API 已删除。

## Step 5 — 交付

```bash
pactify finish --pr      # push + 开 PR
```

---

## Seat 定义详解

座席（seat）= 一个自声明的稳定身份。`pactify init` 的每个 `--seat` 是
`id:roles:entry[:kind]`：

| 字段 | 含义 | 例 |
|---|---|---|
| `id` | 座席 id（slug：小写字母/数字/`-`） | `opencode-worker` |
| `roles` | 逗号分隔：`orchestrator` / `reviewer` / `worker` | `worker` 或 `orchestrator,reviewer` |
| `entry` | 该座席 agent 的入口文件（相对路径，无 `/` 开头、无 `..`） | `AGENTS.md` |
| `kind` | （可选）agent kind，用于 wire 配置 | `opencode` |

**两条铁律**（协议保证职责分离）：① worker 不能自接受（只有该 task 的 reviewer 能 accept，且需先 checkpoint）；
② feature 的所有 task 都 accepted 才能 merge。所以 `owner ≠ reviewer`。

> 注：roster 在 `init` 时一次定下，没有 add-seat 动词——规划项目时把座席想全。

---

## 部署 dashboard（含公网）

`pactify serve` 提供 `/api/sub` 和静态资源，旧治理 API 已删除。`--seat`、`--project`
只用于定时编排。

```bash
pactify serve --addr 127.0.0.1:7777
```

浏览器写请求受 CSRF Origin guard 保护；反向代理域名需配置 `PACTIFY_ALLOWED_ORIGINS`。
公网访问仍需在反向代理（例如 Cloudflare Access）认证本人；Origin guard 不是登录认证。
定时编排仍保留，座席取 `--seat`（默认 `PACT_AGENT_ID`）。
