package cockpit

import (
	"fmt"
	"sort"
	"strings"
)

// modelChoices bounds diagnostics without changing the catalog used to validate
// requests. Prefer the requested provider, then the closest model names; ties
// retain the catalog's order.
func modelChoices(harness, requested string, models []string) string {
	const limit = 10
	provider, _, hasProvider := strings.Cut(strings.ToLower(requested), "/")
	name := modelName(requested)
	type candidate struct {
		id       string
		provider bool
		distance int
	}
	choices := make([]candidate, 0, len(models))
	for _, model := range models {
		choices = append(choices, candidate{
			id:       model,
			provider: hasProvider && provider != "" && strings.HasPrefix(strings.ToLower(model), provider+"/"),
			distance: modelNameDistance(name, modelName(model)),
		})
	}
	sort.SliceStable(choices, func(i, j int) bool {
		if choices[i].provider != choices[j].provider {
			return choices[i].provider
		}
		return choices[i].distance < choices[j].distance
	})
	shown := make([]string, 0, min(len(choices), limit))
	for _, choice := range choices[:min(len(choices), limit)] {
		shown = append(shown, choice.id)
	}
	text := strings.Join(shown, ", ")
	if len(choices) > limit {
		command := harness + " models"
		if harness == "codex" {
			command = "codex debug models"
		}
		hint := fmt.Sprintf("run `%s` to list all", command)
		if harness == "kimi" {
			hint = "run `kimi` and enter `/model` to list all"
		}
		text += fmt.Sprintf("; … and %d more; %s", len(choices)-limit, hint)
	}
	return text
}

func modelName(id string) string {
	id = strings.ToLower(id)
	if _, name, ok := strings.Cut(id, "/"); ok {
		return name
	}
	return id
}

// Levenshtein distance ranks misspelled model names by edits required.
func modelNameDistance(a, b string) int {
	left, right := []rune(a), []rune(b)
	row := make([]int, len(right)+1)
	for j := range row {
		row[j] = j
	}
	for i, x := range left {
		previous := row[0]
		row[0] = i + 1
		for j, y := range right {
			cost := 0
			if x != y {
				cost = 1
			}
			previous, row[j+1] = row[j+1], min(row[j+1]+1, row[j]+1, previous+cost)
		}
	}
	return row[len(right)]
}
