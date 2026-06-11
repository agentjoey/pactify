# Sprint 003

Goal:      Phase 2 (Pact-Base 覆盖+分发) — M2.1 跨 agent 薄封装（MCP 为主）→ M2.2 分发 → M2.3 marketplace → M2.4 官网
Period:    2026-06-10 ~ TBD
Version:   v0.3.0 (target)
Assignee:  claude (+ opencode via pact protocol)

## Context
Phase 1 / Pact-Base 完成（Sprint 002，PR #1–#5）：协议 v1 + bash 参考 + Go CLI（互操作）+
多项目 SSE dashboard + stdio MCP server。Phase 2 把"薄封装"为各 agent 做 drop-in 接入，
MCP 为主、shell 兜底，opencode 深做到端到端 dogfood。

设计/计划：`docs/superpowers/{specs,plans}/2026-06-10-m2.1-cross-agent-onboarding.{md}`

## Tasks

### T1: M2.1 跨 agent 薄封装（MCP 为主，app+CLI 双形态）[HIGH] [claude+opencode]
**Status:** ✅ Done（本地 merge 到 main，commit 990006b；exit gate ✅ PASS）
**Milestone:** M2.1
**实现**（8 个 TDD 任务，subagent 驱动 + 两阶段审查，全程绿）：
- 新包 `internal/agent`：`Adapter` 注册表，7 kind = vendor×surface，三正交轴
  （format=JSON 方言/TOML；scope=project/global；rooting=cwd/`--project`）。
- JSON merger 自动布线全部 JSON kind（含两个桌面 app：claude-desktop、antigravity）；
  codex（两面）TOML **doc-only**（不引 TOML 依赖）。幂等、非法 JSON 不覆盖、symlink 安全。
- 共享 "pact briefing"（MCP-first + shell 兜底）+ 导出 `pact.BakeManagedBlock`（拒 marker-in-body）。
- `pactify agent add <kind>`（--id/--roles/--project/--print；必填校验；写入确认，全局标 machine-global）。
- `pactify init` 第 4 段 seat 字段（kind）；前置校验 entry==kind 默认、拒桌面 kind（fail-closed）。
- `pactify mcp --project` chdir-root（桌面 app 锚定 repo；`filepath.Abs`）。
- 生成 `docs/agent-onboarding.md`，`TestDocFileInSync` 保证不漂移。
**验收门**：确定性 interop（已有 `TestFullLifecycleViaMCP`）+ stdio smoke（`tests/mcp.bats`）
+ `--project` smoke（`tests/mcp_project.bats`，从异 cwd 验锚定）。
**基线**：go 10 包 + bats 92 + vitest 11 全绿。go vet 干净。
**最终整体审查**修了 2 个 merge 前阻塞：I1 `--project` 相对路径未 abs（烤进全局配置）、
I2 桌面 kind 静默写全局文件无输出。
**CI workflow 整体留 M2.2**（本程测试只本地/dogfood 跑）。

#### Exit Gate — opencode 端到端 dogfood ✅ PASS（2026-06-10）
独立 scratch 仓 `~/AgentWorks/Code_Claude/pact-dogfood-m2`：`pactify init` 一步把 opencode 接进来
（`opencode.json` 挂 pact MCP server + `PACT_AGENT_ID=opencode`，`AGENTS.md` 烤 briefing）。
orchestrator(claude) 派 T1（写 `hello.sh`）→ 人只说一次"开始"→ opencode worker 经 pact MCP
工具自驱 join→实现→checkpoint → reviewer(claude) 独立验收 accept → merge，F1 **shipped**。
**消灭人肉中继成立**（除"开始"外无手动转述）。

**Findings：**
- **F1（PATH/分发，已临时修，待产品决策）**：agent MCP config 写的是裸 `pactify` 命令，靠 PATH 解析。
  pactify 只装在 `~/bin`（不在登录 PATH）时，opencode 起不了 MCP server 也用不了 CLI 兜底 → worker
  无法自 join，人肉中继被迫回归。**dogfood 临时修**：装到 `/opt/homebrew/bin`（上 PATH）后重启 opencode 即通。
  → **决策点**：M2.2 分发（brew/curl→PATH）本就解决此问题；或让 `pactify agent add` 把
  `os.Executable()` **绝对路径**写进 config 以摆脱 PATH 依赖（更稳，但绑定二进制位置）。带进 M2.2。
