package serve

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/agentjoey/pactify/internal/sub"
)

// The existing dashboard guard supplies token and exact Origin protection.
// The panel mounts only ReadOnlyHandler and never registers this endpoint.
func (s *Server) registerStorageWrites(mux *http.ServeMux, store sub.Store, reader *sub.StorageReader) {
	mux.HandleFunc("POST /api/sub/storage/prune", func(w http.ResponseWriter, r *http.Request) {
		ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || ct != "application/json" {
			writeErr(w, 415, "application/json required")
			return
		}
		var request struct {
			Version string `json:"version"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&request); err != nil || request.Version == "" {
			writeErr(w, 400, "review a storage plan and provide its version")
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			writeErr(w, 400, "expected a single JSON value")
			return
		}
		result, err := store.PruneStorage(r.Context(), sub.PruneOptions{ExpectedVersion: request.Version})
		reader.Invalidate()
		if errors.Is(err, sub.ErrStoragePlanChanged) {
			writeErr(w, 409, err.Error())
			return
		}
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, result)
	})
}
