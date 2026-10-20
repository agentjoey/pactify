package subpanel

import (
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestRejectedHostBrowserPage(t *testing.T) {
	s, _ := fixture(t)
	for _, host := range []string{"localhost:7777", "attacker.example", "[::1]:7777", "<script>alert(1)</script>"} {
		t.Run(host, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://127.0.0.1/?token="+s.launchToken, nil)
			r.Host = host
			r.Header.Set("Accept", "text/html,application/xhtml+xml")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			body := w.Body.String()
			if w.Code != 403 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(body, "127.0.0.1") || !strings.Contains(body, "pactify sub panel") {
				t.Fatalf("expected friendly HTML 403: %d %s", w.Code, body)
			}
			for _, forbidden := range []string{s.token, s.launchToken, "<script", "<style", "style=", "attacker.example"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("403 contains %q", forbidden)
				}
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "style-src 'self'") || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("missing security headers")
			}
		})
	}
	if w := request(s, "GET", "/?token="+s.launchToken, ""); w.Code != 200 {
		t.Fatal("rejected Host consumed launch token")
	}
}

func TestRejectedHostAPIStaysPlain(t *testing.T) {
	s, _ := fixture(t)
	for _, path := range []string{"/api/jobs", "/api", "/api/jobs/job-00/resume"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1"+path, nil)
		r.Host = "localhost:7777"
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 || w.Body.String() != "loopback host required\n" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
			t.Fatalf("API 403 changed: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestSecurityIncompleteDirectoryDoesNotBreakPanel(t *testing.T) {
	s, store := fixture(t)
	if err := os.Mkdir(store.Dir("half-created"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/jobs?all=1", "/api/jobs/job-00"} {
		if w := request(s, "GET", path, s.token); w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
func TestSecurityURLTokenSingleUseAndNoCookie(t *testing.T) {
	s, _ := fixture(t)
	first := request(s, "GET", "/?token="+s.launchToken, "")
	if first.Code != 200 || !strings.Contains(first.Body.String(), `content="`+s.token+`"`) {
		t.Fatal(first.Code)
	}
	if w := request(s, "GET", "/?token="+s.launchToken, ""); w.Code != 401 {
		t.Fatalf("reused URL token: %d", w.Code)
	}
	if len(first.Result().Cookies()) != 0 {
		t.Fatal("exchange exposes token in cookie")
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/jobs", nil)
	r.Header.Set("Cookie", "pactify_panel_"+s.token[:12]+"="+s.token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("cookie authenticated: %d", w.Code)
	}
}

func TestSecurityExchangeIsAtomic(t *testing.T) {
	s, _ := fixture(t)
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- request(s, "GET", "/?token="+s.launchToken, "").Code }()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 401 {
			t.Fatal(code)
		}
	}
	if success != 1 {
		t.Fatalf("exchanges succeeded: %d", success)
	}
	if w := request(s, "GET", "/api/jobs", s.launchToken); w.Code != 401 {
		t.Fatal("launch secret accepted as session")
	}
	if w := request(s, "GET", "/", ""); w.Code != 200 || strings.Contains(w.Body.String(), s.token) {
		t.Fatal("refreshed shell exposed session")
	}
}
