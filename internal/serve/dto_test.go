package serve

import (
	"os"
	"path/filepath"
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

func TestProjectStateMissingLogIsEmpty(t *testing.T) {
	dto, err := ProjectState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if dto.Project != "unknown" || len(dto.Features) != 0 {
		t.Fatalf("want empty unknown, got %+v", dto)
	}
}
