# 本地 agent 扫描 + 注册（机器级清单）设计

日期：2026-06-13 · 状态：LOCKED（用户已批准设计）
双重目的：① 真实功能——Pactify 安装后扫描本机 agent + 显式注册（onboarding 第一屏 + 日常管理），含后台指令 + 前端 UI；② 元目标——作为载体真实验证 `pactify orchestrate` 无人闭环（claude 编排+开发+评审 / opencode 开发），补上 orchestrate 真 LLM e2e 缺口。

## 0. 范围与三个正交状态轴

| 轴 | 级别 | 含义 | 归属 |
|---|---|---|---|
| **installed** | 机器 | 本机装没装这个 agent 工具 | 本功能（扫描） |
| **registered** | 机器 | 用户**显式**纳入"我的 agent 清单" | 本功能（注册，新增） |
| ~~wired~~ | repo | 已接线进某项目的 pact | 已有（ProbeWiring / ops Wiring 面板），**不在本功能内** |

座席 id / 角色绑定**延后**（后续 by-项目设置 + 默认座席角色配置功能）；v1 注册表是 kind 级。

**非目标**：座席/角色绑定、per-项目 agent 设置、repo 接线（已有）、orchestrate 自动读注册表当 runner 池（延后，待座席绑定）。

## 1. 数据模型

- `~/.pactify/agents.json`（镜像现有 `~/.pactify/projects.json` 的 `internal/registry`）：
  ```json
  { "agents": [ { "kind": "opencode", "label": "", "registered_at": "<ts>" } ] }
  ```
  仅存 registered 记录（kind 唯一键 + 可选 label + 时间戳）。installed 不持久化（每次扫描实时检测）。
- scan 无持久化：实时检测每个 kind 的 installed 状态。

## 2. 后端

### 2.1 扫描检测（扩展 `internal/agent`）
给每个 kind 加"检测信号"，新增 `func Scan() []ScanResult`：
```go
type ScanResult struct {
    Kind      string `json:"kind"`
    Installed bool   `json:"installed"`
    Detail    string `json:"detail"` // 命中的二进制路径 / 配置路径 / "not found"
}
```
检测规则（per kind）：
- **CLI kind**（opencode/claude-code/gemini-cli/codex-cli）：`exec.LookPath(<runner 命令>)`——复用已有 runnerCmd（opencode/claude/gemini/codex）。命中 = installed，detail = 二进制绝对路径。
- **桌面/全局 kind**（antigravity/claude-desktop/codex-app）：检测全局 config 路径（`agent.Adapter.Config()` 的 Global path，ExpandPath）或已知 app bundle 存在。命中 = installed。
- 检测信号作为 kind 元数据加在 `spec`（如 `detectBin string`（CLI）/ 复用 Config().Path（桌面））；`Scan()` 遍历 `Kinds()`。

### 2.2 agent 注册表（新 `internal/agentreg`，镜像 `internal/registry`）
```go
type Agent struct { Kind, Label string; RegisteredAt string }
type Registry struct { Agents []Agent }
func Load() (Registry, error)
func (r Registry) Save() error
func (r *Registry) Register(kind, label string) error   // kind 已存在则更新 label（幂等）
func (r *Registry) Unregister(kind string) error
```
路径 `~/.pactify/agents.json`（PACT_HOME/HOME 解析同 registry）。Register 校验 kind 是已知 kind（`agent.Get`）。

### 2.3 CLI（挂在已有 `pactify agent` 下）
- `pactify agent scan`：跑 `agent.Scan()`，表格打印每 kind 的 installed + 是否已注册。
- `pactify agent register <kind> [--label <s>]`：校验 kind 已知 → `agentreg.Register`。
- `pactify agent unregister <kind>`：`agentreg.Unregister`。

## 3. serve 端点 + 前端 UI

### 3.1 端点（serve，镜像 `wiring.go`，**机器级**不挂 /projects 下）
- `GET /api/agents`：返回 scan 结果 ∪ registry，每条 `{kind, installed, detail, registered, label}`。
- `POST /api/agents/{kind}/register`（body `{label?}`）→ agentreg.Register。
- `DELETE /api/agents/{kind}/register` → agentreg.Unregister。
路由注册镜像 `registerWiringRoutes`。author 写门同现有（仅 author 可注册/解除）。

