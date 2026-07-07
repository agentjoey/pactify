// Package sarif assembles SARIF 2.1.0 logs from source-agnostic findings.
// It has no external dependencies so it can be reused by audit, review, or any
// other producer that needs GitHub Code Scanning compatible output.
package sarif

// Finding is one SARIF result's source-agnostic input.
type Finding struct {
	RuleID  string // e.g. "pact.audit.exec"
	Level   string // "note" | "warning" | "error" (SARIF result level)
	Message string
	// optional metadata surfaced under result.properties
	Seat, Task, Project, TS string
}

// Log is the top-level SARIF 2.1.0 log file.
type Log struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []Run  `json:"runs"`
}

// Run is one run of a SARIF analysis tool.
type Run struct {
	Tool    Tool     `json:"tool"`
	Results []Result `json:"results"`
}

// Tool describes the analysis tool that produced the run.
type Tool struct {
	Driver Driver `json:"driver"`
}

// Driver describes the tool driver (name, version, rules, etc.).
type Driver struct {
	Name           string `json:"name"`
	Version        string `json:"version,omitempty"`
	InformationURI string `json:"informationUri"`
	Rules          []Rule `json:"rules"`
}

// Rule is a SARIF reporting rule.
type Rule struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	ShortDescription ShortDescription `json:"shortDescription"`
}

// ShortDescription is a short plain-text rule description.
type ShortDescription struct {
	Text string `json:"text"`
}

// Result is one finding produced by the tool.
type Result struct {
	RuleID     string     `json:"ruleId"`
	Level      string     `json:"level"`
	Message    Message    `json:"message"`
	Properties Properties `json:"properties,omitempty"`
}

// Message is a plain-text result message.
type Message struct {
	Text string `json:"text"`
}

// Properties carries finding metadata surfaced under result.properties.
type Properties struct {
	Seat    string `json:"seat,omitempty"`
	Task    string `json:"task,omitempty"`
	Project string `json:"project,omitempty"`
	TS      string `json:"ts,omitempty"`
}

// Build assembles a SARIF 2.1.0 Log from findings. Rules are de-duplicated by
// RuleID and listed under tool.driver.rules. driverName e.g. "pactify".
func Build(driverName, driverVersion string, findings []Finding) Log {
	rules := make([]Rule, 0, len(findings))
	seen := make(map[string]struct{}, len(findings))
	results := make([]Result, 0, len(findings))

	for _, f := range findings {
		level := normalizeLevel(f.Level)
		if _, ok := seen[f.RuleID]; !ok && f.RuleID != "" {
			seen[f.RuleID] = struct{}{}
			rules = append(rules, Rule{
				ID:   f.RuleID,
				Name: f.RuleID,
				ShortDescription: ShortDescription{
					Text: f.RuleID,
				},
			})
		}
		results = append(results, Result{
			RuleID: f.RuleID,
			Level:  level,
			Message: Message{
				Text: f.Message,
			},
			Properties: Properties{
				Seat:    f.Seat,
				Task:    f.Task,
				Project: f.Project,
				TS:      f.TS,
			},
		})
	}

	return Log{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []Run{
			{
				Tool: Tool{
					Driver: Driver{
						Name:           driverName,
						Version:        driverVersion,
						InformationURI: "https://github.com/agentjoey/pactify",
						Rules:          rules,
					},
				},
				Results: results,
			},
		},
	}
}

func normalizeLevel(level string) string {
	switch level {
	case "note", "warning", "error":
		return level
	default:
		return "note"
	}
}
