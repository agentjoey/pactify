package serve

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRouterAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "")
	h := serveHandler(t, New(), "127.0.0.1:7777")
	call := func(method, path, body, contentType, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:7777"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	get := func(exists bool, data string) string {
		t.Helper()
		w := call("GET", "/api/sub/router", "", "", "")
		var got struct {
			Path   string
			Exists bool
			Config struct {
				Aliases     map[string]any
				Fallback    map[string]any
				QuotaGroups map[string]any
			}
			Version         string
			ExhaustedRoutes []any
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("GET %d: %s", w.Code, w.Body.String())
		}
		if w.Code != 200 || got.Exists != exists || got.Path != filepath.Join(home, "router.json") || got.Version != fmt.Sprintf("%x", sha256.Sum256([]byte(data))) || got.Config.Aliases == nil || got.Config.Fallback == nil || got.Config.QuotaGroups == nil || got.ExhaustedRoutes == nil {
			t.Fatalf("GET: %d %s", w.Code, w.Body.String())
		}
		return got.Version
	}
	version := get(false, "")
	body := func(v, c string) string {
		b, _ := json.Marshal(map[string]any{"version": v, "config": json.RawMessage(c)})
		return string(b)
	}
	config := `{"aliases":{"fast":{"harness":"codex","model":"test","effort":"low"}},"fallback":{"codex":["@fast","codex"]},"quotaGroups":{"group":{"harness":"agy","models":["one","two"]}}}`
	for _, tc := range []struct {
		body, ct string
		code     int
		field    string
	}{
		{body(version, `{"aliases":{"fast":{"harness":"bad"}}}`), "application/json", 422, "aliases.fast.harness"},
		{body(version, `{"fallback":{"codex":["@missing"]}}`), "application/json", 422, "fallback.codex[0]"},
		{body(version, `{"aliases":{"mm":{"harness":"opencode"}},"fallback":{"codex":["codex","@mm"]}}`), "application/json", 422, "fallback.codex[1]"},
		{body(version, `{"quotaGroups":{"group":{"harness":"agy","models":[]}}}`), "application/json", 422, "quotaGroups.group.models"},
		{body(version, `null`), "application/json", 422, "config"},
		{body(version, config), "text/plain", 415, ""},
		{strings.Repeat(" ", 65537), "application/json", 413, ""},
		{body(version, config) + " {}", "application/json", 400, ""},
	} {
		w := call("PUT", "/api/sub/router", tc.body, tc.ct, "")
		if w.Code != tc.code || tc.field != "" && !strings.Contains(w.Body.String(), `"field":"`+tc.field+`"`) {
			t.Errorf("PUT want %d %s: %d %s", tc.code, tc.field, w.Code, w.Body.String())
		}
	}
	for _, method := range []string{"PUT", "DELETE"} {
		path := "/api/sub/router"
		if method == "DELETE" {
			path = "/api/sub/quota/codex%2Ftest"
		}
		w := call(method, path, body(version, config), "application/json", "https://attacker.example")
		if w.Code != http.StatusForbidden {
			t.Fatalf("cross-origin %s: %d", method, w.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "router.json")); !os.IsNotExist(err) {
		t.Fatal("rejected writes changed disk")
	}
	w := call("PUT", "/api/sub/router", body(version, config), "application/json; charset=utf-8", "http://localhost:7777")
	if w.Code != 200 {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(home, "router.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(home, "router.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	get(true, string(data))
	w = call("PUT", "/api/sub/router", body(version, config), "application/json", "")
	if w.Code != 409 {
		t.Fatalf("conflict: %d %s", w.Code, w.Body.String())
	}
	now := time.Now()
	reset := now.Add(time.Hour)
	quota := map[string]any{"codex/test": map[string]any{"harness": "codex", "model": "test", "resetAt": reset, "recordedAt": now}, "group": map[string]any{"harness": "agy", "model": "one", "resetAt": reset, "recordedAt": now}}
	qdata, _ := json.Marshal(quota)
	if err := os.WriteFile(filepath.Join(home, "quota.json"), qdata, 0600); err != nil {
		t.Fatal(err)
	}
	w = call("GET", "/api/sub/router", "", "", "")
	if !strings.Contains(w.Body.String(), `"key":"codex/test"`) || !strings.Contains(w.Body.String(), `"resetAt":`) {
		t.Fatal(w.Body.String())
	}
	for _, key := range []string{"codex%2Ftest", "group"} {
		w = call("DELETE", "/api/sub/quota/"+key, "", "", "")
		if w.Code != 200 {
			t.Fatalf("DELETE: %d %s", w.Code, w.Body.String())
		}
		if key == "codex%2Ftest" {
			remaining, err := os.ReadFile(filepath.Join(home, "quota.json"))
			if err != nil || !strings.Contains(string(remaining), `"group"`) || strings.Contains(string(remaining), `"codex/test"`) {
				t.Fatalf("DELETE removed unrelated quota: %s %v", remaining, err)
			}
		}
	}
	qdata, _ = os.ReadFile(filepath.Join(home, "quota.json"))
	if strings.TrimSpace(string(qdata)) != "{}" {
		t.Fatal(string(qdata))
	}
	get(true, string(data))
}
