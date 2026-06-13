# Task t2 · agentreg

**Feature:** agents · **Owner:** opencode-worker · **Reviewer:** claude · **Deps:** t1

## 目标
机器级 agent 注册表，镜像 internal/registry（file() 用 PACTIFY_HOME 否则 ~/.pactify，Load 缺文件=空，Save 建父目录原子写）。

## 改文件
新建 `internal/agentreg/agentreg.go` + `agentreg_test.go`。

## 契约
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
func (r *Registry) Register(kind, label, ts string) error // ts 调用方传入（包内不取时间）；kind 必须 agent.Get 已知，否则 error；已存在则更新 label（幂等，不重复条目）
func (r *Registry) Unregister(kind string) error          // 不存在=无声成功
func (r Registry) Has(kind string) bool
```
文件 `~/.pactify/agents.json`，PACTIFY_HOME 优先（同 registry.file()）。

## 验收
Register 未知 kind→error；已知→Has 真 + 文件含 kind；重复 Register 更新 label 不重复；Unregister→Has 假；Unregister 不存在不报错；Load/Save round-trip。用 `t.Setenv("PACTIFY_HOME", t.TempDir())` 隔离，ts 传固定串。

## verify
```
go test ./internal/agentreg/
```

## 完成方式
TDD。你是座席 opencode-worker。`pactify checkpoint t2` 附 verify 输出。不要自标 accepted。
