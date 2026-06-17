package serve

import (
	"io"
	"net/http"

	"github.com/agentjoey/pactify/internal/agentmanifest"
)

func (s *Server) registerManifestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/manifests", s.handleManifestList)
	mux.HandleFunc("POST /api/manifests", s.handleManifestCreate)
	mux.HandleFunc("DELETE /api/manifests/{kind}", s.handleManifestDelete)
}

func (s *Server) handleManifestList(w http.ResponseWriter, _ *http.Request) {
	ms, _ := agentmanifest.Load()
	type item struct {
		Kind     string `json:"kind"`
		Binary   string `json:"binary"`
		Drivable bool   `json:"drivable"`
	}
	out := make([]item, 0, len(ms))
	for _, m := range ms {
		out = append(out, item{Kind: m.Kind, Binary: m.Binary, Drivable: m.Runner != nil})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManifestCreate(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	kind, err := agentmanifest.Install(b)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": kind})
}

func (s *Server) handleManifestDelete(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if err := agentmanifest.Remove(kind); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"removed": kind})
}