### 3.2 UI（dashboard 新增 Agents 面板）
- 数据：`getAgents()`（GET /api/agents）。
- 空注册表 → **onboarding 态**：标题"扫描到这些 agent，选择注册以开始"，列出 installed agents。
- 非空 → 日常管理器：每行 `图标 · kind · installed 徽章 · [注册/已注册 开关]`；未 installed 的灰显 + "未检测到"。
- 复用现有 `ui/` 基础件（Button/Badge）；注册开关 = POST/DELETE + 乐观刷新。机器级面板，与项目无关（放在顶层导航/设置区，非项目内）。
- 显式、有交互、有感知——是 onboarding 第一屏（账号注册之后）。

## 4. 怎么造它 —— orchestrate 无人闭环验证（元目标）

**用 `pactify orchestrate` 真实驱动 claude + opencode 自主造这个功能**，验证：orchestrate 真 LLM 端到端 + 一个 agent 多角色 + per-task 角色翻转 + 两条铁律实战。

### 4.1 座席（多角色）
- `claude`（roles: orchestrator, reviewer, worker）：编排（任务图由我这个 claude 会话写 + assign）+ 评审 opencode 开发的 task + **亲自开发关键/复杂 task**（scan 检测核心 t1——per-kind 检测信号最需设计判断）。
- `opencode-worker`（roles: worker, reviewer）：开发多数 task + **评审 claude 开发的 t1**（满足 owner≠reviewer，角色按 task 翻转）。
- 驱动：`pactify orchestrate --seat-kind claude=claude-code --seat-kind opencode-worker=opencode`。orchestrate 用 `claude -p` / `opencode run` 拉起对应 worker/reviewer。

### 4.2 任务图（依赖链）
| task | 内容 | owner | reviewer | deps | verify |
|---|---|---|---|---|---|
| t1 scan-detect | `agent.Scan()` + per-kind 检测信号 + 单测 | **claude**（复杂核心） | opencode-worker | — | `go test ./internal/agent/ -run Scan` |
| t2 agentreg | `internal/agentreg` 注册表 + 单测 | opencode-worker | claude | t1 | `go test ./internal/agentreg/` |
| t3 cli | `pactify agent scan/register/unregister` + cli_test | opencode-worker | claude | t2 | `go test ./cmd/pactify/ -run Agent` |
| t4 serve | `/api/agents` 三端点 + 测试 | opencode-worker | claude | t3 | `go test ./internal/serve/ -run Agents` |
| t5 ui | Agents 面板（onboarding 空态 + 管理）+ vitest | opencode-worker | claude | t4 | `cd web && npx vitest run src/components/Agents` |

依赖链串行（规避 F1 单工作树）。t1 owner=claude 展示 claude-as-developer + opencode-as-reviewer 的角色翻转。

### 4.3 运行 + 观测
我写任务图（spec + assign）→ `pactify orchestrate` 跑到底。卡住 orchestrate 升级暂停（`.pact/orchestrate/escalation-*.md`），我介入根因（也是 orchestrate 真机观察点 + 协议稳定性发现，记 dogfood 续篇）。成功标准：feature 经 orchestrate 自主 shipped（最少人工——理想仅"启动 orchestrate"一步）+ 一份 orchestrate 真 LLM e2e 观察记录。

## 5. 测试（功能自身）
- Go：`agent.Scan`（各 kind 检测，LookPath/路径 mock 化以可测——检测函数注入 lookPath/statPath 以免依赖真实机器）、agentreg（Register/Unregister/幂等/未知 kind 拒绝/Load-Save round-trip）、CLI（scan 输出 + register/unregister round-trip）、serve（GET 合并 scan∪registry、POST/DELETE）。
- web：vitest Agents 面板（空态 onboarding、installed 徽章、注册开关 POST/DELETE）。
- 全部 task 规格带 `verify:` 行（orchestrate 硬门用）。
- web 构建产物入 `internal/serve/dist`，与 src 同 commit。

## 6. 工艺约束
- scan 检测函数注入文件系统/LookPath 依赖（可测，不依赖真实安装的 agent）。
- 机器级端点不挂 /projects；author 写门一致。
- 注册表镜像现有 registry 模式（HOME/PACT_HOME 解析、原子写）。
- orchestrate 驱动期间共享单工作树——串行依赖链保证至多一个 agent 动手。
