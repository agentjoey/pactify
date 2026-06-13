# recipe-core — orchestrate 动作配方（任务图模板）

把常见多 agent 工作流做成命名配方：用户给一句话目标 + 配方名，配方展开成一组 pact 任务
spec（含 owner/reviewer/deps 占位），把门槛从"手写任务图"降到"选配方"。本任务实现配方
注册表 + 展开 + CLI。**独立新包**，不碰 orchestrate/planner/agent 内部。

## 要实现的文件

### 1) `internal/recipe/recipe.go`（新建）

```go
package recipe

import (
	"fmt"
	"strings"
)

// Task is one node a recipe expands to. SpecTemplate may contain the literal
// token {{goal}} which Expand replaces with the caller's goal text.
type Task struct {
	ID           string
	SpecTemplate string
	Deps         []string
}

// Recipe is a named task-graph template.
type Recipe struct {
	Name        string
	Description string
	Tasks       []Task
}

// ExpandedTask is a recipe Task with {{goal}} substituted into its spec.
type ExpandedTask struct {
	ID   string
	Spec string
	Deps []string
}

// recipes is the built-in registry. Keep three to start:
var recipes = map[string]Recipe{
	"add-tests": {
		Name: "add-tests", Description: "给一个现有功能补测试",
		Tasks: []Task{
			{ID: "t-tests", SpecTemplate: "# 补测试\n\n为以下目标补充测试，TDD，跑通验收命令：\n\n{{goal}}\n\nverify: go test ./...", Deps: nil},
		},
	},
	"review-harden": {
		Name: "review-harden", Description: "实现 + 独立评审加固（两棒，第二棒依赖第一棒）",
		Tasks: []Task{
			{ID: "t-impl", SpecTemplate: "# 实现\n\n实现以下目标，TDD：\n\n{{goal}}\n\nverify: go build ./... && go test ./...", Deps: nil},
			{ID: "t-harden", SpecTemplate: "# 加固\n\n针对上一棒的实现做边界/错误处理加固，补充测试：\n\n{{goal}}\n\nverify: go test ./...", Deps: []string{"t-impl"}},
		},
	},
	"spec-to-plan": {
		Name: "spec-to-plan", Description: "把需求拆成设计 + 实现两棒",
		Tasks: []Task{
			{ID: "t-design", SpecTemplate: "# 设计\n\n为以下需求写简短设计（接口/数据流/测试策略）：\n\n{{goal}}", Deps: nil},
			{ID: "t-build", SpecTemplate: "# 实现\n\n按上一棒设计实现：\n\n{{goal}}\n\nverify: go build ./... && go test ./...", Deps: []string{"t-design"}},
		},
	},
}

// Get returns a recipe by name.
func Get(name string) (Recipe, bool) {
	r, ok := recipes[name]
	return r, ok
}

// Names returns the registered recipe names, sorted.
func Names() []string { /* return sorted keys */ }

// Expand substitutes goal into each task's SpecTemplate ({{goal}} → goal) and
// returns the expanded tasks in order. An empty goal is an error (a recipe needs
// a target).
func (r Recipe) Expand(goal string) ([]ExpandedTask, error) {
	if strings.TrimSpace(goal) == "" {
		return nil, fmt.Errorf("recipe: goal is required")
	}
	var out []ExpandedTask
	for _, t := range r.Tasks {
		out = append(out, ExpandedTask{
			ID:   t.ID,
			Spec: strings.ReplaceAll(t.SpecTemplate, "{{goal}}", goal),
			Deps: t.Deps,
		})
	}
	return out, nil
}
```

要求（TDD，先写失败测试）：
- `Names()` 返回排序后的 3 个配方名。
- `Get("add-tests")` ok=true；`Get("nope")` ok=false。
- `Expand("做个 X")` 把每个 task 的 `{{goal}}` 替换为目标，保留 Deps 顺序；展开数量与配方 Task 数一致。
- `Expand("")`（空目标）返回 error。
- `review-harden` 展开后第二个 task 的 Deps 含 `t-impl`。

### 2) `internal/recipe/recipe_test.go`（新建）
表驱动覆盖上面每条。`go test ./internal/recipe/...` 全绿。

### 3) `cmd/pactify/cmd_recipe.go`（新建）
加 `pactify recipe` 命令（cobra）：
- `recipe list`：打印每个配方的 name + description（用 Names() + Get()）。
- `recipe show <name> --goal "<目标>"`：展开并打印每个 task 的 id、deps、spec 全文；未知配方报错；`--goal` 缺失时报错。
- 注册进 root：`cmd/pactify/commands.go` 的 `root.AddCommand(...)` 加 `newRecipeCmd()`。

## 验收口径
```
go build ./...                       # 干净
go test ./internal/recipe/...        # 全绿
pactify recipe list                  # 列出 3 个配方
pactify recipe show review-harden --goal "加缓存层"   # 展开两棒，第二棒 deps=[t-impl]
```
边界：只新建上述文件 + root 注册一行。不改其它包。
verify: go build ./... && go test ./internal/recipe/...
