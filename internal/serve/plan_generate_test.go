package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

func TestPlanGenStatusIdleByDefault(t *testing.T) {
	dir := t.TempDir()
	s := New([]registry.Project{{Name: "p1", Path: dir}})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/projects/p1/plan/generate/status")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var dto PlanGenStatusDTO
	_ = json.NewDecoder(resp.Body).Decode(&dto)
	if dto.State != "idle" {
		t.Fatalf("state = %q, want idle", dto.State)
	}
}

func TestPlanGenStatusReflectsFile(t *testing.T) {
	dir := t.TempDir()
	if err := writePlanGenStatus(dir, PlanGenStatusDTO{State: "running", Feature: "add-2fa"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := New([]registry.Project{{Name: "p1", Path: dir}})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/projects/p1/plan/generate/status")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var dto PlanGenStatusDTO
	_ = json.NewDecoder(resp.Body).Decode(&dto)
	if dto.State != "running" || dto.Feature != "add-2fa" {
		t.Fatalf("dto = %+v, want running/add-2fa", dto)
	}
}

func pollPlanGen(t *testing.T, base string) PlanGenStatusDTO {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/api/projects/p1/plan/generate/status")
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		var dto PlanGenStatusDTO
		_ = json.NewDecoder(resp.Body).Decode(&dto)
		resp.Body.Close()
		if dto.State != "running" {
			return dto
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("plan-gen never left running")
	return PlanGenStatusDTO{}
}

// Adaptation note: newAuthorRepo inits with seats "claude-opus" (orchestrator,reviewer)
// and "opencode" (worker). The spec called for seat "claude" but that is not in the
// roster produced by newAuthorRepo; we use "claude-opus" instead so actingProject passes.
func TestPlanGenerateSuccess(t *testing.T) {
	dir := newAuthorRepo(t)
	s := New([]registry.Project{{Name: "p1", Path: dir}})
	s.SetSeat("claude-opus")
	s.SetPlannerRunner(func(d string, args, env []string) error {
		manifest := `{"feature":"add-2fa","branch":"feat-2fa","tasks":[` +
			`{"id":"add-2fa-otp","owner":"opencode","reviewer":"claude-opus","spec":".pact/tasks/x.md","verify":"go test ./..."}]}`
		return os.WriteFile(filepath.Join(d, ".pact", "plan-add-2fa.json"), []byte(manifest), 0o644)
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/projects/p1/plan/generate", "application/json",
		strings.NewReader(`{"goal":"add 2fa login","feature":"add-2fa"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != 202 {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()
	got := pollPlanGen(t, srv.URL)
	if got.State != "done" || got.Feature != "add-2fa" {
		t.Fatalf("final = %+v, want done/add-2fa", got)
	}
}

func TestPlanGenerateRunnerErrorBecomesError(t *testing.T) {
	dir := newAuthorRepo(t)
	s := New([]registry.Project{{Name: "p1", Path: dir}})
	s.SetSeat("claude-opus")
	s.SetPlannerRunner(func(d string, args, env []string) error { return os.ErrPermission })
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/api/projects/p1/plan/generate", "application/json",
		strings.NewReader(`{"goal":"x","feature":"add-2fa"}`))
	resp.Body.Close()
	if got := pollPlanGen(t, srv.URL); got.State != "error" {
		t.Fatalf("final = %+v, want error", got)
	}
}

func TestPlanGenerateRejectsBadFeature(t *testing.T) {
	dir := newAuthorRepo(t)
	s := New([]registry.Project{{Name: "p1", Path: dir}})
	s.SetSeat("claude-opus")
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/projects/p1/plan/generate", "application/json",
		strings.NewReader(`{"goal":"x","feature":"Bad_Feature"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPlanGenerateNoSeatRejected(t *testing.T) {
	dir := newAuthorRepo(t)
	s := New([]registry.Project{{Name: "p1", Path: dir}})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/projects/p1/plan/generate", "application/json",
		strings.NewReader(`{"goal":"x","feature":"add-2fa"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}
