# Agent 扫描 + 注册 Implementation Plan（orchestrate 驱动）

> **For agentic workers:** 本计划【不走】subagent-driven-development。实现者是 `pactify orchestrate` 拉起的真 LLM（claude=claude-code / opencode-worker=opencode），读 `.pact/tasks/` 规格自主开发。本计划由 orchestrator（claude，本 session）执行 §A 运行手册：写任务图 + assign + 跑 orchestrate + 观测升级。§B 是写入 `.pact/tasks/` 的任务规格。

**Goal:** 给 pactify 加"本机 agent 扫描 + 机器级注册"（后台 CLI + serve 端点 + 前端 Agents 面板），并用 `pactify orchestrate` 真实无人闭环驱动 claude+opencode 造出来——验证 orchestrate 真 LLM e2e + 一个 agent 多角色 + per-task 角色翻转。

**Architecture:** spec = `docs/superpowers/specs/2026-06-13-agent-scan-register-design.md`（LOCKED）。机器级 installed（扫描）+ registered（`~/.pactify/agents.json`），与 repo wiring 正交。后端镜像 `internal/registry` + `internal/serve/wiring.go`；前端镜像 ops Wiring 面板。

**Tech Stack:** Go（internal/agent 扩展、新 internal/agentreg、internal/serve、cmd/pactify）+ React/Vite/vitest（web）。

**约定:** 实现者走 TDD；每个 task 规格带机器可读 `verify:` 行（orchestrate 硬门用）；web src 变更同 commit 重建 `internal/serve/dist`；scan 检测函数注入 LookPath/文件依赖以可测；feature 经 `pactify merge agents` 合入；协议/CLI 卡点由 orchestrator 介入修。

---

## §A — Orchestrator 运行手册（claude 执行）

### A0 准备
- [ ] **A0.1 基线 + 二进制**
```bash
cd /Users/xtation/AgentWorks/Code_Claude/pactify
git checkout main && git pull --ff-only && go build -o /opt/homebrew/bin/pactify ./cmd/pactify
git checkout -- .pact/STATE.yml .pact/log.jsonl 2>/dev/null   # 清掉浮动的 dogfood 工作树残留
git status --short
```
- [ ] **A0.2 复用现有 .pact 项目**（dogfood 已 init，roster 有 `claude`[orchestrator,reviewer] + `opencode-worker`[worker]）。**不 re-init**（init 会清空 log.jsonl，丢 relay 协调史，#4）。角色是 advisory——assign 只校验 owner≠reviewer（rules.go checkAssign），不强制 owner 有 worker 角色 / reviewer 有 reviewer 角色，所以现有 roster 足够支撑多角色：claude 可当 t1 的 owner、opencode-worker 可当 t1 的 reviewer。确认 roster：`pactify status | head -8`。
- [ ] **A0.3 建 feature 分支 + 写 5 份任务规格**到 `.pact/tasks/`（§B 全文），commit 到 main（docs/chore 直推）。

### A1 派发任务图（多角色 + 角色翻转）
- [ ] **A1.1 assign t1-t5**（确切子命令以 `pactify assign --help` 为准；feature=`agents` branch=`feat-agents`）：
  - t1 scan-detect：owner=**claude**，reviewer=opencode-worker（claude-as-developer 展示）
  - t2 agentreg：owner=opencode-worker，reviewer=claude，deps=t1
  - t3 cli：owner=opencode-worker，reviewer=claude，deps=t2
  - t4 serve：owner=opencode-worker，reviewer=claude，deps=t3
  - t5 ui：owner=opencode-worker，reviewer=claude，deps=t4
  commit assign 事件。

### A2 跑 orchestrate（无人闭环）
- [ ] **A2.1** 启动驱动器：
```bash
pactify orchestrate --feature agents \
  --seat-kind claude=claude-code \
  --seat-kind opencode-worker=opencode
```
  orchestrate 自动：t1 拉 claude(claude -p)开发→拉 opencode 评审→...→t5→全 accepted+硬门→`merge agents`。
  - 先 `--dry-run` 看首个动作正确（应是 RunOwner t1 seat=claude）。
- [ ] **A2.2 观测**：orchestrate 串行跑；卡住写 `.pact/orchestrate/escalation-*.md` 并暂停。orchestrator（我）介入：读升级记录 → 根因（协议 bug / 规格不清 / agent 卡）→ 修 → `pactify orchestrate --feature agents --seat-kind ...` 续跑。每次升级 + 介入记入观测（§C）。

### A3 收尾
- [ ] **A3.1** feature shipped 后：`go build ./... && go test ./...` + `cd web && npx vitest run` 全绿；重建二进制。
- [ ] **A3.2** 写 orchestrate 真 LLM e2e 观测记录 `docs/dogfood/2026-06-13-orchestrate-e2e-log.md`（每棒 owner/reviewer、升级次数+介入、多角色/翻转是否如期、最少人工程度、新协议发现）。
- [ ] **A3.3** sprint + memory 更新；PR feature 分支（若 orchestrate 的 merge 是本地 main，则推 origin 走 PR 决策同 dogfood）。

