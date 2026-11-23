package serve

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/agentjoey/pactify/internal/testenv"
)

func TestOpencodeServePeer(t *testing.T) { testenv.OpencodeServePeer(t) }

func TestHarnessesOpencode1EffortChoices(t *testing.T) {
	testenv.InstallOpencodeServe(t)
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal(err)
	}
	// Exclude installed harnesses; discovery uses only synthetic homes and peers.
	t.Setenv("PATH", filepath.Dir(bin))
	h := serveHandler(t, New(), "127.0.0.1:7777")
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://127.0.0.1:7777/api/sub/harnesses/refresh", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(w, req)
	var body struct{ Harnesses []sub.HarnessInfo }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, row := range body.Harnesses {
		if row.Name == "opencode" {
			if row.Models.Error != "" || !reflect.DeepEqual(row.Efforts, []string{"high", "low", "max"}) {
				t.Fatalf("Settings API lost opencode 1.x variants: %+v", row)
			}
			return
		}
	}
	t.Fatal("missing opencode row")
}

func TestServeHarnesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	bins := t.TempDir()
	t.Setenv("PATH", bins)
	if err := os.WriteFile(filepath.Join(bins, "codex"), []byte("#!/bin/sh\n[ \"$1\" = --version ] || exit 99\necho 'codex-cli 0.162.1'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "verified-versions.json"), []byte(`{"codex":{"version":"0.162.0"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	h := serveHandler(t, New(), "127.0.0.1:7777")
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://127.0.0.1:7777/api/sub/harnesses/refresh", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(w, req)
	var body struct {
		Harnesses []struct {
			Name                     string
			Installed                bool
			Version, VerifiedVersion string
			Default                  map[string]string
			Models                   struct {
				Items []string
				Error string
			}
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, h := range body.Harnesses {
		if h.Name == "codex" {
			if !h.Installed || h.Version != "0.162.1" || h.VerifiedVersion != "0.162.0" || h.Models.Error == "" || h.Models.Items == nil || len(h.Default) != 2 {
				t.Fatal(h)
			}
		}
	}
}

// The fixture permits listing/version commands only. Any model invocation or
// prompt would fail, and all home paths point to disposable test directories.
func TestHarnessesDiscoveryCacheAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	source := t.TempDir()
	t.Setenv("HOME", source)
	token := filepath.Join(source, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(token), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, nil, 0600); err != nil {
		t.Fatal(err)
	}
	bins := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	fail := filepath.Join(t.TempDir(), "fail")
	t.Setenv("PATH", bins)
	// Paths contain no shell interpolation; positional parameters receive them.
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo 'agy 1.2.3';;\nmodels) echo listing >> '" + calls + "'; if [ -f '" + fail + "' ]; then exit 1; fi; printf 'model-one\\tOne\\n';;\n*) exit 99;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bins, "agy"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	h := serveHandler(t, New(), "127.0.0.1:7777")
	get := func(query string) string {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:7777/api/sub/harnesses"+query, nil))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	postRefresh := func() string {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "http://127.0.0.1:7777/api/sub/harnesses/refresh", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if body := get(""); !strings.Contains(body, "model-one") {
		t.Fatal(body)
	}
	get("")
	count := func() int { data, _ := os.ReadFile(calls); return strings.Count(string(data), "listing") }
	if count() != 1 {
		t.Fatal(count())
	}
	// GET ignores ?refresh=1 and returns cached data without executing CLIs.
	get("?refresh=1")
	if count() != 1 {
		t.Fatalf("GET ?refresh=1 triggered discovery: %d", count())
	}
	postRefresh()
	if count() != 2 {
		t.Fatal(count())
	}
	if err := os.WriteFile(fail, nil, 0600); err != nil {
		t.Fatal(err)
	}
	body := postRefresh()
	if count() != 4 || !strings.Contains(body, "agy models:") || !strings.Contains(body, "model-one") {
		t.Fatal(count(), body)
	}
	get("")
	get("?refresh=1")
	if count() != 4 {
		t.Fatal("failure was not cached")
	}
}

