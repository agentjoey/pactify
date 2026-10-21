package serve

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestStoragePruneWriteBoundary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	token := strings.Repeat("t", 32)
	t.Setenv("PACTIFY_SERVE_TOKEN", token)
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "")
	handler := serveHandler(t, New(), "127.0.0.1:7777")
	call := func(method, path, auth, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7777/api/sub"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		auth, origin string
		want         int
	}{{"", "", 401}, {token, "https://evil.test", 403}, {token, "http://127.0.0.1:7778", 403}} {
		if w := call("POST", "/storage/prune", tc.auth, tc.origin, `{}`); w.Code != tc.want {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body)
		}
	}
	plan := call("GET", "/storage/plan", token, "", "")
	if plan.Code != 200 {
		t.Fatalf("%d %s", plan.Code, plan.Body)
	}
	var p struct {
		Version string `json:"version"`
	}
	json.Unmarshal(plan.Body.Bytes(), &p)
	if p.Version == "" {
		t.Fatal("missing confirmation version")
	}
	if w := call("POST", "/storage/prune", token, "http://localhost:7777", `{"version":"old"}`); w.Code != 409 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := call("POST", "/storage/prune", token, "http://localhost:7777", `{"version":"`+p.Version+`"}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	entries, _ := os.ReadDir(home)
	if len(entries) == 0 {
		t.Fatal("write did not record run")
	}
}
