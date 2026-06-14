// Package tokens persists per-task token tallies captured from agent runs and
// parses an agent's headless JSON output to extract token usage. The store lives
// at .pact/orchestrate/tokens.json (best-effort telemetry; absence = no data).
package tokens

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Tally is one task's accumulated token usage across its runs.
type Tally struct {
	Tokens int `json:"tokens"`
	Runs   int `json:"runs"`
}

// Store maps task id → tally.
type Store struct {
	Tasks map[string]Tally `json:"tasks"`
}

func path(repoDir string) string {
	return filepath.Join(repoDir, ".pact", "orchestrate", "tokens.json")
}

// Load reads the token store for a repo; a missing/empty file yields an empty
// store (not an error).
func Load(repoDir string) Store {
	s := Store{Tasks: map[string]Tally{}}
	b, err := os.ReadFile(path(repoDir))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	if s.Tasks == nil {
		s.Tasks = map[string]Tally{}
	}
	return s
}

// Add accumulates tokens for a task (one run).
func (s *Store) Add(taskID string, tok int) {
	if s.Tasks == nil {
		s.Tasks = map[string]Tally{}
	}
	t := s.Tasks[taskID]
	t.Tokens += tok
	t.Runs++
	s.Tasks[taskID] = t
}

// Save writes the store, creating .pact/orchestrate/ if needed.
func Save(repoDir string, s Store) error {
	p := path(repoDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// Get returns a task's accumulated tokens (0 if unknown).
func (s Store) Get(taskID string) int { return s.Tasks[taskID].Tokens }

// usageShape matches the token fields agents emit in their headless JSON. Both
// snake_case (claude / Anthropic) and the bare totals are accepted.
type usageShape struct {
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	// Some CLIs put totals at the top level.
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	Tokens       int `json:"tokens"`
}

// Parse extracts total tokens (input + output) from an agent's headless JSON
// output. claude (`--output-format json`) emits one JSON object with a `usage`
// block (VERIFIED). stream-json kinds emit JSONL — we scan lines and take the
// LAST one that carries usage. Returns (n, true) on success. Per-kind formats
// beyond claude are best-effort until each is run-verified.
func Parse(kind, output string) (int, bool) {
	best, ok := 0, false
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var u usageShape
		if json.Unmarshal([]byte(line), &u) != nil {
			continue
		}
		n := u.Usage.InputTokens + u.Usage.OutputTokens
		if n == 0 {
			n = u.InputTokens + u.OutputTokens
		}
		if n == 0 {
			n = u.TotalTokens
		}
		if n == 0 {
			n = u.Tokens
		}
		if n > 0 {
			best, ok = n, true // last usage-bearing line wins
		}
	}
	// claude emits a single multi-line pretty JSON object (not JSONL); the
	// line-scan above misses it, so fall back to parsing the whole blob.
	if !ok {
		var u usageShape
		if json.Unmarshal([]byte(output), &u) == nil {
			if n := u.Usage.InputTokens + u.Usage.OutputTokens; n > 0 {
				return n, true
			}
		}
	}
	return best, ok
}
