package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestsCRUD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	s := &Server{}

	body := []byte("kind=\"webx\"\nbinary=\"webx\"\n[runner]\nargs=[\"run\",\"{briefing}\"]\n")
	r := httptest.NewRequest("POST", "/api/agents/manifests", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleManifestCreate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("POST = %d (%s)", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(home, ".pactify", "agents", "webx.toml")); err != nil {
		t.Fatalf("not written: %v", err)
	}

	r2 := httptest.NewRequest("POST", "/api/agents/manifests", bytes.NewReader([]byte("binary=\"x\"\n")))
	w2 := httptest.NewRecorder()
	s.handleManifestCreate(w2, r2)
	if w2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid POST = %d, want 422", w2.Code)
	}

	r3 := httptest.NewRequest("GET", "/api/agents/manifests", nil)
	w3 := httptest.NewRecorder()
	s.handleManifestList(w3, r3)
	var got []map[string]any
	json.NewDecoder(w3.Body).Decode(&got)
	if len(got) != 1 || got[0]["kind"] != "webx" {
		t.Fatalf("list = %v", got)
	}

	r4 := httptest.NewRequest("DELETE", "/api/agents/manifests/webx", nil)
	r4.SetPathValue("kind", "webx")
	w4 := httptest.NewRecorder()
	s.handleManifestDelete(w4, r4)
	if w4.Code != http.StatusOK {
		t.Fatalf("DELETE = %d", w4.Code)
	}
}
