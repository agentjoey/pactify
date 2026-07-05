package serve

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// writeGuard rejects cross-site browser-initiated mutating requests (SEC-1).
//
// serve binds localhost, but that does NOT stop a malicious web page in the
// user's browser from firing a CSRF POST at 127.0.0.1: cross-origin "simple
// requests" (e.g. Content-Type: text/plain, which json.Decoder happily parses)
// skip the CORS preflight entirely, and endpoints like
// POST /api/projects/{id}/orchestrate/run execute code. DNS rebinding makes the
// page itself same-origin with the attacker's hostname, bypassing any
// "localhost-only" assumption.
//
// The guard keys on the Origin header, which browsers attach to EVERY mutating
// request (same-origin or cross-origin, fetch or form) and non-browser clients
// (curl, CLI, Go) do not send at all:
//
//   - no Origin            → not a browser-mediated write → allow (CLI/scripts)
//   - Origin host loopback → the user's own dashboard (any port: embedded
//     dashboard, vite dev proxy, playwright) → allow
//   - Origin in the allowlist (PACTIFY_ALLOWED_ORIGINS, comma-separated full
//     origins, e.g. a reverse-proxy domain) → allow
//   - anything else, including "null" (sandboxed iframes) → 403
//
// Read requests (GET/HEAD) pass untouched: no registered GET mutates, and the
// browser's same-origin policy already blocks a cross-site page from READING
// responses. (Residual: DNS-rebinding page could read GET responses since the
// browser considers them same-origin; closing that needs per-instance token
// auth — tracked in backlog SEC-1 follow-up, not load-bearing for the
// code-exec hole this guard closes.)
func writeGuard(next http.Handler) http.Handler {
	allowed := allowedOrigins()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			origin := r.Header.Get("Origin")
			if origin != "" && !originAllowed(origin, allowed) {
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

// allowedOrigins reads the PACTIFY_ALLOWED_ORIGINS escape hatch (comma-separated
// full origins like "https://pact.example.com") for reverse-proxied dashboards,
// where the browser's Origin is the proxy's domain rather than loopback.
func allowedOrigins() map[string]struct{} {
	out := map[string]struct{}{}
	for _, o := range strings.Split(os.Getenv("PACTIFY_ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(strings.TrimSuffix(o, "/")); o != "" {
			out[o] = struct{}{}
		}
	}
	return out
}

// originAllowed reports whether the Origin header value names this machine's
// own dashboard: a loopback host on any port/scheme, or an explicit allowlist
// entry. "null" and unparseable origins are never allowed.
func originAllowed(origin string, allowed map[string]struct{}) bool {
	if _, ok := allowed[strings.TrimSuffix(origin, "/")]; ok {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false // includes "null" and garbage
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}
