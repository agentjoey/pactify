package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/audit"
	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

func (s *Server) registerCockpitRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/projects/{id}/cockpit/prompt", s.handleCockpitPrompt)
	mux.HandleFunc("GET /api/projects/{id}/cockpit/stream", s.handleCockpitStream)
	mux.HandleFunc("POST /api/projects/{id}/cockpit/permission", s.handleCockpitPermission)
	mux.HandleFunc("POST /api/projects/{id}/cockpit/cancel", s.handleCockpitCancel)
	mux.HandleFunc("GET /api/projects/{id}/cockpit/status", s.handleCockpitStatus)
}

// ensureCockpit lazily creates the cockpit.Manager used by the HTTP handlers.
// Tests may pre-inject s.cockpit; in that case this is a no-op.
func (s *Server) ensureCockpit() error {
	if s.cockpit != nil {
		return nil
	}
	baseDir := filepath.Join(os.TempDir(), "pactify-cockpit")
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return fmt.Errorf("mkdir cockpit base: %w", err)
	}
	s.cockpit = cockpit.NewManagerCtxAudit(context.Background(), baseDir, s.backendForKey, s.cockpitAudit)
	return nil
}

// cockpitAudit is the live audit sink for cockpit tool starts and approval
// decisions. It is best-effort: errors are ignored so audit plumbing can never
// block agent pumps.
func (s *Server) cockpitAudit(key cockpit.SessionKey, ev cockpit.AuditEvent) {
	s.pmu.RLock()
	p, ok := s.projects[key.Project]
	s.pmu.RUnlock()
	if !ok {
		return
	}

	_ = audit.Append(audit.Record{
		TS:       time.Now().UTC().Format(time.RFC3339),
		Project:  key.Project,
		Repo:     p.Path,
		Seat:     key.Seat,
		Kind:     "cockpit",
		Tool:     ev.Tool,
		Summary:  ev.Summary,
		Risk:     ev.Risk,
		Decision: ev.Decision,
		Session:  key.Project + "/" + key.Seat,
	})
}

// backendForKey selects a real Backend for a (project, seat) pair based on the
// seat's agent kind recorded in the project's folded state.
func (s *Server) backendForKey(key cockpit.SessionKey) (cockpit.Backend, error) {
	s.pmu.RLock()
	_, ok := s.projects[key.Project]
	s.pmu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown project %q", key.Project)
	}

	kind := s.seatKind(key.Project, key.Seat)
	switch kind {
	case "claude-code":
		return cockpit.NewClaudeBackend(), nil
	case "codex-cli":
		return cockpit.NewCodexBackend(), nil
	case "kimi-cli", "gemini-cli":
		return cockpit.NewACPBackend(kind), nil
	default:
		return nil, fmt.Errorf("seat %q kind %q is not deep-integration (claude-code/codex-cli/kimi-cli/gemini-cli only)", key.Seat, kind)
	}
}

// seatKind returns the agent kind for seat in project by folding the project's
// .pact/log.jsonl. It reuses the memoized full-fold helper used by the state
// endpoints; if the seat is not in the roster it returns "".
func (s *Server) seatKind(project, seat string) string {
	s.pmu.RLock()
	p, ok := s.projects[project]
	s.pmu.RUnlock()
	if !ok {
		return ""
	}

	// Prefer the cached full fold so repeated cockpit calls don't re-read the log.
	dto, _, err := s.projectStateFull(project, p.Path)
	if err != nil {
		// Fallback to a direct fold if the memo path errors (e.g. unreadable dir).
		evts, rerr := event.ReadAll(paths.LogIn(p.Path))
		if rerr != nil {
			return ""
		}
		dto = toDTO(projection.Project(evts))
	}
	for _, a := range dto.Agents {
		if a.ID == seat {
			return a.Kind
		}
	}
	return ""
}

type cockpitPromptReq struct {
	Seat string `json:"seat"`
	Text string `json:"text"`
}

type cockpitPromptResp struct {
	OK       bool   `json:"ok"`
	ThreadID string `json:"threadId"`
}

func (s *Server) handleCockpitPrompt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, dir, ok := s.project(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	var req cockpitPromptReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Seat == "" {
		writeErr(w, http.StatusBadRequest, "seat is required")
		return
	}
	if req.Text == "" {
		writeErr(w, http.StatusBadRequest, "text is required")
		return
	}
	if err := s.ensureCockpit(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	key := cockpit.SessionKey{Project: id, Seat: req.Seat}
	opts := cockpit.StartOpts{RepoDir: dir, Seat: req.Seat}
	cs, err := s.cockpit.Session(r.Context(), key, opts)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := cs.Prompt(r.Context(), req.Text); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cockpitPromptResp{OK: true, ThreadID: cs.ThreadID()})
}

