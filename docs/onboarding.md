# Onboarding — 5 分钟把一个新项目接入 Pactify

把任意 repo 接入 Pactify，让你已装的 agent（opencode / Claude / Gemini / Codex / Kimi …）
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
pactify agent register opencode   # 注册（或在 dashboard Settings 一键 Register）
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

**B. Dashboard 一键（serve 起来后）**

```bash
pactify serve --seat <你的座席>      # 起 dashboard（--seat 见 §部署）
```

打开 dashboard → **Setup** 视图 → 填项目路径 → 确认建议的座席/角色 → **Apply**，
一键完成 init + wire（后端 `POST /api/setup/apply`）。TOML 类 agent 会提示需手工补的配置片段。

> Setup apply 只针对**全新项目**（无 `.pact`）。已有项目用 `pactify agent add` 给座席加 agent。

## Step 3 — 规划任务

```bash
pactify plan "把登录加上双因子认证"   # planner agent 把一句话拆成任务图
# 生成 .pact/plan-<feature>.json + 各 task spec；默认停下等你 review
pactify plan apply <feature>          # 确认后落地为 assign（或 dashboard Plan 视图 Apply）
```

## Step 4 — 跑

```bash
pactify orchestrate --feature <feature> \
  --seat-kind <worker-seat>=opencode --seat-kind <reviewer-seat>=claude-code
# 自驱：worker 干活 → reviewer 独立重跑验收命令 → accept → 全 accept 后 merge
```

或 dashboard **Live** 视图 **Run** 按钮启动；暂停（escalation）时可**看 diff** + **Resume** 续跑。

## Step 5 — 交付

```bash
pactify finish --pr      # push + 开 PR（或 dashboard Ship 按钮）
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

`pactify serve` 的**有副作用写端点**（assign/accept/orchestrate run/ship/setup apply…）需要一个
acting seat 授权。本地起 serve 务必带 `--seat`：

```bash
pactify serve --addr 127.0.0.1:7777 --seat claude
```

未配 `--seat` 时写端点 fail-closed（422），只读 dashboard 仍可用。

**公网暴露**（可选）：用 Cloudflare Tunnel 把 serve 映射到域名，并**在 Cloudflare Access 加 OTP/邮箱
policy 认证本人**——Access 认证「是谁」，acting seat 授权「以哪个座席写」，两层即足够（无需额外确认弹窗）。
本机部署参考 `~/.pactify/cloudflare-tunnel-pactify.md`。
