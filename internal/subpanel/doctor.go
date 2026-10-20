package subpanel

import (
	"github.com/agentjoey/pactify/internal/buildinfo"
	"net/http"
)

func (s *Server) doctor(w http.ResponseWriter, _ *http.Request) {
	// Doctor stores verified versions, not complete reports or a last-run timestamp.
	jsonResponse(w, map[string]any{"lastRun": nil, "harnesses": s.store.StoredHarnessVersions()})
}
func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	jsonResponse(w, map[string]string{"version": buildinfo.Version})
}