func (s *Server) handleCockpitStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, dir, ok := s.project(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	seat := r.URL.Query().Get("seat")
	if seat == "" {
		writeErr(w, http.StatusBadRequest, "seat is required")
		return
	}

	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	if err := s.ensureCockpit(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	key := cockpit.SessionKey{Project: id, Seat: seat}
	cs, err := s.cockpit.Session(r.Context(), key, cockpit.StartOpts{RepoDir: dir, Seat: seat})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "retry: 3000\n: ok\n\n")
	fl.Flush()

	// Replay persisted history before going live so a reconnecting client sees the
	// full thread in order.
	if hist, err := cs.History(); err == nil {
		for _, e := range hist {
			writeCockpitEvent(w, e)
		}
		fl.Flush()
	}

	subID, ch := cs.Subscribe()
	defer cs.Unsubscribe(subID)

	hb := time.NewTicker(sseHeartbeat)
	defer hb.Stop()

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			writeCockpitEvent(w, e)
			fl.Flush()
		case <-hb.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeCockpitEvent(w io.Writer, e cockpit.Event) {
	b, err := json.Marshal(e)
	if err != nil {
		// Should never happen; emit a structured error envelope instead of crashing.
		b, _ = json.Marshal(cockpit.Event{Kind: cockpit.EventError, Err: fmt.Sprintf("marshal event: %v", err)})
	}
	_, _ = io.WriteString(w, "data: ")
	_, _ = w.Write(b)
	_, _ = io.WriteString(w, "\n\n")
}

type cockpitPermissionReq struct {
	Seat       string `json:"seat"`
	ApprovalID string `json:"approvalId"`
	Decision   string `json:"decision"`
}

func (s *Server) handleCockpitPermission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, ok := s.project(id); !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	var req cockpitPermissionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Seat == "" {
		writeErr(w, http.StatusBadRequest, "seat is required")
		return
	}
	if req.ApprovalID == "" {
		writeErr(w, http.StatusBadRequest, "approvalId is required")
		return
	}
	if err := s.ensureCockpit(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	key := cockpit.SessionKey{Project: id, Seat: req.Seat}
	cs, ok := s.cockpit.Get(key)
	if !ok {
		writeErr(w, http.StatusBadRequest, "session not found")
		return
	}

	var d cockpit.Decision
	switch req.Decision {
	case "allow":
		d = cockpit.DecisionAllow
	case "deny":
		d = cockpit.DecisionDeny
	case "allow_for_session":
		d = cockpit.DecisionAllowForSession
	default:
		writeErr(w, http.StatusBadRequest, "decision must be allow, deny or allow_for_session")
		return
	}

	if err := cs.Respond(req.ApprovalID, d); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type cockpitCancelReq struct {
	Seat string `json:"seat"`
}

func (s *Server) handleCockpitCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, ok := s.project(id); !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	var req cockpitCancelReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Seat == "" {
		writeErr(w, http.StatusBadRequest, "seat is required")
		return
	}
	if err := s.ensureCockpit(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	key := cockpit.SessionKey{Project: id, Seat: req.Seat}
	cs, ok := s.cockpit.Get(key)
	if !ok {
		writeErr(w, http.StatusBadRequest, "session not found")
		return
	}
	if err := cs.Interrupt(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type cockpitPendingItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	ToolName string `json:"toolName"`
}

type cockpitStatusDTO struct {
	ThreadID string               `json:"threadId"`
	Pending  []cockpitPendingItem `json:"pending"`
}

func (s *Server) handleCockpitStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, ok := s.project(id); !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	seat := r.URL.Query().Get("seat")
	if seat == "" {
		writeErr(w, http.StatusBadRequest, "seat is required")
		return
	}
	if err := s.ensureCockpit(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	key := cockpit.SessionKey{Project: id, Seat: seat}
	cs, ok := s.cockpit.Get(key)
	if !ok {
		writeJSON(w, http.StatusOK, cockpitStatusDTO{ThreadID: "", Pending: []cockpitPendingItem{}})
		return
	}

	pending := cs.Pending()
	items := make([]cockpitPendingItem, 0, len(pending))
	for _, p := range pending {
		items = append(items, cockpitPendingItem{ID: p.ID, Kind: p.Kind, ToolName: p.ToolName})
	}
	writeJSON(w, http.StatusOK, cockpitStatusDTO{ThreadID: cs.ThreadID(), Pending: items})
}
