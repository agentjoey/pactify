package serve

import (
	"context"
	"encoding/json"
	"errors"
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

// errNothingToResume is surfaced by the resume helper/endpoint when no thread
// has been persisted for the (project, seat).
var errNothingToResume = errors.New("nothing to resume")

func (s *Server) registerCockpitRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/projects/{id}/cockpit/prompt", s.handleCockpitPrompt)
	mux.HandleFunc("GET /api/projects/{id}/cockpit/stream", s.handleCockpitStream)
	mux.HandleFunc("POST /api/projects/{id}/cockpit/permission", s.handleCockpitPermission)
	mux.HandleFunc("POST /api/projects/{id}/cockpit/cancel", s.handleCockpitCancel)
	mux.HandleFunc("GET /api/projects/{id}/cockpit/status", s.handleCockpitStatus)
	mux.HandleFunc("POST /api/projects/{id}/cockpit/resume", s.handleCockpitResume)
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

// cockpitCapableKind reports whether a seat kind can host a deep-integration
// cockpit session. Kept in sync with backendForKey's supported kinds.
func cockpitCapableKind(kind string) bool {
	switch kind {
	case "claude-code", "codex-cli", "kimi-cli", "gemini-cli", "opencode":
		return true
	default:
		return false
	}
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
	if !cockpitCapableKind(kind) {
		return nil, fmt.Errorf("seat %q kind %q is not deep-integration (claude-code/codex-cli/kimi-cli/gemini-cli/opencode only)", key.Seat, kind)
	}
	switch kind {
	case "claude-code":
		return cockpit.NewClaudeBackend(), nil
	case "codex-cli":
		return cockpit.NewCodexBackend(), nil
	case "kimi-cli", "gemini-cli", "opencode":
		return cockpit.NewACPBackend(kind), nil
	default:
		return nil, fmt.Errorf("seat %q kind %q is not deep-integration (claude-code/codex-cli/kimi-cli/gemini-cli/opencode only)", key.Seat, kind)
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

// cockpitProjectDir returns the repo dir for a registered project.
func (s *Server) cockpitProjectDir(project string) (string, bool) {
	s.pmu.RLock()
	defer s.pmu.RUnlock()
	p, ok := s.projects[project]
	return p.Path, ok
}

// cockpitSessionFor gets or starts a cockpit session for (project, seat),
// persisting a non-empty threadID when one is available. Used by the local
// HTTP endpoints and the remote Cockpiter implementation so both surfaces share
// the same lifecycle.
func (s *Server) cockpitSessionFor(ctx context.Context, project, seat string) (*cockpit.CockpitSession, string, error) {
	dir, ok := s.cockpitProjectDir(project)
	if !ok {
		return nil, "", fmt.Errorf("unknown project %q", project)
	}
	if err := s.ensureCockpit(); err != nil {
		return nil, "", err
	}
	key := cockpit.SessionKey{Project: project, Seat: seat}
	cs, err := s.cockpit.Session(ctx, key, cockpit.StartOpts{RepoDir: dir, Seat: seat})
	if err != nil {
		return nil, "", err
	}
	tid := cs.ThreadID()
	if tid != "" {
		s.cockpit.NoteThread(key, tid)
	}
	return cs, tid, nil
}

// cockpitSessionGet returns an existing live session without creating one.
func (s *Server) cockpitSessionGet(project, seat string) (*cockpit.CockpitSession, bool, error) {
	if err := s.ensureCockpit(); err != nil {
		return nil, false, err
	}
	key := cockpit.SessionKey{Project: project, Seat: seat}
	cs, ok := s.cockpit.Get(key)
	return cs, ok, nil
}

// cockpitResumeSession resumes the persisted thread for (project, seat).
func (s *Server) cockpitResumeSession(ctx context.Context, project, seat string) (*cockpit.CockpitSession, string, error) {
	if _, ok := s.cockpitProjectDir(project); !ok {
		return nil, "", fmt.Errorf("unknown project %q", project)
	}
	if err := s.ensureCockpit(); err != nil {
		return nil, "", err
	}
	key := cockpit.SessionKey{Project: project, Seat: seat}
	threadID := s.cockpit.StoredThread(key)
	if threadID == "" {
		return nil, "", errNothingToResume
	}
	cs, err := s.cockpit.Resume(ctx, key, threadID)
	if err != nil {
		return nil, "", err
	}
	return cs, cs.ThreadID(), nil
}

// cockpitDecision parses a remote permission decision string. It accepts both
// the HTTP endpoint spelling (allow_for_session) and the wire rpc spelling
// (allow_session) so the two surfaces stay aligned.
func cockpitDecision(decision string) (cockpit.Decision, bool) {
	switch decision {
	case "allow":
		return cockpit.DecisionAllow, true
	case "deny":
		return cockpit.DecisionDeny, true
	case "allow_for_session", "allow_session":
		return cockpit.DecisionAllowForSession, true
	default:
		return "", false
	}
}

// pendingItemsFor returns the pending-approval snapshot for a session.
func pendingItemsFor(cs *cockpit.CockpitSession) []cockpitPendingItem {
	pending := cs.Pending()
	items := make([]cockpitPendingItem, 0, len(pending))
	for _, p := range pending {
		items = append(items, cockpitPendingItem{
			ID:       p.ID,
			Kind:     p.Kind,
			ToolName: p.ToolName,
			RawInput: p.RawInput,
			Risk:     cockpit.GradeRisk(p.ToolName, string(p.RawInput)),
		})
	}
	return items
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
	if _, _, ok := s.project(id); !ok {
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

	cs, _, err := s.cockpitSessionFor(r.Context(), id, req.Seat)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := cs.Prompt(r.Context(), req.Text); err != nil {
		if errors.Is(err, cockpit.ErrPromptRateLimited) {
			writeErr(w, http.StatusTooManyRequests, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cockpitPromptResp{OK: true, ThreadID: cs.ThreadID()})
}

func (s *Server) handleCockpitStream(w http.ResponseWriter, r *http.Request) {
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

	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	cs, _, err := s.cockpitSessionFor(r.Context(), id, seat)
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

	cs, ok, err := s.cockpitSessionGet(id, req.Seat)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusBadRequest, "session not found")
		return
	}

	d, ok := cockpitDecision(req.Decision)
	if !ok {
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

	cs, ok, err := s.cockpitSessionGet(id, req.Seat)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	ToolName string          `json:"toolName"`
	RawInput json.RawMessage `json:"rawInput,omitempty"`
	Risk     string          `json:"risk"`
}

type cockpitStatusDTO struct {
	ThreadID  string               `json:"threadId"`
	Capable   bool                 `json:"capable"`
	Resumable bool                 `json:"resumable"`
	Reason    string               `json:"reason"`
	Pending   []cockpitPendingItem `json:"pending"`
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

	// Capability pre-flight: a live session is proof enough; otherwise judge by
	// the seat's roster kind (the same set backendForKey accepts). Reason is set
	// only when incapable, so the panel can show a friendly hint instead of
	// letting the user type into a cockpit that can never start.
	key := cockpit.SessionKey{Project: id, Seat: seat}
	cs, ok := s.cockpit.Get(key)
	if !ok {
		kind := s.seatKind(id, seat)
		capable := cockpitCapableKind(kind)
		reason := ""
		if !capable {
			reason = fmt.Sprintf("seat %q has no deep-integration or ACP kind (kind=%q)", seat, kind)
		}
		resumable := capable && s.cockpit.StoredThread(key) != ""
		writeJSON(w, http.StatusOK, cockpitStatusDTO{ThreadID: "", Capable: capable, Resumable: resumable, Reason: reason, Pending: []cockpitPendingItem{}})
		return
	}

	// A live session may have received its threadID asynchronously; persist it.
	if tid := cs.ThreadID(); tid != "" {
		s.cockpit.NoteThread(key, tid)
	}

	writeJSON(w, http.StatusOK, cockpitStatusDTO{ThreadID: cs.ThreadID(), Capable: true, Resumable: false, Pending: pendingItemsFor(cs)})
}

type cockpitResumeReq struct {
	Seat string `json:"seat"`
}

type cockpitResumeResp struct {
	OK       bool   `json:"ok"`
	ThreadID string `json:"threadId"`
}

func (s *Server) handleCockpitResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, ok := s.project(id); !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	var req cockpitResumeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Seat == "" {
		writeErr(w, http.StatusBadRequest, "seat is required")
		return
	}

	_, tid, err := s.cockpitResumeSession(r.Context(), id, req.Seat)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errNothingToResume) {
			status = http.StatusBadRequest
		}
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cockpitResumeResp{OK: true, ThreadID: tid})
}