### 协议/CLI 修复纪律
orchestrate 或 agent 卡在协议/CLI bug：orchestrator 开 `fix/*` 独立分支修 + 回归测试 + PR，记观测，再续跑。worker 卡协议 bug 上 = orchestrator 先修协议（不算替 worker 写实现）。

---

## §B — 五份 Pact 任务规格（写入 .pact/tasks/）

> 给【orchestrate 拉起的真 LLM】看的契约：目标 / 文件 / 确切签名 / 验收 / verify 行。worker 用 TDD 自写实现；reviewer 跑 verify 命令独立验证后 accept/changes。

### t1 · scan-detect（owner=claude, reviewer=opencode-worker）

**目标**：检测本机装了哪些 agent kind。可测——检测依赖（LookPath / 文件存在）注入，不依赖真实安装的 agent。

**改文件**：`internal/agent/agent.go`（spec 加检测字段）+ 新建 `internal/agent/scan.go` + `internal/agent/scan_test.go`。**不碰** runner/probe/briefing。

**契约**：
```go
// agent.go：spec 加一个检测二进制字段（CLI kind 用），桌面 kind 留空靠 config 路径。
//   spec 加: detectBin string  // CLI 二进制名（LookPath）；"" = 桌面 kind 靠全局 config 路径
//   registry 补值：opencode→"opencode", claude-code→"claude", gemini-cli→"gemini",
//                  codex-cli→"codex"；antigravity/claude-desktop/codex-app→""（靠 Config().Path 全局存在）
// scan.go：
type ScanResult struct {
    Kind      string `json:"kind"`
    Installed bool   `json:"installed"`
    Detail    string `json:"detail"` // 命中二进制路径 / 配置路径 / "not found"
}
// Scan 用注入的探针检测每个 Kinds()；生产探针 = exec.LookPath + os.Stat。
type scanProbe struct {
    lookPath func(string) (string, error) // exec.LookPath
    statPath func(string) bool            // os.Stat 存在性（ExpandPath 后）
}
func Scan() []ScanResult                  // 生产入口：用默认探针
func scanWith(p scanProbe) []ScanResult   // 可测内核
```
**检测规则**：detectBin 非空 → `lookPath(detectBin)` 命中即 installed（detail=路径）；detectBin 空（桌面 kind）→ `statPath(ExpandPath(Config().Path))` 存在即 installed。结果按 Kinds() 顺序。

**验收测试**（scan_test.go）：注入 fake probe——某些 kind 的 lookPath 命中、某些 miss；桌面 kind 的 statPath 命中/miss；断言 ScanResult 的 Installed/Detail 正确，覆盖 CLI 命中、CLI miss、桌面命中、桌面 miss。`Scan()`（默认探针）至少不 panic（smoke）。

**verify:** `go test ./internal/agent/ -run Scan`

### t2 · agentreg（owner=opencode-worker, reviewer=claude, deps=t1）

**目标**：机器级 agent 注册表，镜像 `internal/registry`。

**新建**：`internal/agentreg/agentreg.go` + `agentreg_test.go`。

**契约**（镜像 registry：file() 用 PACTIFY_HOME 否则 ~/.pactify，Load 缺文件=空，Save 建父目录原子写）：
```go
package agentreg
type Agent struct {
    Kind         string `json:"kind"`
    Label        string `json:"label,omitempty"`
    RegisteredAt string `json:"registered_at"`
}
type Registry struct { Agents []Agent `json:"agents"` }
func Load() (Registry, error)
func (r Registry) Save() error
func (r *Registry) Register(kind, label, ts string) error  // ts 由调用方传入（可测，不在包内取时间）；kind 必须是 agent.Get 已知 kind，否则 error；已存在则更新 label（幂等）
func (r *Registry) Unregister(kind string) error           // 不存在 = 无声成功（幂等）
func (r Registry) Has(kind string) bool
```
注册表文件 `~/.pactify/agents.json`（PACTIFY_HOME 优先，同 registry.file()）。

**验收测试**：Register 未知 kind→error；Register 已知 kind→Has 真 + 文件含该 kind；重复 Register 更新 label 不重复条目；Unregister→Has 假；Unregister 不存在不报错；Load/Save round-trip（用 t.Setenv("PACTIFY_HOME", t.TempDir())）。ts 传固定串。

**verify:** `go test ./internal/agentreg/`

### t3 · cli（owner=opencode-worker, reviewer=claude, deps=t2）

**目标**：`pactify agent scan/register/unregister` 子命令，挂在已有 `pactify agent` 下（与 add/docs 并列，见 cmd/pactify/cmd_agent.go newAgentCmd）。

**改文件**：`cmd/pactify/cmd_agent.go`（加三个子命令）+ `cmd/pactify/cli_test.go`（加 smoke）。

