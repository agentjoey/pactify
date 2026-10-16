package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/agentjoey/pactify/internal/sub"
)

// Writes are mounted only by serve, inside its existing Origin guard.
func (s *Server) registerRouterWrites(mux *http.ServeMux, store sub.Store) {
	mux.HandleFunc("PUT /api/sub/router", func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeErr(w, http.StatusUnsupportedMediaType, "application/json required")
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				writeErr(w, http.StatusRequestEntityTooLarge, "body exceeds 64 KiB")
			} else {
				writeErr(w, 400, "invalid request body")
			}
			return
		}
		var req struct {
			Version string          `json:"version"`
			Config  json.RawMessage `json:"config"`
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeErr(w, 400, "invalid JSON body")
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			writeErr(w, 400, "expected a single JSON value")
			return
		}
		err = store.SaveRouter(r.Context(), req.Version, req.Config)
		var configErr *sub.RouterConfigError
		switch {
		case errors.As(err, &configErr):
			writeJSON(w, 422, map[string]any{"errors": []map[string]string{{"field": configErr.Field, "message": configErr.Err.Error()}}})
		case errors.Is(err, sub.ErrRouterConflict):
			writeErr(w, 409, "Config changed elsewhere — reload to see the latest")
		case err != nil:
			writeErr(w, 500, err.Error())
		default:
			writeJSON(w, 200, map[string]bool{"saved": true})
		}
	})
	mux.HandleFunc("DELETE /api/sub/quota/{key}", func(w http.ResponseWriter, r *http.Request) {
		if err := store.ClearQuota(r.Context(), r.PathValue("key")); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"cleared": true})
	})
}
