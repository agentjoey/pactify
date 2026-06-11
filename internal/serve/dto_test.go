package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectStateReadsAndMaps(t *testing.T) {
	root := t.TempDir()
	pact := filepath.Join(root, ".pact")
	os.MkdirAll(pact, 0o755)
	log := filepath.Join(pact, "log.jsonl")
	lines := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"p","protocol_version":1,"seats":[{"id":"claude-opus","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"opencode","roles":["worker"],"entry":"AGENTS.md"}],"base_branch":"main"}}
{"event_id":"2","ts":"2026-01-01T00:01:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"assign","task_id":"T1","feature":"F","payload":{"owner":"opencode","reviewer":"claude-opus","branch":"feat/x","spec":"s"}}
`
	os.WriteFile(log, []byte(lines), 0o644)

	dto, err := ProjectState(root)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Project != "p" || len(dto.Agents) != 2 || len(dto.Features) != 1 {
		t.Fatalf("bad dto: %+v", dto)
	}
	if dto.Features[0].Tasks[0].Owner != "opencode" || dto.Features[0].Tasks[0].Status != "assigned" {
		t.Fatalf("bad task: %+v", dto.Features[0].Tasks[0])
	}
	if dto.AwaitingCount != 0 {
		t.Fatalf("awaiting=%d", dto.AwaitingCount)
	}
}

// TestProjectStateForwardsDeps asserts a task assigned with a deps array
// surfaces those deps in the DTO and serializes them under "deps" in JSON,
// while a deps-free task omits the field (omitempty).
func TestProjectStateForwardsDeps(t *testing.T) {
	root := t.TempDir()
	pact := filepath.Join(root, ".pact")
	os.MkdirAll(pact, 0o755)
	log := filepath.Join(pact, "log.jsonl")
	lines := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"p","protocol_version":1,"seats":[{"id":"claude-opus","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"opencode","roles":["worker"],"entry":"AGENTS.md"}],"base_branch":"main"}}
{"event_id":"2","ts":"2026-01-01T00:01:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"assign","task_id":"T1","feature":"F","payload":{"owner":"opencode","reviewer":"claude-opus","branch":"feat/x","spec":"s"}}
{"event_id":"3","ts":"2026-01-01T00:02:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"assign","task_id":"T2","feature":"F","payload":{"owner":"opencode","reviewer":"claude-opus","branch":"feat/x","spec":"s","deps":["T1"]}}
`
	os.WriteFile(log, []byte(lines), 0o644)

	dto, err := ProjectState(root)
	if err != nil {
		t.Fatal(err)
	}
	tasks := dto.Features[0].Tasks
	if len(tasks) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Deps != nil {
		t.Fatalf("T1 (deps-free) must have nil Deps, got %+v", tasks[0].Deps)
	}
	if got := tasks[1].Deps; len(got) != 1 || got[0] != "T1" {
		t.Fatalf("T2 deps = %+v, want [T1]", got)
	}

	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if !strings.Contains(js, `"deps":["T1"]`) {
		t.Fatalf("DTO JSON missing deps for T2: %s", js)
	}
	// T1 has no deps line (omitempty). Exactly one "deps": occurrence expected.
	if n := strings.Count(js, `"deps":`); n != 1 {
		t.Fatalf("want exactly one deps field in JSON, got %d: %s", n, js)
	}
}

// TestProjectStateEmptyListsMarshalAsArrays pins the dogfood acceptance bug: a
// freshly-registered repo with agents but ZERO features must serialize
// "features":[] (and "agents":[] when empty), never "null". Go marshals a nil
// slice as null; the dashboard's State type promises non-null arrays and every
// consumer does features.map/forEach, so a null crashed the whole canvas.
func TestProjectStateEmptyListsMarshalAsArrays(t *testing.T) {
	root := t.TempDir()
	pact := filepath.Join(root, ".pact")
	os.MkdirAll(pact, 0o755)
	log := filepath.Join(pact, "log.jsonl")
	// init only: two seats, no features assigned yet (the dogfood state).
	lines := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"greet","protocol_version":1,"seats":[{"id":"claude-opus","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"opencode","roles":["worker"],"entry":"AGENTS.md"}],"base_branch":"main"}}
`
	os.WriteFile(log, []byte(lines), 0o644)

	dto, err := ProjectState(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Agents) != 2 {
		t.Fatalf("want 2 agents, got %d", len(dto.Agents))
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if !strings.Contains(js, `"features":[]`) {
		t.Fatalf(`features must marshal as [] not null: %s`, js)
	}
	if strings.Contains(js, `"features":null`) || strings.Contains(js, `"agents":null`) {
		t.Fatalf("no list field may marshal as null: %s", js)
	}
}

func TestProjectStateMissingLogIsEmpty(t *testing.T) {
	dto, err := ProjectState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if dto.Project != "unknown" || len(dto.Features) != 0 {
		t.Fatalf("want empty unknown, got %+v", dto)
	}
}