func TestHarnessesRefreshWriteBoundary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	token := strings.Repeat("t", 32)
	t.Setenv("PACTIFY_SERVE_TOKEN", token)
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "")
	bins := t.TempDir()
	marker := filepath.Join(t.TempDir(), "executed")
	codexPath := filepath.Join(bins, "codex")
	script := "#!/bin/sh\nprintf executed >> '" + marker + "'\nif [ \"$1\" = --version ]; then echo 'codex-cli 0.163.0'; exit 0; fi\nexit 99\n"
	if err := os.WriteFile(codexPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bins)
	resolved, err := filepath.EvalSymlinks(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	catalog := map[string]any{}
	for _, name := range sub.Names() {
		if name == "codex" {
			catalog[name] = map[string]any{
				"Executable": map[string]any{"Path": resolved, "Size": info.Size(), "ModTime": info.ModTime().UTC()},
				"Version":    "0.162.1",
				"Models":     map[string]any{"items": []string{"cached-model"}, "error": "", "fetchedAt": now},
			}
			continue
		}
		catalog[name] = map[string]any{
			"Models": map[string]any{"items": []string{}, "error": "harness is not installed", "fetchedAt": now},
		}
	}
	catalogFile := filepath.Join(home, "harness-catalog.json")
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	beforeStat, err := os.Stat(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	handler := serveHandler(t, New(), "127.0.0.1:7777")
	call := func(method, path, auth, origin, ct, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7777/api/sub"+path, strings.NewReader(body))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
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
	// GET /api/sub/harnesses?refresh=1 must return cached metadata without running CLIs or rewriting catalog.
	w := call("GET", "/harnesses?refresh=1", token, "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":"0.162.1"`) || !strings.Contains(w.Body.String(), `"cached-model"`) {
		t.Fatalf("GET ?refresh=1: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("GET /api/sub/harnesses?refresh=1 executed harness CLI")
	}
	afterGet, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	afterGetStat, err := os.Stat(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterGet, data) || !afterGetStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Fatal("GET /api/sub/harnesses?refresh=1 rewrote harness-catalog.json")
	}
	// Token, Origin, method, and JSON guards protect POST /api/sub/harnesses/refresh.
	for _, tc := range []struct {
		method, auth, origin, ct, body string
		want                           int
	}{
		{"POST", "", "", "application/json", `{}`, 401},
		{"POST", token, "https://evil.test", "application/json", `{}`, 403},
		{"POST", token, "http://127.0.0.1:7778", "application/json", `{}`, 403},
		{"POST", token, "http://localhost:7777", "text/plain", `{}`, 415},
		{"POST", token, "http://localhost:7777", "application/json", `{bad`, 400},
		{"GET", token, "", "", "", 404},
	} {
		if w := call(tc.method, "/harnesses/refresh", tc.auth, tc.origin, tc.ct, tc.body); w.Code != tc.want {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("rejected refresh request executed harness CLI")
	}
	// Valid POST /api/sub/harnesses/refresh runs discovery and returns refreshed rows.
	w = call("POST", "/harnesses/refresh", token, "http://localhost:7777", "application/json", `{}`)
	var refreshed struct {
		Harnesses []sub.HarnessInfo `json:"harnesses"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &refreshed) != nil {
		t.Fatalf("POST refresh: %d %s", w.Code, w.Body.String())
	}
	foundCodex := false
	for _, row := range refreshed.Harnesses {
		if row.Name == "codex" {
			foundCodex = true
			if row.Version != "0.163.0" {
				t.Fatalf("expected refreshed version 0.163.0, got %+v", row)
			}
		}
	}
	if !foundCodex {
		t.Fatal("missing codex row in refresh response")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("POST refresh did not execute harness CLI: %v", err)
	}
	afterPost, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(afterPost, data) || !strings.Contains(string(afterPost), "0.163.0") {
		t.Fatalf("POST refresh did not persist updated catalog: %s", string(afterPost))
	}
}
