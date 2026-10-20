package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// putRouter exercises the full Handler stack and proves allowed writes reach
// the real handler by checking router.json on disk.
func putRouter(t *testing.T, s *Server, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", "http://127.0.0.1:7777/api/sub/router", strings.NewReader(`{"version":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","config":{"aliases":{},"fallback":{},"quotaGroups":{}}}`))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	serveHandler(t, s, "127.0.0.1:7777").ServeHTTP(w, req)
	return w
}

func routerExists(t *testing.T) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(os.Getenv("PACTIFY_HOME"), "router.json"))
	return err == nil
}

// SEC-1: a malicious web page in the user's browser can fire cross-origin
// "simple" POSTs at the localhost API (no CORS preflight); before the guard
// this could reach worker-launching endpoints. Browsers attach
// Origin to every mutating request, non-browser clients don't — the guard
// rejects mutating requests whose Origin is present and not this dashboard.
func TestWriteGuardBlocksCrossOriginBrowserWrites(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "")
	s := New()

	for _, origin := range []string{
		"https://evil.example.com", // classic CSRF
		"http://attacker.com:7777", // DNS rebinding (page served from rebound host)
		"null",                     // sandboxed iframe
		"garbage not a url",        // unparseable
	} {
		w := putRouter(t, s, origin)
		if w.Code != http.StatusForbidden {
			t.Errorf("Origin %q: code = %d, want 403", origin, w.Code)
		}
		if !strings.Contains(w.Body.String(), "cross-origin write rejected") {
			t.Errorf("Origin %q: body should carry the guard error, got %s", origin, w.Body)
		}
	}
	if routerExists(t) {
		t.Error("blocked write must not reach the handler (router was written)")
	}
}

func TestWriteGuardAllowsOwnDashboardAndCLI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	s := New()

	for _, origin := range []string{"", "http://127.0.0.1:7777", "http://localhost:7777", "http://[::1]:7777"} {
		t.Run(origin, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			if w := putRouter(t, s, origin); w.Code != http.StatusOK {
				t.Fatalf("Origin %q: code = %d (%s), want 200", origin, w.Code, w.Body)
			}
			if !routerExists(t) {
				t.Fatal("request did not save router.json")
			}
		})
	}
}

// A reverse-proxied dashboard has a non-loopback Origin; PACTIFY_ALLOWED_ORIGINS
// is the explicit opt-in for exactly that origin.
func TestWriteGuardAllowlistEnv(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "https://pact.example.com, https://other.example.com/")

	s := New()
	for _, origin := range []string{"https://pact.example.com", "https://other.example.com"} {
		t.Run(origin, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			if w := putRouter(t, s, origin); w.Code != http.StatusOK || !routerExists(t) {
				t.Fatalf("allowlisted origin: code = %d (%s), want 200 + router written", w.Code, w.Body)
			}
		})
	}
	if w := putRouter(t, s, "https://evil.example.com"); w.Code != http.StatusForbidden {
		t.Errorf("non-allowlisted origin: code = %d, want 403", w.Code)
	}
}

// Reads on an accepted Host do not require a matching Origin.
func TestWriteGuardIgnoresReads(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	s := New()
	r := httptest.NewRequest("GET", "http://127.0.0.1:7777/api/sub/jobs", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	serveHandler(t, s, "127.0.0.1:7777").ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET with foreign Origin: code = %d, want 200 (guard must not touch reads)", w.Code)
	}
}
