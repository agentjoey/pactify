package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

// seedTierPlan builds a project whose plan manifest exercises every tier state
// the review UI must distinguish (exec-tiering-ui). Returns the repo root and
// the sentinel content of an out-of-repo file that must never be read.
func seedTierPlan(t *testing.T) (root, sentinel string) {
	t.Helper()
	root = t.TempDir()
	pactDir := filepath.Join(root, ".pact")
	if err := os.MkdirAll(filepath.Join(pactDir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}

	initLine := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"testp","protocol_version":1,"seats":[{"id":"claude","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"alice","roles":["worker"],"entry":"A.md"},{"id":"bob","roles":["worker"],"entry":"B.md"}],"base_branch":"main"}}` + "\n"
	if err := os.WriteFile(filepath.Join(pactDir, "log.jsonl"), []byte(initLine), 0o644); err != nil {
		t.Fatal(err)
	}

	specs := map[string]string{
		"explicit-l1.md": "# t1\ntier: L1\n",        // explicit default tier
		"no-tier.md":     "# t2\nverify: go test\n", // no tier line at all
		"lower.md":       "# t3\ntier: l2\n",        // lowercase, normalizes to L2
		"spec-l0.md":     "# t4\ntier: L0\n",        // conflicts with manifest L3
		"unknown.md":     "# t6\ntier: L9\n",        // unrecognized value, collapses to L1
	}
	for name, content := range specs {
		if err := os.WriteFile(filepath.Join(pactDir, "tasks", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A real, readable file OUTSIDE the repo: the traversal manifest entry must
	// never cause its content to be read or surfaced.
	sentinel = "tier: L3-OUTSIDE-SENTINEL"
	outside := filepath.Join(filepath.Dir(root), "outside-spec.md")
	if err := os.WriteFile(outside, []byte("# escape\n"+sentinel+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := `{"feature":"demo-feature","branch":"feat-demo","tasks":[` +
		`{"id":"t1","owner":"alice","reviewer":"bob","spec":".pact/tasks/explicit-l1.md","verify":"go test","dimension":"correctness","role":"backend"},` +
		`{"id":"t2","owner":"bob","reviewer":"alice","spec":".pact/tasks/no-tier.md","verify":"go test"},` +
		`{"id":"t3","owner":"alice","reviewer":"bob","spec":".pact/tasks/lower.md","verify":"go test"},` +
		`{"id":"t4","owner":"bob","reviewer":"alice","spec":".pact/tasks/spec-l0.md","verify":"go test","tier":"L3"},` +
		`{"id":"t5","owner":"alice","reviewer":"bob","spec":"../outside-spec.md","verify":"go test"},` +
		`{"id":"t6","owner":"alice","reviewer":"bob","spec":".pact/tasks/unknown.md","verify":"go test"}` +
		`]}`
	if err := os.WriteFile(filepath.Join(pactDir, "plan-demo-feature.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, sentinel
}

func getPlanReview(t *testing.T, baseURL, id, feature string) PlanReviewDTO {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/projects/" + id + "/plan/" + feature)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("plan review status=%d, want 200", resp.StatusCode)
	}
	var out PlanReviewDTO
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func taskByID(t *testing.T, tasks []planTaskDTO, id string) planTaskDTO {
	t.Helper()
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %q not in review DTO %+v", id, tasks)
	return planTaskDTO{}
}

func TestPlanReviewTier(t *testing.T) {
	root, sentinel := seedTierPlan(t)
	srv := New([]registry.Project{{Name: "p", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	dto := getPlanReview(t, ts.URL, "p", "demo-feature")
	if !dto.Present || !dto.Valid {
		t.Fatalf("plan must be present+valid, got %+v", dto)
	}

	// The product distinction: explicit `tier: L1` is NOT the same as a spec
	// with no tier line (planner missed the mandatory tier).
	t1 := taskByID(t, dto.Tasks, "t1")
	if t1.Tier != "L1" || t1.TierMissing {
		t.Fatalf("t1 explicit L1: got tier=%q missing=%v", t1.Tier, t1.TierMissing)
	}
	if t1.TierRaw != "" {
		t.Fatalf("t1 explicit L1 must NOT carry tier_raw, got %q", t1.TierRaw)
	}
	if t1.Dimension != "correctness" || t1.Role != "backend" {
		t.Fatalf("t1 dimension/role pass-through: got %q/%q", t1.Dimension, t1.Role)
	}

	t2 := taskByID(t, dto.Tasks, "t2")
	if !t2.TierMissing || t2.Tier != "L1" {
		t.Fatalf("t2 no tier line: got tier=%q missing=%v, want missing=true tier=L1", t2.Tier, t2.TierMissing)
	}

	// ParseTier normalization: lowercase spec value renders uppercase.
	t3 := taskByID(t, dto.Tasks, "t3")
	if t3.Tier != "L2" || t3.TierMissing {
		t.Fatalf("t3 lowercase l2: got tier=%q missing=%v", t3.Tier, t3.TierMissing)
	}

	// Manifest↔spec conflict: the spec value wins (the engine reads the spec)
	// and the conflict is called out for the reviewer.
	t4 := taskByID(t, dto.Tasks, "t4")
	if t4.Tier != "L0" || t4.TierMissing {
		t.Fatalf("t4 conflict: got tier=%q missing=%v, want spec side L0", t4.Tier, t4.TierMissing)
	}
	if !strings.Contains(t4.TierConflict, "manifest says L3") || !strings.Contains(t4.TierConflict, "engine will use L0") {
		t.Fatalf("t4 conflict note: got %q", t4.TierConflict)
	}

	// Path hardening: the traversal spec is refused (TierMissing) and the
	// out-of-repo file's content must not appear anywhere in the response.
	t5 := taskByID(t, dto.Tasks, "t5")
	if !t5.TierMissing || t5.Tier != "L1" {
		t.Fatalf("t5 traversal spec: got tier=%q missing=%v, want missing=true tier=L1", t5.Tier, t5.TierMissing)
	}

	// Unrecognized tier value (a typo like `tier: L9`): the engine collapses it
	// to L1, but the raw value must survive in TierRaw so the UI can name it —
	// otherwise the row is byte-identical to an explicit `tier: L1`.
	t6 := taskByID(t, dto.Tasks, "t6")
	if t6.Tier != "L1" || t6.TierMissing || t6.TierRaw != "L9" {
		t.Fatalf("t6 unrecognized tier: got tier=%q missing=%v raw=%q, want L1/false/L9", t6.Tier, t6.TierMissing, t6.TierRaw)
	}

	raw := getPlanReviewRaw(t, ts.URL, "p", "demo-feature")
	if strings.Contains(raw, sentinel) {
		t.Fatalf("out-of-repo spec content leaked into the response")
	}
}

func getPlanReviewRaw(t *testing.T, baseURL, id, feature string) string {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/projects/" + id + "/plan/" + feature)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
