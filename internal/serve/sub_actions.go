package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/agentjoey/pactify/internal/sub"
)

// registerJobWrites is mounted only inside serve's Origin guard. Passing the
// launcher keeps request handling testable without starting a real harness.
func (s *Server) registerJobWrites(mux *http.ServeMux, store sub.Store, startWorker func(string) error) {
	for _, action := range []string{"cancel", "resume", "apply", "discard"} {
		mux.HandleFunc("POST /api/sub/jobs/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				writeErr(w, 415, "application/json required")
				return
			}
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
			if err != nil {
				var limit *http.MaxBytesError
				if errors.As(err, &limit) {
					writeErr(w, 413, "body exceeds 64 KiB")
				} else {
					writeErr(w, 400, "invalid request body")
				}
				return
			}
			var req struct {
				Brief string `json:"brief"`
			}
			if len(bytes.TrimSpace(data)) > 0 {
				decoder := json.NewDecoder(bytes.NewReader(data))
				decoder.DisallowUnknownFields()
				if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
					writeErr(w, 400, "JSON object required")
					return
				}
				if decoder.Decode(&req) != nil {
					writeErr(w, 400, "invalid JSON body")
					return
				}
				if decoder.Decode(new(any)) != io.EOF {
					writeErr(w, 400, "expected a single JSON value")
					return
				}
			}
			if action == "resume" && len(bytes.TrimSpace([]byte(req.Brief))) == 0 {
				writeErr(w, 400, "task brief must not be empty")
				return
			}
			id := r.PathValue("id")
			st, err := store.Load(id)
			if err != nil {
				if os.IsNotExist(err) {
					writeErr(w, 404, "job not found")
				} else {
					writeErr(w, 400, err.Error())
				}
				return
			}
			if err := store.CheckAction(st, action); err != nil {
				writeErr(w, 409, err.Error())
				return
			}
			switch action {
			case "cancel":
				_, err = store.CancelJob(r.Context(), id)
			case "resume":
				var child sub.State
				child, err = store.ResumeDashboardJob(r.Context(), id, req.Brief)
				if err == nil {
					if err = startWorker(child.ID); err == nil {
						writeJSON(w, 200, map[string]string{"jobId": child.ID})
						return
					}
					child.Status = sub.StatusFailed
					child.Error = err.Error()
					err = errors.Join(err, store.SaveFallback(child))
				}
			case "apply":
				err = store.ApplyWorktree(id)
			case "discard":
				err = store.DiscardWorktree(id)
			}
			if err != nil {
				var conflict *sub.ActionConflict
				if errors.Is(err, sub.ErrDashboardJobLimit) {
					writeErr(w, 429, err.Error())
				} else if errors.As(err, &conflict) {
					writeErr(w, 409, err.Error())
				} else {
					writeErr(w, 500, err.Error())
				}
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
		})
	}
}
