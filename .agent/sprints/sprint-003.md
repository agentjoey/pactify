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

### T8: Squad M3.3b — 通讯可视化（waits overlay + 回放刮擦器）[HIGH]
**Status:** ✅ Done（PR #15 merged，2026-06-11）
**交付**：serve 只读端点 `GET /timeline`（事件索引,1-based n,task/feature omitempty）+
`state?at=N`（前缀折叠回放,clamp,无 at 字节级不变有回归测试）；canvas comms 开关
（虚线角色色等待边+原因 chip:awaiting review/changes requested/blocked by dep,
空闲坐席变暗,阻塞琥珀描边,未入座警告徽章,图例;display-only lens 不碰 layout.json）；
ReplayBar 全状态时间旅行（滑块/±1/LIVE,回放模式全只读,历史快照绕过 applyState,
toast/脉冲仅 live）；live 脉冲（状态变更 900ms 角色色光晕,reduced-motion 关闭,
首快照/回放不触发）。零协议/引擎/schema 改动。基线 bats 111 / vitest 99 / go 全绿。
**质量**：逐任务双审查折叠修复（C1 错误信封统一+测试卫生;C2 死 memo 删除——unmet
判定已涵盖传递性阻塞;C3 空板闪烁/stale 响应/timeline 不刷新三连修;C4 不可达
seat 徽章删除+display memo 合一+roleColorVar 共源）+ fable5 终审 APPROVED。
**插曲**：C4 实现 agent 中途断连(socket),半成品续派补完（缺 CSS/测试 + SeatNode
缺 target handle 真 bug）。

