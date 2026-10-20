package serve

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

type securityConfig struct {
	listenAddr *net.TCPAddr
	host       string
	token      string
	allowed    map[string]struct{}
}

func newSecurityConfig(addr string) (*securityConfig, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid serve address: %w", err)
	}
	token := os.Getenv("PACTIFY_SERVE_TOKEN")
	if token != "" && utf8.RuneCountInString(token) < 32 {
		return nil, fmt.Errorf("PACTIFY_SERVE_TOKEN must contain at least 32 characters")
	}
	resolved, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolve serve address: %w", err)
	}
	if !resolved.IP.IsLoopback() && token == "" {
		return nil, fmt.Errorf("non-loopback serve address requires PACTIFY_SERVE_TOKEN (at least 32 characters); set it for an authenticating reverse proxy or use --addr 127.0.0.1:7777")
	}
	return &securityConfig{listenAddr: resolved, host: host, token: token, allowed: allowedOrigins()}, nil
}

func (s *securityConfig) guard(next http.Handler) http.Handler {
	hosts := map[string]struct{}{}
	ownOrigins := map[string]struct{}{}
	var localHosts []string
	if s.listenAddr.IP.IsLoopback() {
		localHosts = []string{"127.0.0.1", "localhost", "::1"}
	} else {
		host := s.host
		if host == "" && s.listenAddr.IP != nil {
			host = s.listenAddr.IP.String()
		}
		if host != "" {
			localHosts = []string{host}
		}
	}
	for _, host := range localHosts {
		authority := net.JoinHostPort(host, strconv.Itoa(s.listenAddr.Port))
		hosts[authority] = struct{}{}
		// Browser writes on a remote deployment require an explicit proxy origin.
		if s.listenAddr.IP.IsLoopback() {
			ownOrigins["http://"+authority] = struct{}{}
		}
	}
	for origin := range s.allowed {
		if u := parseOrigin(origin); u != nil {
			hosts[u.Host] = struct{}{}
			host := u.Hostname()
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			hosts[host] = struct{}{}
		}
	}
	writes := writeGuard(next, ownOrigins, s.allowed)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		if _, ok := hosts[r.Host]; !ok || r.Host == "" {
			writeErr(w, http.StatusForbidden, "host not allowed")
			return
		}
		if s.token != "" && strings.HasPrefix(r.URL.Path, "/api/") {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(s.token)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				writeErr(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		writes.ServeHTTP(w, r)
	})
}

// writeGuard requires an exact dashboard or authenticated reverse-proxy Origin
// for browser writes. CLI/scripts without an Origin remain supported.
func writeGuard(next http.Handler, ownOrigins, allowed map[string]struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			origin := r.Header.Get("Origin")
			// An explicitly empty header is not an absent Origin.
			if _, present := r.Header["Origin"]; present && (len(r.Header.Values("Origin")) != 1 || !originAllowed(origin, ownOrigins, allowed)) {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": "cross-origin write rejected: origin " + origin +
						" is not this dashboard (set PACTIFY_ALLOWED_ORIGINS to allow a reverse-proxy origin)",
				})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// allowedOrigins reads comma-separated full origins for a reverse proxy that
// performs authentication. Preserve the existing environment parsing contract.
func allowedOrigins() map[string]struct{} {
	out := map[string]struct{}{}
	for _, o := range strings.Split(os.Getenv("PACTIFY_ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(strings.TrimSuffix(o, "/")); o != "" {
			out[o] = struct{}{}
		}
	}
	return out
}

// parseOrigin rejects URL components that cannot occur in a browser Origin.
func parseOrigin(origin string) *url.URL {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(origin, "#") {
		return nil
	}
	return u
}

func originAllowed(origin string, ownOrigins, allowed map[string]struct{}) bool {
	if parseOrigin(origin) == nil {
		return false
	}
	if _, ok := ownOrigins[origin]; ok {
		return true
	}
	_, ok := allowed[origin]
	return ok
}
