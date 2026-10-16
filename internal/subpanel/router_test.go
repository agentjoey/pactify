package subpanel

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestPanelRouterReadOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, exists := range []bool{false, true} {
		if exists {
			if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(`{"aliases":{"fast":{"harness":"codex"}}}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		w := request(s, "GET", "/api/router", s.token)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"exists":`) {
			t.Fatalf("GET: %d %s", w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/router", "/quota/codex%2Fmodel"} {
		for _, method := range []string{"PUT", "DELETE", "POST", "PATCH", "HEAD"} {
			w := httptest.NewRecorder()
			ReadOnlyHandler(store).ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if full := request(s, method, "/api"+path, s.token); full.Code != 405 {
				t.Fatalf("authenticated panel %s %s: %d", method, path, full.Code)
			}
			if w.Code != 405 || w.Header().Get("Allow") != "GET" {
				t.Fatalf("%s %s: %d", method, path, w.Code)
			}
		}
	}
}