### T9: Dashboard v2 — 商业化 SaaS 级 polish(三波 + hotfix)[HIGH]
**Status:** ✅ Done（PR #16 hotfix + #17 W1 + #18 W2 + #19 W3,全部 merged,2026-06-11/12）
**设计**:brainstorm 决策 B 旗舰版 + Linear 精致密度感 + ⌘K;可视化伴侣出 5 块 mockup 板
逐板拍板(B·Indigo 色板/蚁群八品级头像 45°/Office 作战室/⌘K+时间轴+详情面板),
视觉常数锁进 spec,mockup 入库 docs/superpowers/mockups/dashboard-v2/ 作实装 markup 源。
**W0 hotfix(#16)**:验收阻塞三连——空仓 features 序列化 null 崩画布(真凶,Go 侧+前端
双修)/草稿切视图即丢(提升 App 层)/draft 自动编号。
**W1(#17)**:tokens+自托管字体(@theme static 防 tree-shake——审查抓到七成 token 被
裁掉的真问题)/ui 九件套基础件/蚁群头像系统/TaskCard v2+看板/TopBar v2+动态标题。
**W2(#18)**:Plan 画布质感/AntEdge 爬线(cap6)/连线手感重做(验收反馈 2.5)/右键菜单+
吸附+框选(审查抓到 RF 内置 Backspace 可删已提交节点的 Critical)/office.ts 纯派生
(审查牵出 changes_requested 从所有抽屉消失的语义洞)/OfficeView 默认模式(修复 agent
整改在途动画坐标空间错位)/任务详情面板(动词语义逐字节保真)。
**W3(#19)**:⌘K(cmdk 唯一新依赖)/回放时间轴+?at 深链(抓到 WebKit replaceState 限频)/
人话化报错(11 条引擎原文核验)/骨架屏/空仓引导/聚焦模式 + 生命周期边角四连修。
**基线**:vitest 99→358 / go 全绿 / bats 111;每个 web commit 带重建 dist。
**插曲**:opus 实现 agent socket 断连 4 次(T3/C4/T15 半成品续派补完,T14/T15 空启动重派)。
**Deferred**:千级 tick 渲染降采样、Canvas.tsx 体量二刀(useBuildMode/样式拆分)、
⌘K 画布 fitView 聚焦动画、回放跳转入 ⌘K(需 timeline jump 钩子)。

### T3: M2.3 Claude marketplace 上架 + 其余 agent 一键 + brew tap [MED]
**Status:** 🔲 Todo（Claude 插件模板已在 M2.2 打样，本任务=提交社区 marketplace 审核 + Codex/opencode/Gemini 一键 + brew tap）
**Milestone:** M2.3

### T4: M2.4 pactify.dev 官网 v1 [MED]
**Status:** ✅ **全量上线**（PR #7 代码 + Vercel 项目/构建配置核验无误 + pactify.dev/www DNS 生效；真 curl|sh 从 pactify.dev 装 v0.3.0 实测通；PR #8 把全仓安装 URL 切到 pactify.dev）
**Milestone:** M2.4
**交付**：Astro 6 静态站 `site/`——终端真实派 landing（打字 hero/双线合一/§1§2 条款/quickstart/入座区/可访问复制 CTA，动效全 CSS+reduced-motion 降级）；/protocol + /onboarding 构建时直读仓内规范（glob loader base ../docs，零拷贝）；/install.sh = 真安装器（prebuild 同步 + check-dist 字节守卫）；CI site job（informative）；Vercel runbook。
**质量**：brainstorm 用可视化伴侣做了视觉调研板+结构 mockup（用户选向：终端派+双线/入座）；逐任务双审查 + fable5 终审（抓 CTA 双击毁命令真 bug、dasharray 常数 bug、移动端 CTA 碎裂等，全修）。
**Deferred（PR #7 描述在案）**：og:image、/protocol TOC、README/插件 hook URL 切到 pactify.dev、site job 升 required、hero 版本串随 release 同步。

### T10: Canvas P0 — 交互地基重构(位置物化 + e2e 验收门)[HIGH]
**Status:** ✅ Done（PR #20 W1 + #21 W2 + #22 W3,2026-06-12）
**背景**:Dashboard v2 验收实测四个基础交互全废——拖一个 box 其他乱跳/拉线完全不可交互/
Office(默认落地)零操作面/Office 无缩放。根因复盘:①位置被当"派生数据"每渲染重算
(deriveFlow 碰撞避让级联挤压);②jsdom 测试桩(measured/handles 假几何)混入生产路径,
v12 源码证实会整体覆盖 DOM 真实锚点;③CSS 用了 v11 时代死类名(.connecting 从不被应用);
④Toolbar/Hud 只挂 plan。流程教训:393 个 jsdom 测试全绿但交互结构性测不出来。
**设计**:研究先行(Dify 画布源码 + RF v12 官方文档/源码两路并查)→ spec 锁定
"位置创建时物化一次"模型(Dify 同款)。
**W1(#20)**:layout v2(子节点父相对)+deriveGraph/placeNew/mergeNodes 纯函数层+Canvas
管线切换+条形 handle/v12 类名/自定义连接线。审查拦下:项目切换竞态(旧项目坐标会 PUT 进
新项目,layoutLoaded 门修)/多选拖拽+draft 派发互吃/connecting 类被受控 className 冲掉。
**W2(#21)**:Toolbar 双模式+Hud/minimap 进 Office+desk 物化(坐席加入不挤桌)+dock 空态
+office 右键菜单。审查拦下:家具面板右键漏 pane 菜单/desk 整组重建。
**W3(#22)**:Playwright e2e 门(mock server 真形状对齐 dto.go+SSE 钩子)+7 条回归用例
(四个用户报告一一对应+两条负向+创建闭环)+CI e2e job 必过门。**e2e 首跑即抓到真 bug**:
isValidConnection 拦截后 onConnect 的三条 notice 分支全部不可达(提示从未真正出现过),
改 onConnectEnd 路由。vitest 393 + e2e 7×2 + go test 全绿。
**插曲**:opus 实现 agent 断连 ×3(T2/T4/T5)+会话限额 ×1,均半成品续派补完。

### T11: 无 UI dogfood — pact 自托管交付 M3.4 relay [HIGH]
**Status:** ✅ Done（feature relay shipped 进 main，2026-06-13）
**背景**：UI 端验收差，战略转向先跑通无 UI 全链路稳定性。pactify 首次用自己的协议开发自己。
**阵容**：claude 编排+评审 / opencode-worker 交付 / （antigravity 第三家因 GUI 无法无人驱动，本轮放弃）。
**交付**：M3.4 relay 接口（serve watcher 旁路事件→可配置 HTTP POST，best-effort 异步、失败隔离）。3 棒依赖链 t1 relay-client → t2 watcher-hook → t3 集成+文档，全经 assign→checkpoint→review→accept→merge。
**关键转折**：用户两次点破"人肉中继没消灭"——orchestrator 缺自主观测(#6)、一次性 worker 致人退化成调度器(#9)。运营层解法落地：orchestrator 用 `opencode run` 非交互拉起 worker + 后台观测自动接住，t2/t3 全无人闭环。
**产出 = stability 报告**（docs/dogfood/2026-06-13-stability-log.md，10 条发现）：协议核心硬（生命周期/铁律/返工回路/自主闭环成立），最大缺口 = 缺产品级编排驱动（#9，下轮 `pactify orchestrate`/worker 守护）。次要：checkpoint CommitAll 扫工具垃圾(#8)、软链 entry 不兼容(#5)、F1 单树(#7)、GUI agent 进不了无人闭环(#0/#1)。
