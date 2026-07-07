# Task doctor-setup-bridge (A3-doctor) — pactify doctor --setup-bridge + 桥预检

## 目标
让 claude 深度集成桥在新机器可用:`pactify doctor --setup-bridge` 一次性物化
`bridge/claude-host/` 的 node 依赖(`npm ci`);doctor 常规检查里加 node + 桥依赖预检。
纯加性:不带 flag 时 doctor 行为不变(已有 --json 也不受影响)。

## 改文件(仅这些 + 测试)
- `internal/doctor/bridge.go`(新增:node 检查 + 桥依赖检查 + setup 逻辑)
- `cmd/pactify/cmd_doctor.go`(加 --setup-bridge flag 分支)
- `internal/doctor/bridge_test.go`(新增)

## 契约
### internal/doctor/bridge.go
- `func BridgeChecks(repoRoot string) []Check`:返回两条 Check(复用现有 `Check{Name,OK,Detail}`,
  已有 json tag):
  1. `cli node: present` — `exec.LookPath("node")` 有则 OK + 版本(可选 `node --version`),无则
     !OK + "install Node.js (the claude cockpit bridge needs it)"。
  2. `claude bridge: deps` — 检查 `<repoRoot>/bridge/claude-host/node_modules/@anthropic-ai/claude-agent-sdk`
     目录存在则 OK "bridge deps materialized",否则 !OK "run `pactify doctor --setup-bridge`"。
  （repoRoot 定位:调用方给;doctor 命令用 cwd 或 exe 上溯找含 bridge/claude-host 的目录——
   给一个 `func FindRepoRoot(start string) (string, bool)` 上溯找 `bridge/claude-host/package.json`,
   **循环必须在文件系统根终止**:`parent==dir` 即停,别写成 `dir!=""`(filepath.Dir("/")=="/" 会死循环)。）
- `func SetupBridge(repoRoot string, out io.Writer) error`:在 `<repoRoot>/bridge/claude-host` 跑
  `npm ci`(若无 package-lock 则 `npm install`),stdout/stderr 接到 out;成功打印 "bridge deps installed"。
  找不到 bridge 目录返回清晰 error。

### cmd/pactify/cmd_doctor.go
- 加 `var setupBridge bool` + `--setup-bridge` flag "materialize the claude cockpit bridge node deps (npm ci)"。
- RunE 开头:若 setupBridge → 定位 repoRoot(FindRepoRoot(cwd),失败再试 exe 上溯)→ 调 SetupBridge,
  打印结果,return(不跑常规 doctor)。
- 常规 doctor(非 setup-bridge):把 `BridgeChecks(repoRoot)` 的结果 append 进 checks 列表
  (与现有 doctor.Run + checkMCP 一起),这样 doctor 文本/JSON 都带上 node+bridge 两项。
  repoRoot 定位失败则跳过 bridge 检查(不报错,老环境无 bridge 目录也不炸)。

## 测试(bridge_test.go)
- `FindRepoRoot`:造 t.TempDir()/bridge/claude-host/package.json,从子目录能上溯找到;根终止不死循环
  (从 "/" 之类调用不 hang——可用一个已知无 bridge 的临时树断言返回 false 且快速返回)。
- `BridgeChecks`:构造有/无 node_modules/@anthropic-ai/claude-agent-sdk 的假 repoRoot,断言 deps 检查
  OK/!OK 正确;node 检查按 LookPath 结果(node 在 PATH 时 OK)。
- （SetupBridge 真跑 npm 不在单测内做——太重;可测"bridge 目录不存在时返回 error"。）

## 验收 / Acceptance(视角: maintainability — 加性、根终止不死循环、老环境无 bridge 不炸)
- reviewer 独立跑 verify + 真机 `pactify doctor --setup-bridge` 由 reviewer 亲验(幂等物化)。

## verify
verify: go build ./... && go test ./internal/doctor/ ./cmd/pactify/
