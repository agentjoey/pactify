package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

func TestOrchestrateStatus(t *testing.T) {
	dir := t.TempDir()
	s := New([]registry.Project{{Name: "p1", Path: dir}})
	handler := s.Handler()

	t.Run("Unknown Project", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/projects/unknown/orchestrate/status", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("No File", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/projects/p1/orchestrate/status", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		expected := `{"present":false}` + "\n"
		if rr.Body.String() != expected {
			t.Errorf("expected %q, got %q", expected, rr.Body.String())
		}
	})

	pactDir := filepath.Join(dir, ".pact", "orchestrate")
	if err := os.MkdirAll(pactDir, 0755); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(pactDir, "status.json")

	t.Run("Valid File", func(t *testing.T) {
		content := `{"phase":"act","status":"looping"}`
		if err := os.WriteFile(statusPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/api/projects/p1/orchestrate/status", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		expected := `{"present":true,"status":{"phase":"act","status":"looping"}}` + "\n"
		if rr.Body.String() != expected {
			t.Errorf("expected %q, got %q", expected, rr.Body.String())
		}
	})

	t.Run("Invalid File", func(t *testing.T) {
		if err := os.WriteFile(statusPath, []byte(`{"phase":`), 0644); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/api/projects/p1/orchestrate/status", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rr.Code)
		}
	})
}
