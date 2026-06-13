# agent-config-serve — per-agent 配置的 serve 端点

为 dashboard 的 Agent Config 面板加两个端点：读/写某 agent kind 的 model + 权限姿态
override。后端逻辑已存在（`agentreg` + `agentcfg`），本任务只**在已有的
`internal/serve/agents.go` 里加 2 个路由 + 2 个 handler**，不碰其它文件。

## 先读学模式
- `internal/serve/agents.go`：看 `registerAgentRoutes`（已注册 GET /api/agents 等）、`handleAgentRegister`（POST handler 怎么 decode body、`r.PathValue("kind")`、`writeJSON`/`writeErr`、注册前 `agent.Get(kind)` 校验 kind 合法）。**照抄这个文件的风格。**
- `internal/agentcfg/agentcfg.go`：`agentcfg.Resolve(kind) (Effective, bool)`，`Effective{Command string; Args []string; Model string; Scoped bool}`。
- `internal/agentreg/agentreg.go`：`Load() (Registry, error)`、`(Registry).Has(kind) bool`、`(Registry).Config(kind) (model string, allowedTools []string, restricted bool)`、`(*Registry).SetConfig(kind, model string, allowedTools []string, restricted bool) error`（**要求 kind 已注册，否则返回 error**）、`(Registry).Save() error`。
- `internal/agent/agent.go`：`agent.Get(kind) (Adapter, bool)`、`agent.Drivable(kind) bool`。

## 要加的内容（都在 agents.go 里）

### 1) 在 `registerAgentRoutes` 末尾加两行
```go
	mux.HandleFunc("GET /api/agents/{kind}/config", s.handleAgentConfigGet)
	mux.HandleFunc("POST /api/agents/{kind}/config", s.handleAgentConfigSet)
```

### 2) 两个 handler + DTO（加在 agents.go 里）
```go
type agentConfigDTO struct {
	Kind         string   `json:"kind"`
	Registered   bool     `json:"registered"`
	Drivable     bool     `json:"drivable"`
	Model        string   `json:"model"`               // 当前 override（空=未设，用默认）
	AllowedTools []string `json:"allowed_tools"`
	Restricted   bool     `json:"restricted"`
	EffModel     string   `json:"effective_model"`     // 叠加默认后的有效 model（agentcfg.Resolve）
	EffScoped    bool     `json:"effective_scoped"`
}

// GET /api/agents/{kind}/config — 读某 kind 的 override + 有效解析值。
func (s *Server) handleAgentConfigGet(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := agent.Get(kind); !ok {
		writeErr(w, http.StatusNotFound, "unknown kind")
		return
	}
	reg, err := agentreg.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	model, tools, restricted := reg.Config(kind)
	dto := agentConfigDTO{
		Kind: kind, Registered: reg.Has(kind), Drivable: agent.Drivable(kind),
		Model: model, AllowedTools: tools, Restricted: restricted,
	}
	if eff, ok := agentcfg.Resolve(kind); ok {
		dto.EffModel, dto.EffScoped = eff.Model, eff.Scoped
	}
	writeJSON(w, http.StatusOK, dto)
}

type agentConfigReq struct {
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools"`
	Restricted   bool     `json:"restricted"`
}

// POST /api/agents/{kind}/config — 写 override。kind 必须已注册（SetConfig 要求）。
func (s *Server) handleAgentConfigSet(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := agent.Get(kind); !ok {
		writeErr(w, http.StatusNotFound, "unknown kind")
		return
	}
	var req agentConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	reg, err := agentreg.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !reg.Has(kind) {
		writeErr(w, http.StatusBadRequest, "agent not registered — register it first")
		return
	}
	if err := reg.SetConfig(kind, req.Model, req.AllowedTools, req.Restricted); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := reg.Save(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 返回更新后的状态（复用 GET 的 DTO 形状）
	model, tools, restricted := reg.Config(kind)
	dto := agentConfigDTO{
		Kind: kind, Registered: true, Drivable: agent.Drivable(kind),
		Model: model, AllowedTools: tools, Restricted: restricted,
	}
	if eff, ok := agentcfg.Resolve(kind); ok {
		dto.EffModel, dto.EffScoped = eff.Model, eff.Scoped
	}
	writeJSON(w, http.StatusOK, dto)
}
```
（imports 需补 `encoding/json`、`agentcfg`；`agent`/`agentreg` 已 import。若 writeJSON/writeErr 签名不符以 agents.go 实际为准。）

### 3) 测试：在 `internal/serve/agents_test.go` 末尾加（参照该文件已有的 `New(nil)` + `httptest` + `postJSON` 写法）
- GET /api/agents/opencode/config（未注册时）→ 200，registered=false、drivable=true、effective_model 非空（默认）。
- GET /api/agents/nope/config → 404。
- POST /api/agents/opencode/config 前先 POST /api/agents/opencode/register 注册；然后 POST config body `{"model":"deepseek/custom","restricted":true,"allowed_tools":["Read","Edit"]}` → 200，返回 model=deepseek/custom、restricted=true、effective_model=deepseek/custom。
- POST /api/agents/opencode/config 在**未注册**时 → 400（"register it first"）。
- 每个子测试用独立 `t.Setenv("PACTIFY_HOME", t.TempDir())`（参照 agents_test.go 现有写法）。

## 验收口径
```
go build ./...                       # 干净
go test ./internal/serve/...         # 全绿
```
边界：只改 agents.go（加路由+handler+DTO+imports）+ agents_test.go（加测试）。不碰其它文件。
verify: go build ./... && go test ./internal/serve/...
