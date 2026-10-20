package serve

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestServeListenAddressPolicy(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7777", "[::1]:7777", "localhost:7777"} {
		t.Run(addr, func(t *testing.T) {
			t.Setenv("PACTIFY_SERVE_TOKEN", "")
			t.Setenv("PACTIFY_HOME", t.TempDir())
			h := serveHandler(t, New(), addr)
			for _, host := range []string{"127.0.0.1:7777", "localhost:7777", "[::1]:7777"} {
				r := httptest.NewRequest("GET", "http://"+host+"/api/sub/jobs", nil)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != 200 {
					t.Errorf("bind %s Host %s: %d", addr, host, w.Code)
				}
			}
		})
	}
	token := strings.Repeat("t", 32)
	t.Setenv("PACTIFY_SERVE_TOKEN", token)
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "https://[2001:db8::1]:8443, https://dash.example.com")
	for _, addr := range []string{"192.0.2.1:7777", "[2001:db8::2]:7777", "0.0.0.0:7777", "[::]:7777", ":7777"} {
		security, err := newSecurityConfig(addr)
		if err != nil {
			t.Fatal(err)
		}
		if addr == ":7777" {
			continue
		} // No named Host for an omitted bind host.
		for _, bindHost := range []string{security.host, "server.example.com"} {
			// A hostname resolved to this IP uses the same policy, preserving its name.
			security.host = bindHost
			h := security.guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
			for _, host := range []string{net.JoinHostPort(bindHost, "7777"), "[2001:db8::1]", "[2001:db8::1]:8443", "dash.example.com", "localhost:7777", "evil.example", net.JoinHostPort(bindHost, "5199")} {
				r := httptest.NewRequest("GET", "/api/sub/jobs", nil)
				r.Host = host
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				want := 204
				if host == "localhost:7777" || host == "evil.example" || host == net.JoinHostPort(bindHost, "5199") {
					want = 403
				}
				if w.Code != want {
					t.Errorf("bind %s Host %s: %d, want %d", addr, host, w.Code, want)
				}
			}
		}
	}
}

func TestServeSSESecurityBoundary(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	token := strings.Repeat("t", 32)
	t.Setenv("PACTIFY_SERVE_TOKEN", token)
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Create("stream", sub.Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = sub.StatusSucceeded
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.LogPath(st.ID), []byte("{\"kind\":\"message\",\"text\":\"stream works\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(nil)
	ts.Config.Handler = serveHandler(t, New(), ts.Listener.Addr().String())
	ts.Start()
	defer ts.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	for _, tc := range []struct {
		host, auth string
		status     int
	}{
		{"evil.example", "Bearer " + token, 403},
		{ts.Listener.Addr().String(), "", 401},
		{ts.Listener.Addr().String(), "Bearer wrong", 401},
		{ts.Listener.Addr().String(), "Bearer " + token, 200},
	} {
		req, err := http.NewRequest("GET", ts.URL+"/api/sub/jobs/stream/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = tc.host
		req.Header.Set("Authorization", tc.auth)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tc.status {
			t.Fatalf("SSE status %d, want %d: %s", resp.StatusCode, tc.status, data)
		}
		if tc.status == 200 && (resp.Header.Get("Content-Type") != "text/event-stream" || !strings.Contains(string(data), "stream works") || !strings.Contains(string(data), "event: done")) {
			t.Fatalf("SSE did not stream: %s", data)
		}
	}
}

func TestServeTokenAllAPIMethods(t *testing.T) {
	token := strings.Repeat("t", 32)
	t.Setenv("PACTIFY_SERVE_TOKEN", token)
	h := guardedTestRoutes(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		for _, auth := range []string{"", "Bearer " + token} {
			r := httptest.NewRequest(method, "http://localhost:7777/api/sub/router", nil)
			r.Header.Set("Authorization", auth)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := 401
			if auth != "" {
				want = 204
			}
			if w.Code != want {
				t.Errorf("%s: %d, want %d", method, w.Code, want)
			}
		}
	}
}

func TestServeUnnamedBindHost(t *testing.T) {
	t.Setenv("PACTIFY_SERVE_TOKEN", strings.Repeat("t", 32))
	security, err := newSecurityConfig(":7777")
	if err != nil {
		t.Fatal(err)
	}
	// Run supplies the actual wildcard listener address after binding.
	security.listenAddr = &net.TCPAddr{IP: net.IPv6zero, Port: 7777}
	h := security.guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		host   string
		status int
	}{{"[::]:7777", 204}, {":7777", 403}, {"localhost:7777", 403}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("Host %q: %d, want %d", tc.host, w.Code, tc.status)
		}
	}
}