- **F2（init 工件随首个 feature 漂移，Minor）**：init 生成的入口/config 文件（AGENTS.md/CLAUDE.md/
  opencode.json）若在派活前未 commit，会被 worker 首个 checkpoint 的 `git add -A` 扫进 feature 提交，
  污染 feature diff。建议 `pactify init` 或工作流在开首个 feature 分支前先提交 scaffold 工件。低优。

### T2: M2.2 分发 + 引导 + Claude 一键 [HIGH]
**Status:** ✅ Done（PR #6 merged fa1ea66，2026-06-10；CI 首跑绿 + 分支保护已生效）
**Milestone:** M2.2
**Context:** 吸收 M2.1 F1——分发的核心交付就是把 `pactify` 放上 agent 继承的 PATH。
**Acceptance:**
- [x] GitHub Actions CI（vet/test/build + bats + vitest，tidy-diff 门/缓存/超时/并发组）+ 分支保护（EP-009，
      `test` 必过 + PR + admin 可绕，已用 gh api 生效，命令见 CONTRIBUTING）
- [x] release 流水线：GoReleaser v2（darwin/linux × amd64/arm64，sha256 checksums，trimpath，
      version/commit/date ldflags 注入 + `pactify version`/`--version`），`release.yml` on `v*` tag
- [x] `curl|sh` 安装脚本（os/arch 检测、checksum 校验、空版本守卫、sha256sum 兜底、PATH 感知、
      可测 seam PACTIFY_VERSION/DOWNLOAD_BASE/BINDIR/SOURCE_ONLY）+ `go install` 路径；
      **brew tap 留 M2.3**（spec 决策）
- [x] **F1 决策 = 裸 `pactify` + 分发上 PATH**（project config 提交进仓必须可移植，绝对路径会烂；agent 码零改动）
- [x] 三支柱新增：**B 引导**——`pactify doctor`（PATH/repo/seat/内容感知 wiring/真 MCP 握手 5 检查）+
      `pactify setup`（TTY 交互引导，非 TTY 让路，fail-closed，座位无条件采用）；**C Claude 一键**——
      `plugins/pact`（skill+MCP+SessionStart 提示 hook，`claude plugin validate` ✔）+ 同仓
      `.claude-plugin/marketplace.json`（`/plugin marketplace add agentjoey/pactify` → `/plugin install pact@pactify`）
**质量**：每任务两阶段审查（spec=opus / quality=fable5）+ fable5 终审抓 4 个 merge 前修复
（C1 旧座位污染 init、I1 MCP 检查缺失、I2 wiring 假阳性、I3 空座位）；CI 首跑暴露 dash 的
POSIX `.` 不传参问题（PACTIFY_SOURCE_ONLY env 守卫修复）。基线 go 全套 + bats 101 + vitest 11。
**Deferred minors（终审 M1–M6）**：plugin.json 版本与 release 同步机制；go-install 用户 version
fallback（debug.ReadBuildInfo）；release.yml 加测试门；checkPath 用 LookPath 对比真实解析；
spec B2 措辞（setup 只指向 doctor 不内嵌跑）；CI 跑 claude plugin validate。
**✅ v0.3.0 已发布（2026-06-10）**：tag → release workflow 绿 → 4 平台产物 + checksums 上线；
真实 `curl|sh` 端到端实测通（latest 解析/下载/checksum 校验/安装/版本注入 `pactify 0.3.0 (27a059a)`）；
本机 /opt/homebrew/bin 已用正式渠道升级，doctor PATH ✓ + MCP 握手 ✓。

