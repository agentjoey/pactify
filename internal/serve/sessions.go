package serve

import (
	"net/http"
	"os/exec"

	"github.com/agentjoey/pactify/internal/sessions"
)

// sessionRunFn runs an agent CLI and returns its combined output. A package var
// so tests fake the CLIs without spawning processes.
var sessionRunFn sessions.Runner = func(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *Server) registerSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents/{kind}/sessions", s.handleSessionsList)
	mux.HandleFunc("POST /api/agents/{kind}/sessions/prune", s.handleSessionsPrune)
}

type sessionsListResp struct {
	Kind      string `json:"kind"`
	Supported bool   `json:"supported"`
	CanPrune  bool   `json:"can_prune"`
	Output    string `json:"output"`
}

// handleSessionsList returns the kind's session listing (or supported=false with
// no output when the kind has no known session command).
func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	resp := sessionsListResp{Kind: kind, Supported: sessions.Supported(kind), CanPrune: sessions.CanPrune(kind)}
	if resp.Supported {
		out, err := sessions.Manager{Run: sessionRunFn}.List(kind)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		resp.Output = out
	}
	writeJSON(w, http.StatusOK, resp)
}

type sessionsPruneResp struct {
	Kind    string `json:"kind"`
	Skipped bool   `json:"skipped"` // true = kind has no verified prune command (graceful no-op)
	Output  string `json:"output"`
}

// handleSessionsPrune prunes the kind's sessions. Unsupported kinds return
// skipped=true (a no-op, not an error) so the UI can say "nothing to prune".
func (s *Server) handleSessionsPrune(w http.ResponseWriter, r *http.Request) {
	if !s.requireSeat(w) {
		return
	}
	kind := r.PathValue("kind")
	out, skipped, err := sessions.Manager{Run: sessionRunFn}.Prune(kind)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sessionsPruneResp{Kind: kind, Skipped: skipped, Output: out})
}
