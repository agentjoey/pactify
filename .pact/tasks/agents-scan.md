# Task scan · scan-detect

**Feature:** agents · **Owner:** claude · **Reviewer:** opencode-worker · **Deps:** 无

## 目标
检测本机装了哪些 agent kind。必须可测——检测依赖（LookPath / 文件存在）注入，不依赖真实安装的 agent。

## 改文件
- `internal/agent/agent.go`：spec 加检测字段 `detectBin string`（放在 runnerArgs 之后），所有 registry 条目补值：opencode→"opencode"、claude-code→"claude"、gemini-cli→"gemini"、codex-cli→"codex"；antigravity/claude-desktop/codex-app→""（桌面 kind，靠 Config().Path 全局存在检测）。
- 新建 `internal/agent/scan.go` + `internal/agent/scan_test.go`。
- **不碰** runner/probe/briefing 逻辑。

## 契约
```go
type ScanResult struct {
    Kind      string `json:"kind"`
    Installed bool   `json:"installed"`
    Detail    string `json:"detail"` // 命中二进制路径 / 配置路径 / "not found"
}
type scanProbe struct {
    lookPath func(string) (string, error) // 生产 = exec.LookPath
    statPath func(string) bool            // 生产 = os.Stat 存在性（ExpandPath 后）
}
func Scan() []ScanResult                  // 生产入口：默认探针
func scanWith(p scanProbe) []ScanResult   // 可测内核
```
检测规则：detectBin 非空 → lookPath(detectBin) 命中即 installed（detail=路径）；detectBin 空（桌面 kind）→ statPath(ExpandPath(Config().Path)) 存在即 installed（detail=路径）。结果按 Kinds() 顺序。

## 验收
scan_test.go：注入 fake probe，覆盖 CLI 命中 / CLI miss / 桌面命中 / 桌面 miss，断言 ScanResult.Installed/Detail；Scan() 默认探针 smoke 不 panic。

## verify
```
go test ./internal/agent/ -run Scan
```

## 完成方式
TDD。你是座席 claude（owner）。`pactify checkpoint scan` 附 verify 输出。不要自标 accepted——reviewer 是 opencode-worker。
