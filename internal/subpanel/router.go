package subpanel

import (
	"net/http"
	"time"
)

func (s *Server) router(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.store.RouterSnapshot(time.Now())
	if err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, snapshot)
}
