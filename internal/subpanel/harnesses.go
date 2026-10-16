package subpanel

import "net/http"

func (s *Server) harnesses(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Harnesses(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		jsonResponse(w, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, map[string]any{"harnesses": rows})
}