**行为**：
- `pactify agent scan`：`agent.Scan()` → 表格打印每 kind：`<kind>  installed|—  <detail>  [registered]`（注册状态查 agentreg.Load().Has）。
- `pactify agent register <kind> [--label <s>]`：校验 `agent.Get(kind)` 已知 → `agentreg.Load()` → `Register(kind,label, time.Now().Format(...))` → `Save()`。未知 kind 报错列出 `agent.Kinds()`。
- `pactify agent unregister <kind>`：Load → Unregister → Save。
- 时间戳在 CLI 层取（`time.Now()`），传给 agentreg（包内不取时间）。

**验收测试**（cli_test，建 binary 跑）：`agent scan` 退出 0 且输出含某 kind 行；`agent register opencode` 后 `agent scan` 标该 kind registered；`agent unregister opencode` 后不再 registered；`agent register bogus` 非零退出 + 错误列出已知 kinds。用 `PACTIFY_HOME=<tempdir>` 隔离。

**verify:** `go test ./cmd/pactify/ -run Agent`

### t4 · serve（owner=opencode-worker, reviewer=claude, deps=t3）

**目标**：机器级 agent 端点，镜像 `internal/serve/wiring.go` 的注册模式。

**改文件**：新建 `internal/serve/agents.go`（+ `agents_test.go`）；在 `api.go` 的 Handler 注册处加 `s.registerAgentRoutes(mux)`（紧挨 registerWiringRoutes）。

**端点**（机器级，**不挂** /projects/{id}）：
- `GET /api/agents`：合并 `agent.Scan()` 与 `agentreg.Load()`，每条 `{kind, installed, detail, registered, label}`（JSON）。
- `POST /api/agents/{kind}/register`（body `{"label":"..."}` 可空）：校验 kind 已知 → agentreg.Register（serve 层取 ts）→ Save → 200。author 写门同 wiring（仅 author 可写；非 author 403/404 同现有约定——参照 handleWire）。
- `DELETE /api/agents/{kind}/register`：agentreg.Unregister → Save → 200。
- 错误：未知 kind → 400 + 引擎风格错误；写门未过 → 同 wiring。

**验收测试**（agents_test.go，httptest）：GET 返回含已知 kind 且 registered 反映 agentreg 状态（用 PACTIFY_HOME tempdir）；POST register 后 GET 显示 registered=true + label；DELETE 后 registered=false；POST 未知 kind→400；非 author 写被拒（同 wiring 测试模式）。

**verify:** `go test ./internal/serve/ -run Agents`

### t5 · ui（owner=opencode-worker, reviewer=claude, deps=t4）

**目标**：dashboard Agents 面板——空注册表即 onboarding 第一屏，非空即管理器。

**改文件**：新建 `web/src/components/Agents.tsx`（+ `Agents.test.tsx`）；`web/src/lib/api.ts` 加 `getAgents()`/`registerAgent(kind,label?)`/`unregisterAgent(kind)`；接入顶层导航（机器级，**非项目内**——放 App 的设置/顶层区，参照现有顶层面板挂法）。

**行为**：
- `getAgents()` → GET /api/agents（类型 `AgentRow{kind, installed, detail, registered, label}`）。
- 注册表空（无 registered）→ onboarding 态：标题"扫描到这些 agent，选择注册以开始"+ installed agents 列表。
- 非空 → 管理器：每行 `kind · installed 徽章（已装/未检测到）· [注册/已注册 开关]`。未 installed 灰显。
- 开关 = registerAgent/unregisterAgent + 乐观刷新。复用 `ui/` 基础件（Button/Badge）。observe 模式只读（开关禁用），同现有 author 门。

**验收测试**（Agents.test.tsx，vitest + mock fetch）：空 registry 渲染 onboarding 文案 + installed agent 行；点"注册"调 registerAgent 且乐观显示已注册；点"已注册"调 unregisterAgent；未 installed 行灰显且开关禁用；observe（author=false）开关禁用。

**verify:** `cd web && npx vitest run src/components/Agents`
（注：t5 改 web/src → 同 commit 重建 `internal/serve/dist`：`cd web && npm run build`。）

---

## Self-Review 结论
- **spec 覆盖**：§1 数据模型→t2；§2.1 扫描→t1；§2.2 注册表→t2；§2.3 CLI→t3；§3.1 端点→t4；§3.2 UI→t5；§4 多角色/翻转/orchestrate 驱动→§A 运行手册（t1 owner=claude+reviewer=opencode 翻转）；§5 可测性（注入探针）→t1/t2 契约；§6 工艺→各 verify + dist 重建。无缺口。
- **占位符**：CLI 确切子命令参数标"以 --help 为准"（dogfood 同款，受测项）；其余无 TBD。
- **类型一致**：`ScanResult`/`scanProbe`（t1）、`agentreg.Agent/Registry/Register(kind,label,ts)`（t2）、CLI 调 agent.Scan+agentreg（t3）、serve 合并二者（t4）、`AgentRow`（t5）跨 task 一致；镜像的 registry.file()/Load/Save 模式对照真实代码。
- **角色铁律**：t1 owner=claude≠reviewer=opencode-worker；t2-t5 owner=opencode-worker≠reviewer=claude——全满足 owner≠reviewer，且演示一个 agent 多角色 + per-task 翻转。
