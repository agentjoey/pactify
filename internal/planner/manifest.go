package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// slugRe matches kebab-case ids: lowercase alphanumerics and dashes, starting
// with an alphanumeric. Used for both feature ids and task ids.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type PlanTask struct {
	ID       string   `json:"id"`
	Owner    string   `json:"owner"`
	Reviewer string   `json:"reviewer"`
	Spec     string   `json:"spec"`
	Verify   string   `json:"verify"`
	Deps     []string `json:"deps,omitempty"`
}

type Plan struct {
	Feature string     `json:"feature"`
	Branch  string     `json:"branch"`
	Tasks   []PlanTask `json:"tasks"`
}

func Parse(b []byte) (Plan, error) {
	var p Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return Plan{}, fmt.Errorf("parse manifest: %w", err)
	}
	if p.Feature == "" || p.Branch == "" || len(p.Tasks) == 0 {
		return Plan{}, errors.New("parse manifest: feature, branch, and tasks required")
	}
	return p, nil
}

func (p Plan) Validate(roster []string) error {
	var errs []string

	if p.Feature == "" {
		errs = append(errs, "feature must not be empty")
	}
	if p.Feature != "" && !slugRe.MatchString(p.Feature) {
		errs = append(errs, fmt.Sprintf("feature %q must be a kebab-case slug (^[a-z0-9][a-z0-9-]*$)", p.Feature))
	}
	if len(p.Feature) > 40 {
		errs = append(errs, fmt.Sprintf("feature %q exceeds 40 chars", p.Feature))
	}
	if p.Branch == "" {
		errs = append(errs, "branch must not be empty")
	}

	rosterSet := make(map[string]bool, len(roster))
	for _, r := range roster {
		rosterSet[r] = true
	}

	seen := map[string]bool{}
	idToTask := map[string]int{}
	for i, t := range p.Tasks {
		idToTask[t.ID] = i

		prefix := fmt.Sprintf("task %d: ", i)
		if t.ID == "" {
			errs = append(errs, prefix+"id must not be empty")
			prefix = fmt.Sprintf("task %d (no id): ", i)
		}
		if t.ID != "" && !slugRe.MatchString(t.ID) {
			errs = append(errs, prefix+fmt.Sprintf("id %q must be a kebab-case slug", t.ID))
		}
		if t.Owner == "" {
			errs = append(errs, prefix+"owner must not be empty")
		} else if !rosterSet[t.Owner] {
			errs = append(errs, fmt.Sprintf(prefix+"owner %q not in roster", t.Owner))
		}
		if t.Reviewer == "" {
			errs = append(errs, prefix+"reviewer must not be empty")
		} else if !rosterSet[t.Reviewer] {
			errs = append(errs, fmt.Sprintf(prefix+"reviewer %q not in roster", t.Reviewer))
		}
		if t.Owner != "" && t.Reviewer != "" && t.Owner == t.Reviewer {
			errs = append(errs, prefix+"owner and reviewer must differ")
		}
		if t.Spec == "" {
			errs = append(errs, prefix+"spec must not be empty")
		}
		if t.Verify == "" {
			errs = append(errs, prefix+"verify must not be empty")
		}

		if seen[t.ID] {
			errs = append(errs, fmt.Sprintf("task %d: duplicate id %q", i, t.ID))
		}
		if t.ID != "" {
			seen[t.ID] = true
		}
	}

	for _, t := range p.Tasks {
		if t.ID == "" {
			continue
		}
		for _, d := range t.Deps {
			if d == t.ID {
				errs = append(errs, fmt.Sprintf("task %s: self-referencing dep %q", t.ID, d))
				continue
			}
			if _, ok := idToTask[d]; !ok {
				errs = append(errs, fmt.Sprintf("task %s: dep %q not found in plan", t.ID, d))
			}
		}
	}

	if err := checkCycle(p, idToTask); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

func checkCycle(p Plan, idToTask map[string]int) error {
	graph := make(map[string][]string, len(p.Tasks))
	for _, t := range p.Tasks {
		if t.ID == "" {
			continue
		}
		graph[t.ID] = t.Deps
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var dfs func(n string) bool
	dfs = func(n string) bool {
		color[n] = gray
		for _, m := range graph[n] {
			if _, inPlan := idToTask[m]; !inPlan {
				continue
			}
			switch color[m] {
			case gray:
				return true
			case white:
				if dfs(m) {
					return true
				}
			}
		}
		color[n] = black
		return false
	}
	for _, t := range p.Tasks {
		if t.ID == "" {
			continue
		}
		if color[t.ID] == white {
			if dfs(t.ID) {
				return errors.New("dependency cycle detected")
			}
		}
	}
	return nil
}
