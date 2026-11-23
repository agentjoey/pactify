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

// The existing dashboard guard supplies token and exact Origin protection.
// The panel mounts only ReadOnlyHandler and never registers this endpoint.
func (s *Server) registerHarnessWrites(mux *http.ServeMux, store sub.Store) {
	mux.HandleFunc("POST /api/sub/harnesses/refresh", func(w http.ResponseWriter, r *http.Request) {
		ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || ct != "application/json" {
			writeErr(w, 415, "application/json required")
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				writeErr(w, 413, "body exceeds 4 KiB")
			} else {
				writeErr(w, 400, "invalid request body")
			}
			return
		}
		if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 {
			if bytes.Equal(trimmed, []byte("null")) {
				writeErr(w, 400, "JSON object required")
				return
			}
			var req struct{}
			decoder := json.NewDecoder(bytes.NewReader(trimmed))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&req) != nil {
				writeErr(w, 400, "invalid JSON body")
				return
			}
			if decoder.Decode(new(any)) != io.EOF {
				writeErr(w, 400, "expected a single JSON value")
				return
			}
		}
		rows, err := store.Harnesses(r.Context(), true)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"harnesses": rows})
	})
}
