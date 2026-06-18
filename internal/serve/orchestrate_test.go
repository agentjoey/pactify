package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestOrchestrateParallel(t *testing.T) {
	dir := t.TempDir()
	s := New([]registry.Project{{Name: "p1", Path: dir}})
	handler := s.Handler()

	get := func() (*httptest.ResponseRecorder, ParallelStatusDTO) {
		req := httptest.NewRequest("GET", "/api/projects/p1/orchestrate/parallel", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		var dto ParallelStatusDTO
		_ = json.Unmarshal(rr.Body.Bytes(), &dto)
		return rr, dto
	}

	t.Run("no parallel dir → present=false", func(t *testing.T) {
		rr, dto := get()
		if rr.Code != http.StatusOK || dto.Present {
			t.Fatalf("want 200 present=false, got %d present=%v", rr.Code, dto.Present)
		}
	})

	t.Run("aggregates per-feature files sorted by id", func(t *testing.T) {
		pdir := filepath.Join(dir, ".pact", "orchestrate", "parallel")
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(pdir, "fb.json"), []byte(`{"feature":"fb","action":"run_owner"}`), 0o644)
		os.WriteFile(filepath.Join(pdir, "fa.json"), []byte(`{"feature":"fa","action":"merge"}`), 0o644)
		os.WriteFile(filepath.Join(pdir, "junk.txt"), []byte(`ignore me`), 0o644)
		rr, dto := get()
		if rr.Code != http.StatusOK || !dto.Present {
			t.Fatalf("want 200 present=true, got %d present=%v", rr.Code, dto.Present)
		}
		if len(dto.Features) != 2 {
			t.Fatalf("want 2 features, got %d", len(dto.Features))
		}
		// sorted by filename: fa before fb
		var first struct {
			Feature string `json:"feature"`
		}
		json.Unmarshal(dto.Features[0], &first)
		if first.Feature != "fa" {
			t.Errorf("first feature = %q, want fa (sorted)", first.Feature)
		}
	})
}

func TestOrchestrateRunningStaleGuard(t *testing.T) {
	mk := func(dir, body string) {
		p := filepath.Join(dir, ".pact", "orchestrate")
		_ = os.MkdirAll(p, 0o755)
		_ = os.WriteFile(filepath.Join(p, "status.json"), []byte(body), 0o644)
	}
	s := New(nil)
	recent := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)

	d1 := t.TempDir()
	mk(d1, `{"done":false,"escalated":false,"updated_at":"`+recent+`"}`)
	if !s.orchestrateRunning(d1) {
		t.Error("recent active run should be running")
	}

	d2 := t.TempDir()
	mk(d2, `{"done":false,"escalated":false,"updated_at":"`+old+`"}`)
	if s.orchestrateRunning(d2) {
		t.Error("30-min-old run must be treated as stale (not running)")
	}

	d3 := t.TempDir()
	mk(d3, `{"done":false,"escalated":false,"updated_at":"20260617-231101"}`)
	if s.orchestrateRunning(d3) {
		t.Error("unparseable old-format timestamp must be stale (not running)")
	}

	d4 := t.TempDir()
	mk(d4, `{"done":true,"escalated":false,"updated_at":"`+recent+`"}`)
	if s.orchestrateRunning(d4) {
		t.Error("done run is not running")
	}
}

func TestInferKindFromName(t *testing.T) {
	known := []string{"claude-code", "opencode", "gemini-cli", "kimi-cli"}
	cases := map[string]string{
		"opencode-worker": "opencode",
		"gemini-worker":   "gemini-cli",
		"kimi-worker":     "kimi-cli",
		"claude":          "claude-code",
		"weird-seat":      "",
	}
	for seat, want := range cases {
		if got := inferKindFromName(seat, known); got != want {
			t.Errorf("inferKindFromName(%q) = %q, want %q", seat, got, want)
		}
	}
}
