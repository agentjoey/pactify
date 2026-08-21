package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

// seedRuntimeTierProject builds a project with one assigned task (spec under
// .pact/tasks/) and returns its root; the caller writes the spec content and
// the orchestrate status.json snapshot.
func seedRuntimeTierProject(t *testing.T, seatKind string) string {
	t.Helper()
	root := t.TempDir()
	pactDir := filepath.Join(root, ".pact")
	if err := os.MkdirAll(filepath.Join(pactDir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pactDir, "orchestrate"), 0o755); err != nil {
		t.Fatal(err)
	}

	kindField := ""
	if seatKind != "" {
		kindField = `,"kind":"` + seatKind + `"`
	}
	log := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"p","protocol_version":1,"seats":[{"id":"claude","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"alice","roles":["worker"],"entry":"A.md"` + kindField + `}],"base_branch":"main"}}` + "\n" +
		`{"event_id":"2","ts":"2026-01-01T00:01:00Z","agent_id":"claude","role":"orchestrator","event_type":"assign","task_id":"t1","feature":"f1","payload":{"owner":"alice","reviewer":"claude","branch":"feat/f1","spec":".pact/tasks/t1.md"}}` + "\n"
	if err := os.WriteFile(filepath.Join(pactDir, "log.jsonl"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeOrchestrateStatus(t *testing.T, root, body string) {
	t.Helper()
	p := filepath.Join(root, ".pact", "orchestrate", "status.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getOrchestrateStatus(t *testing.T, baseURL, id string) (raw string, dto OrchestrateStatusDTO) {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/projects/" + id + "/orchestrate/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("orchestrate status=%d, want 200", resp.StatusCode)
	}
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	raw = sb.String()
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		t.Fatalf("status DTO decode: %v (%q)", err, raw)
	}
	return raw, dto
}

// The status endpoint surfaces the current task's tier (from the SPEC, the
// engine's own source) plus the serve-resolved effort, without any change to
// orchestrate.Status.
func TestOrchestrateStatusTierEffort(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir()) // isolate machine-level roles/agentreg
	root := seedRuntimeTierProject(t, "claude-code")
	if err := os.WriteFile(filepath.Join(root, ".pact", "tasks", "t1.md"), []byte("# t1\ntier: L0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeOrchestrateStatus(t, root, `{"feature":"f1","task":"t1","seat":"alice","action":"run_owner","phase":"owner working","done":false}`)

	srv := New([]registry.Project{{Name: "p", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	_, dto := getOrchestrateStatus(t, ts.URL, "p")
	if dto.Tier != "L0" {
		t.Fatalf("tier = %q, want L0 (from spec)", dto.Tier)
	}
	if dto.Effort != "low" {
		t.Fatalf("effort = %q, want low (EffortForTier L0, claude-code has EffortArgs)", dto.Effort)
	}
}

// A per-seat effort pin (roles binding) must win over the tier-derived budget:
// tier L0 derives "low", but the operator's explicit "high" is what shows.
func TestOrchestrateStatusEffortSeatOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	rolesJSON := `{"profiles":{"pinned":{"kind":"claude-code","effort":"high"}},"bindings":{"alice":"pinned"}}`
	if err := os.WriteFile(filepath.Join(home, "roles.json"), []byte(rolesJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	root := seedRuntimeTierProject(t, "claude-code")
	if err := os.WriteFile(filepath.Join(root, ".pact", "tasks", "t1.md"), []byte("# t1\ntier: L0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeOrchestrateStatus(t, root, `{"feature":"f1","task":"t1","seat":"alice","action":"run_owner","phase":"owner working","done":false}`)

	srv := New([]registry.Project{{Name: "p", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	_, dto := getOrchestrateStatus(t, ts.URL, "p")
	if dto.Tier != "L0" {
		t.Fatalf("tier = %q, want L0", dto.Tier)
	}
	if dto.Effort != "high" {
		t.Fatalf("effort = %q, want the per-seat override high (not tier-derived low)", dto.Effort)
	}
}

// Effort == "" is the common case: a kind with no EffortArgs (kimi-cli)
// resolves without error and simply reports no injected budget.
func TestOrchestrateStatusEffortKindWithoutEffortArgs(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	root := seedRuntimeTierProject(t, "kimi-cli")
	if err := os.WriteFile(filepath.Join(root, ".pact", "tasks", "t1.md"), []byte("# t1\ntier: L3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeOrchestrateStatus(t, root, `{"feature":"f1","task":"t1","seat":"alice","action":"run_owner","phase":"owner working","done":false}`)

	srv := New([]registry.Project{{Name: "p", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	raw, dto := getOrchestrateStatus(t, ts.URL, "p")
	if dto.Tier != "L3" {
		t.Fatalf("tier = %q, want L3", dto.Tier)
	}
	if dto.Effort != "" {
		t.Fatalf("effort = %q, want \"\" (kimi-cli declares no EffortArgs)", dto.Effort)
	}
	if !strings.Contains(raw, `"tier":"L3"`) {
		t.Fatalf("response must carry tier: %s", raw)
	}
}

// No tier line in the spec (or a status naming no task, e.g. an old status
// file) → tier/effort stay out of the DTO; nothing breaks.
func TestOrchestrateStatusTierOmitted(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	root := seedRuntimeTierProject(t, "claude-code")
	if err := os.WriteFile(filepath.Join(root, ".pact", "tasks", "t1.md"), []byte("# t1\nverify: go test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeOrchestrateStatus(t, root, `{"feature":"f1","task":"t1","seat":"alice","action":"run_owner","phase":"owner working","done":false}`)

	srv := New([]registry.Project{{Name: "p", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	raw, _ := getOrchestrateStatus(t, ts.URL, "p")
	if strings.Contains(raw, `"tier"`) || strings.Contains(raw, `"effort"`) {
		t.Fatalf("spec without tier must omit tier/effort, got %s", raw)
	}

	writeOrchestrateStatus(t, root, `{"feature":"f1","task":"","seat":"","action":"stuck","phase":"stuck","escalated":true}`)
	raw, _ = getOrchestrateStatus(t, ts.URL, "p")
	if strings.Contains(raw, `"tier"`) || strings.Contains(raw, `"effort"`) {
		t.Fatalf("task-less status must omit tier/effort, got %s", raw)
	}
}
