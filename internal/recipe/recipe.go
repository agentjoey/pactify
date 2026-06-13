package recipe

import (
	"fmt"
	"sort"
	"strings"
)

type Task struct {
	ID           string
	SpecTemplate string
	Deps         []string
}

type Recipe struct {
	Name        string
	Description string
	Tasks       []Task
}

type ExpandedTask struct {
	ID   string
	Spec string
	Deps []string
}

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

func Get(name string) (Recipe, bool) {
	r, ok := recipes[name]
	return r, ok
}

func Names() []string {
	keys := make([]string, 0, len(recipes))
	for k := range recipes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (r Recipe) Expand(goal string) ([]ExpandedTask, error) {
	if strings.TrimSpace(goal) == "" {
		return nil, fmt.Errorf("recipe: goal is required")
	}
	out := make([]ExpandedTask, 0, len(r.Tasks))
	for _, t := range r.Tasks {
		deps := make([]string, len(t.Deps))
		copy(deps, t.Deps)
		out = append(out, ExpandedTask{
			ID:   t.ID,
			Spec: strings.ReplaceAll(t.SpecTemplate, "{{goal}}", goal),
			Deps: deps,
		})
	}
	return out, nil
}
