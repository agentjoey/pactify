package subpanel

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestStorageReadOnlyAndCached(t *testing.T) {
	home := t.TempDir()
	store := sub.Store{Root: filepath.Join(home, "jobs")}
	handler := ReadOnlyHandler(store)
	for _, path := range []string{"/storage", "/storage/plan"} {
		for _, method := range []string{"GET", "POST", "HEAD", "PUT", "DELETE"} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			want := 405
			if method == "GET" {
				want = 200
				if !strings.Contains(w.Body.String(), "cacheMaxBytes") {
					t.Fatal(w.Body.String())
				}
			}
			if w.Code != want {
				t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
			}
		}
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Fatalf("GET wrote files: %v", entries)
	}
	os.MkdirAll(filepath.Join(home, "cache"), 0700)
	os.WriteFile(filepath.Join(home, "cache", "later"), []byte("new"), 0600)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/storage", nil))
	if strings.Contains(w.Body.String(), "cache/later") {
		t.Fatal("polling ignored status cache")
	}
}
