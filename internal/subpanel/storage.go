package subpanel

import (
	"net/http"

	"github.com/agentjoey/pactify/internal/sub"
)

func (s *Server) storageStatus(w http.ResponseWriter, r *http.Request) {
	reader := s.storageReader
	if reader == nil {
		reader = &s.storage
	}
	value, err := reader.Snapshot(r.Context(), s.store)
	if err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, value)
}
func (s *Server) storagePlan(w http.ResponseWriter, r *http.Request) {
	value, err := s.store.PlanStorage(r.Context(), sub.PruneOptions{})
	if err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, value)
}
