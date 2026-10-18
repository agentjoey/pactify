package serve

import (
	"encoding/json"
	"net/http"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/agentjoey/pactify/internal/subpanel"
)

// Server serves the harness dashboard.
type Server struct{}

// New builds a harness dashboard server.
func New() *Server { return &Server{} }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	store, err := sub.DefaultStore()
	if err != nil {
		mux.HandleFunc("/api/sub/", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		})
	} else {
		s.registerRouterWrites(mux, store)
		s.registerJobWrites(mux, store, store.StartDashboardWorker)
		mux.Handle("/api/sub/", http.StripPrefix("/api/sub", subpanel.ReadOnlyHandler(store)))
	}
	// Unknown API routes must not fall through to the SPA index.
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", dashboardHandler())
	// SEC-1: every mutating route above goes through the browser-CSRF guard —
	// register new routes inside this mux so they inherit it.
	return writeGuard(mux)
}