### T5: Phase 3 / M3.1+M3.2 — Squad 编排画布 + 派发 [HIGH]
**Status:** ✅ Done（PR #10 merged 6081bf6，2026-06-11 夜间包；CI test+site 双绿）
**Milestone:** M3.1 + M3.2（M3.3/M3.4 留后）
**交付**：目录感知引擎 `pact.At(dir).As(seat)`（旧套件一行不改全绿）；`deps` 协议扩展（additive v1：
assign 校验 存在/同feature/无环DFS，join+checkpoint 双点硬门控，无 deps 日志与 bash 字节对齐，
schema+addendum）；serve author API（--seat 行动座位逐请求验 roster、按项目互斥、verbs 4 端点
引擎错误 422 原文、tasks 409 防 clobber、layout sidecar 原子写）；React Flow 画布（角色色节点/
依赖边/build mode 本地草稿/拖拽派发/评审流/SSE toasts/停滞点/observe-only）。
**E2E smoke 实测**：API 全生命周期 → shipped + layout 回写。
**质量**：逐任务双审查 + fable5 终审；merge 前修了 9 个真问题（拖拽坐标系、spec 传 md 体击穿
STATE、toast 轰炸、评审流不可达、品牌色漂移、clobber 竞态、绝对路径泄漏、join 门时序洞
（checkpoint 补门关死）、draft 边消失）。基线：go 全套+race / bats 105 / vitest 49。
**Deferred（PR #10 在案）**：跨进程锁（advisory flock）、mutex 并发 pin 测试、M3.3/M3.4。

### T6: 站点 v2 [HIGH]
**Status:** ✅ Done（PR #9 merged a8c846c，2026-06-11 夜间包；已上线 pactify.dev 实测）
**交付**：11 段营销页（光缆 hero 三色拧缆+真逐字打字机、persona 座位+铁律、agent marquee 真锚点、
角色色走查轮播、Phase3 画布占位、why cards、产品阶梯、philosophy、CTA、4 栏 footer）+
brand 色-角色体系全站贯通 + check-dist 升至 11 断言。质量过程修了 CLS 坍塌、CTA 双击毁命令、
标题语义、中文批注泄漏、dasharray 常数等真问题。

### T7: Squad M3.3a — Ops 面板（注册/接线/座位出处）[HIGH]
**Status:** ✅ Done（PR #14 merged，2026-06-11；用户验收测试反馈直接驱动）
**交付**：UI 运行时注册/移除 repo（动态 watcher）+ 每 kind 接线状态与一键接线（桌面类真实路径
知情同意门，codex doc-only 片段，probe 与 doctor 共源）+ 座位出处（join 可选 client 元数据：
MCP clientInfo / pactify-cli，addendum 注明 advisory；Seats 面板 last join + before→after 变更告警）。
serve 并发改造（RWMutex,race 干净）。基线 bats 109 / vitest 69。
**Deferred**：移除项目时关 SSE 订阅、坏日志 500 信封、M3.3b 通讯可视化。

### T3: M2.3 Claude marketplace 上架 + 其余 agent 一键 + brew tap [MED]
**Status:** 🔲 Todo（Claude 插件模板已在 M2.2 打样，本任务=提交社区 marketplace 审核 + Codex/opencode/Gemini 一键 + brew tap）
**Milestone:** M2.3

### T4: M2.4 pactify.dev 官网 v1 [MED]
**Status:** ✅ **全量上线**（PR #7 代码 + Vercel 项目/构建配置核验无误 + pactify.dev/www DNS 生效；真 curl|sh 从 pactify.dev 装 v0.3.0 实测通；PR #8 把全仓安装 URL 切到 pactify.dev）
**Milestone:** M2.4
**交付**：Astro 6 静态站 `site/`——终端真实派 landing（打字 hero/双线合一/§1§2 条款/quickstart/入座区/可访问复制 CTA，动效全 CSS+reduced-motion 降级）；/protocol + /onboarding 构建时直读仓内规范（glob loader base ../docs，零拷贝）；/install.sh = 真安装器（prebuild 同步 + check-dist 字节守卫）；CI site job（informative）；Vercel runbook。
**质量**：brainstorm 用可视化伴侣做了视觉调研板+结构 mockup（用户选向：终端派+双线/入座）；逐任务双审查 + fable5 终审（抓 CTA 双击毁命令真 bug、dasharray 常数 bug、移动端 CTA 碎裂等，全修）。
**Deferred（PR #7 描述在案）**：og:image、/protocol TOC、README/插件 hook URL 切到 pactify.dev、site job 升 required、hero 版本串随 release 同步。
