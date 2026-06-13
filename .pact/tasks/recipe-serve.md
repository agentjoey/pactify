# recipe-serve — Recipes 的 serve 端点（给 dashboard 消费）

为已有的 `internal/recipe` 包加两个 HTTP 端点，让前端 Recipes 视图能列配方 + 预览展开。
**只新建 `internal/serve/recipes.go` + 测试 + 在 api.go 注册一行**，不碰 recipe 包本体、不碰其它 handler。

## 先读这两个文件学模式（务必照抄风格）
- `internal/serve/setup.go`：看 `registerSetupRoutes`、handler 签名 `(w http.ResponseWriter, r *http.Request)`、`writeJSON(w, http.StatusOK, dto)`、`writeErr(w, status, msg)` 的用法。
- `internal/serve/agents.go`：看 GET handler + DTO struct 的写法；POST handler 怎么 decode body（`json.NewDecoder(r.Body).Decode(&req)`）。
- `internal/serve/api.go` 的 `Handler()`：看 `s.registerXxxRoutes(mux)` 在哪注册。

## recipe 包已有的导出（直接用，别改）
```go
recipe.Names() []string                       // 排序后的配方名
recipe.Get(name string) (recipe.Recipe, bool) // Recipe{Name, Description string; Tasks []Task}
(recipe.Recipe).Expand(goal string) ([]recipe.ExpandedTask, error) // ExpandedTask{ID, Spec string; Deps []string}；空 goal 返回 error
```

## 要实现的文件

### 1) `internal/serve/recipes.go`（新建）
```go
package serve

import (
	"encoding/json"
	"net/http"

	"github.com/agentjoey/pactify/internal/recipe"
)

func (s *Server) registerRecipeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/recipes", s.handleRecipeList)
	mux.HandleFunc("POST /api/recipes/{name}/expand", s.handleRecipeExpand)
}

type recipeItemDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GET /api/recipes — 列出所有内置配方（name + description）。
func (s *Server) handleRecipeList(w http.ResponseWriter, _ *http.Request) {
	out := make([]recipeItemDTO, 0)
	for _, name := range recipe.Names() {
		if r, ok := recipe.Get(name); ok {
			out = append(out, recipeItemDTO{Name: r.Name, Description: r.Description})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type recipeExpandReq struct {
	Goal string `json:"goal"`
}

type expandedTaskDTO struct {
	ID   string   `json:"id"`
	Spec string   `json:"spec"`
	Deps []string `json:"deps,omitempty"`
}

// POST /api/recipes/{name}/expand  body {goal} — 把配方按 goal 展开成任务列表预览。
// 未知配方 → 404；空 goal → 400（Expand 返回 error）。
func (s *Server) handleRecipeExpand(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	rec, ok := recipe.Get(name)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown recipe")
		return
	}
	var req recipeExpandReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	tasks, err := rec.Expand(req.Goal)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]expandedTaskDTO, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, expandedTaskDTO{ID: t.ID, Spec: t.Spec, Deps: t.Deps})
	}
	writeJSON(w, http.StatusOK, out)
}
```
（若 `writeJSON`/`writeErr` 的精确签名与上面不符，以 setup.go/agents.go 里的实际用法为准。）

### 2) 在 `internal/serve/api.go` 的 `Handler()` 里注册（加一行）
在已有的 `s.registerSetupRoutes(mux)` 那一组旁边加：
```go
	s.registerRecipeRoutes(mux)
```

### 3) `internal/serve/recipes_test.go`（新建）
参照 `internal/serve/orchestrate_test.go` / `setup_test.go` 的 `New([]registry.Project{...})` + `httptest` 写法：
- GET /api/recipes → 200，返回 ≥3 个配方，含 `add-tests`。
- POST /api/recipes/add-tests/expand  body `{"goal":"做个X"}` → 200，返回 ≥1 个 task，spec 里含 "做个X"（{{goal}} 被替换）。
- POST /api/recipes/add-tests/expand  body `{"goal":""}` → 400。
- POST /api/recipes/nope/expand → 404。
- 用 `New(nil)`（recipe 端点不依赖 project，可不注册任何 project）。

## 验收口径
```
go build ./...                       # 干净
go test ./internal/serve/...         # 全绿
```
边界：只新建 recipes.go + recipes_test.go + api.go 注册一行。不改 recipe 包、不碰其它 serve 文件。
verify: go build ./... && go test ./internal/serve/...
