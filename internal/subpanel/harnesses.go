package subpanel

import "net/http"

func (s *Server) harnesses(w http.ResponseWriter, r *http.Request) {
	// Read-only: ignore query parameters and return cached harness metadata.
	// Explicit refresh is served by a serve-only POST endpoint.
	rows, err := s.store.Harnesses(r.Context(), false)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		jsonResponse(w, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, map[string]any{"harnesses": rows})
}
