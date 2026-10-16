package serve

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	h := New(nil).Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/sub/harnesses?refresh=1", nil))
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
	h := New(nil).Handler()
	get := func(query string) string {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/sub/harnesses"+query, nil))
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
	get("?refresh=1")
	if count() != 2 {
		t.Fatal(count())
	}
	if err := os.WriteFile(fail, nil, 0600); err != nil {
		t.Fatal(err)
	}
	body := get("?refresh=1")
	if count() != 3 || !strings.Contains(body, "agy models --model gemini-3.8-flash-low:") || !strings.Contains(body, "model-one") {
		t.Fatal(count(), body)
	}
	get("")
	if count() != 3 {
		t.Fatal("failure was not cached")
	}
}
