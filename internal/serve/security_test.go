package serve

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// postManifest fires a POST through the FULL Handler() stack (guard included)
// with the given Origin header ("" = none, like curl/CLI), returning the
// recorder. The manifests endpoint is the lightest registered mutating route
// (needs only PACTIFY_HOME). Allow-path assertions additionally check the
// manifest file actually landed, so a 200 from the SPA fallback route can
// never pass vacuously.
func postManifest(t *testing.T, s *Server, origin, kind string) *httptest.ResponseRecorder {
	t.Helper()
	body := []byte("kind=\"" + kind + "\"\nbinary=\"" + kind + "\"\n[runner]\nargs=[\"run\",\"{briefing}\"]\n")
	r := httptest.NewRequest("POST", "/api/manifests", bytes.NewReader(body))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// manifestExists reports whether the manifest for kind was actually written
// under PACTIFY_HOME (proof the request reached the real handler).
func manifestExists(t *testing.T, kind string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(os.Getenv("PACTIFY_HOME"), ".pactify", "agents", kind+".toml"))
	return err == nil
}

// SEC-1: a malicious web page in the user's browser can fire cross-origin
// "simple" POSTs at the localhost API (no CORS preflight); before the guard
// this reached code-executing endpoints like orchestrate/run. Browsers attach
// Origin to every mutating request, non-browser clients don't — the guard
// rejects mutating requests whose Origin is present and not this dashboard.
func TestWriteGuardBlocksCrossOriginBrowserWrites(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	s := New(nil)
	s.SetSeat("test")

	for _, origin := range []string{
		"https://evil.example.com", // classic CSRF
		"http://attacker.com:7777", // DNS rebinding (page served from rebound host)
		"null",                     // sandboxed iframe
		"garbage not a url",        // unparseable
	} {
		w := postManifest(t, s, origin, "evilkind")
		if w.Code != http.StatusForbidden {
			t.Errorf("Origin %q: code = %d, want 403", origin, w.Code)
		}
		if !strings.Contains(w.Body.String(), "cross-origin write rejected") {
			t.Errorf("Origin %q: body should carry the guard error, got %s", origin, w.Body)
		}
	}
	if manifestExists(t, "evilkind") {
		t.Error("blocked write must not reach the handler (manifest was written)")
	}
}

func TestWriteGuardAllowsOwnDashboardAndCLI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	s := New(nil)
	s.SetSeat("test")

	cases := []struct{ origin, kind string }{
		{"", "cli1"},                      // curl / CLI / Go clients send no Origin
		{"http://127.0.0.1:7777", "own1"}, // embedded dashboard
		{"http://localhost:5199", "dev1"}, // vite dev proxy port
		{"http://[::1]:7777", "six1"},     // IPv6 loopback
	}
	for _, c := range cases {
		if w := postManifest(t, s, c.origin, c.kind); w.Code != http.StatusOK {
			t.Errorf("Origin %q: code = %d (%s), want 200", c.origin, w.Code, w.Body)
		}
		if !manifestExists(t, c.kind) {
			t.Errorf("Origin %q: request never reached the real handler (no manifest written)", c.origin)
		}
	}
}

// A reverse-proxied dashboard has a non-loopback Origin; PACTIFY_ALLOWED_ORIGINS
// is the explicit opt-in for exactly that origin.
func TestWriteGuardAllowlistEnv(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "https://pact.example.com, https://other.example.com/")

	s := New(nil)
	s.SetSeat("test")
	if w := postManifest(t, s, "https://pact.example.com", "prox1"); w.Code != http.StatusOK || !manifestExists(t, "prox1") {
		t.Errorf("allowlisted origin: code = %d (%s), want 200 + manifest written", w.Code, w.Body)
	}
	if w := postManifest(t, s, "https://other.example.com", "prox2"); w.Code != http.StatusOK || !manifestExists(t, "prox2") {
		t.Errorf("allowlisted origin (trailing-slash entry): code = %d (%s), want 200 + manifest written", w.Code, w.Body)
	}
	if w := postManifest(t, s, "https://evil.example.com", "prox3"); w.Code != http.StatusForbidden {
		t.Errorf("non-allowlisted origin: code = %d, want 403", w.Code)
	}
}

// Reads stay open: no registered GET mutates, and same-origin policy already
// prevents a cross-site page from reading responses.
func TestWriteGuardIgnoresReads(t *testing.T) {
	s := New(nil)
	r := httptest.NewRequest("GET", "/api/projects", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET with foreign Origin: code = %d, want 200 (guard must not touch reads)", w.Code)
	}
}
