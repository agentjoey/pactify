package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

func TestPlanReview(t *testing.T) {
	dir := t.TempDir()
	s := New([]registry.Project{{Name: "p1", Path: dir}})
	handler := s.Handler()

	get := func(feature string) (*httptest.ResponseRecorder, PlanReviewDTO) {
		req := httptest.NewRequest("GET", "/api/projects/p1/plan/"+feature, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		var dto PlanReviewDTO
		_ = json.Unmarshal(rr.Body.Bytes(), &dto)
		return rr, dto
	}

	t.Run("unknown project 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/projects/nope/plan/x", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("want 404, got %d", rr.Code)
		}
	})

	t.Run("no manifest → present=false", func(t *testing.T) {
		rr, dto := get("relay")
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rr.Code)
		}
		if dto.Present {
			t.Errorf("present should be false with no manifest")
		}
	})

	t.Run("valid manifest → tasks surfaced", func(t *testing.T) {
		pactDir := filepath.Join(dir, ".pact")
		if err := os.MkdirAll(pactDir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := `{
		  "feature": "relay",
		  "branch": "feat-relay",
		  "tasks": [
		    {"id": "t1", "owner": "w", "reviewer": "r", "spec": ".pact/tasks/t1.md", "verify": "go test ./..."},
		    {"id": "t2", "owner": "w", "reviewer": "r", "spec": ".pact/tasks/t2.md", "verify": "go test ./...", "deps": ["t1"]}
		  ]
		}`
		if err := os.WriteFile(filepath.Join(pactDir, "plan-relay.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		rr, dto := get("relay")
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rr.Code)
		}
		if !dto.Present {
			t.Fatal("present should be true")
		}
		if dto.Feature != "relay" || dto.Branch != "feat-relay" {
			t.Errorf("feature/branch = %q/%q", dto.Feature, dto.Branch)
		}
		if len(dto.Tasks) != 2 {
			t.Fatalf("want 2 tasks, got %d", len(dto.Tasks))
		}
		if dto.Tasks[1].ID != "t2" || len(dto.Tasks[1].Deps) != 1 || dto.Tasks[1].Deps[0] != "t1" {
			t.Errorf("task t2 deps wrong: %+v", dto.Tasks[1])
		}
	})

	t.Run("malformed manifest → present + invalid", func(t *testing.T) {
		pactDir := filepath.Join(dir, ".pact")
		_ = os.MkdirAll(pactDir, 0o755)
		_ = os.WriteFile(filepath.Join(pactDir, "plan-broken.json"), []byte("{not json"), 0o644)
		_, dto := get("broken")
		if !dto.Present || dto.Valid {
			t.Errorf("malformed should be present+invalid, got present=%v valid=%v", dto.Present, dto.Valid)
		}
		if dto.Error == "" {
			t.Error("malformed should carry a parse error")
		}
	})
}
