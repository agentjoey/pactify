package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeHostBoundary(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "https://dash.example.com, https://proxy.example.com:8443/")
	h := serveHandler(t, New(), "127.0.0.1:7777")
	for _, path := range []string{"/", "/assets/missing.js", "/api/sub/jobs", "/api/sub/jobs/missing/events", "/api/missing"} {
		for _, host := range []string{"", "evil.example", "127.0.0.1", "localhost:5199", "127.0.0.2:7777", "[::1]:5199", "dash.example.com:7777", "proxy.example.com:9999", "localhost:07777", "localhost:7777@evil.example"} {
			r := httptest.NewRequest("GET", path, nil)
			r.Host = host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Errorf("%s Host %q: got %d, want 403", path, host, w.Code)
			}
		}
	}
	for _, host := range []string{"127.0.0.1:7777", "localhost:7777", "[::1]:7777", "dash.example.com", "proxy.example.com", "proxy.example.com:8443"} {
		r := httptest.NewRequest("GET", "/api/sub/jobs", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Errorf("allowed Host %q: %d %s", host, w.Code, w.Body)
		}
	}
}

func TestServeExactWriteOrigins(t *testing.T) {
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "https://dash.example.com, https://proxy.example.com:8443/")
	h := guardedTestRoutes(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, origin := range []string{"http://localhost:5199", "https://localhost:7777", "http://127.0.0.2:7777", "http://localhost", "http://localhost:7777/", "http://localhost:7777/path", "http://user@localhost:7777", "http://localhost:7777?query", "null", "https://dash.example.com/", "https://dash.example.com:443", "https://proxy.example.com"} {
			r := httptest.NewRequest(method, "/api/sub/router", nil)
			r.Host = "127.0.0.1:7777"
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 || !strings.Contains(w.Body.String(), "PACTIFY_ALLOWED_ORIGINS") {
				t.Errorf("%s Origin %q: %d %s", method, origin, w.Code, w.Body)
			}
		}
		for _, origin := range []string{"", "http://localhost:7777", "http://127.0.0.1:7777", "http://[::1]:7777", "https://dash.example.com", "https://proxy.example.com:8443"} {
			r := httptest.NewRequest(method, "/api/sub/router", nil)
			r.Host = "localhost:7777"
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 204 {
				t.Errorf("allowed %s Origin %q: %d %s", method, origin, w.Code, w.Body)
			}
		}
	}
}

func TestServeAPIToken(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	token := strings.Repeat("t", 32)
	t.Setenv("PACTIFY_SERVE_TOKEN", token)
	h := serveHandler(t, New(), "127.0.0.1:7777")
	for _, path := range []string{"/api/sub/jobs", "/api/sub/jobs/missing/events", "/api/unknown"} {
		for _, auth := range []string{"", "Bearer wrong", "Basic " + token, "Bearer " + token + "extra"} {
			r := httptest.NewRequest("GET", path, nil)
			r.Host = "127.0.0.1:7777"
			r.Header.Set("Authorization", auth)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Errorf("%s invalid auth: %d, want 401", path, w.Code)
			}
		}
	}
	for _, path := range []string{"/", "/api/sub/jobs"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:7777"
		if path != "/" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Errorf("%s: %d %s", path, w.Code, w.Body)
		}
	}
}

func TestServeSecurityHeaders(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h := serveHandler(t, New(), "127.0.0.1:7777")
	for _, path := range []string{"/", "/api/sub/jobs", "/api/missing"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:7777"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		for key, want := range map[string]string{"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Content-Security-Policy": "frame-ancestors 'none'"} {
			if got := w.Header().Get(key); got != want {
				t.Errorf("%s %s = %q, want %q", path, key, got, want)
			}
		}
	}
}

func TestServeRefusesUnsafeListen(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "[::]:0", ":0", "127.0.0.1:0"} {
		for _, token := range []string{"", "short"} {
			if token == "" && addr == "127.0.0.1:0" {
				continue
			}
			t.Run(addr+"/"+token, func(t *testing.T) {
				t.Setenv("PACTIFY_SERVE_TOKEN", token)
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				err := New().Run(ctx, addr)
				if err == nil || !strings.Contains(err.Error(), "PACTIFY_SERVE_TOKEN") || !strings.Contains(err.Error(), "32") {
					t.Errorf("Run(%q) = %v, want actionable token error", addr, err)
				}
			})
		}
	}
}

func serveHandler(t *testing.T, s *Server, addr string) http.Handler {
	t.Helper()
	h, err := s.Handler(addr)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func guardedTestRoutes(t *testing.T, h http.Handler) http.Handler {
	t.Helper()
	security, err := newSecurityConfig("127.0.0.1:7777")
	if err != nil {
		t.Fatal(err)
	}
	return security.guard(h)
}

func TestServeDoctorAndInfoBoundaries(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "")
	t.Setenv("PACTIFY_SERVE_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	h, err := New().Handler("127.0.0.1:7777")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/sub/doctor", "/api/sub/info"} {
		for _, tc := range []struct {
			method, host string
			want         int
		}{
			{"GET", "evil.example:7777", http.StatusForbidden},
			{"POST", "evil.example:7777", http.StatusForbidden},
			{"GET", "127.0.0.1:7777", http.StatusOK},
			{"POST", "127.0.0.1:7777", http.StatusMethodNotAllowed},
			{"HEAD", "127.0.0.1:7777", http.StatusMethodNotAllowed},
		} {
			r := httptest.NewRequest(tc.method, path, nil)
			r.Host = tc.host
			if tc.method == "POST" {
				r.Header.Set("Origin", "http://127.0.0.1:7777")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Errorf("%s %s Host %s: %d %s", tc.method, path, tc.host, w.Code, w.Body.String())
			}
			if tc.want == http.StatusMethodNotAllowed && w.Header().Get("Allow") != "GET" {
				t.Errorf("%s Allow: %q", path, w.Header().Get("Allow"))
			}
		}
	}
}
