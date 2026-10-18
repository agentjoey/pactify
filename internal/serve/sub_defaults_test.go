package serve

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterDefaultsPUT(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h := New().Handler()
	version := fmt.Sprintf("%x", sha256.Sum256(nil))
	for _, tc := range []struct {
		config string
		code   int
		field  string
	}{
		{`{"defaults":{"unknown":{}}}`, 422, "defaults.unknown"},
		{`{"defaults":{"codex":{"harness":"kimi"}}}`, 422, "defaults.codex"},
		{`{"defaults":{"codex":{"model":"configured","effort":"model-specific"}}}`, 200, ""},
	} {
		data, _ := json.Marshal(map[string]any{"version": version, "config": json.RawMessage(tc.config)})
		r := httptest.NewRequest("PUT", "/api/sub/router", strings.NewReader(string(data)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code || tc.field != "" && !strings.Contains(w.Body.String(), tc.field) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/sub/router", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"defaults":{"codex":{"model":"configured","effort":"model-specific"}}`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
